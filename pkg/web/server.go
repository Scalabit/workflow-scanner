package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

// Constants for magic numbers.
const (
	APITokenLength   = 32
	PriceInCents     = 1000 // €10.00 in cents
	ReadTimeoutSecs  = 15   // HTTP read timeout in seconds
	WriteTimeoutSecs = 15   // HTTP write timeout in seconds
	IdleTimeoutSecs  = 60   // HTTP idle timeout in seconds
)

type Config struct {
	ClientID          string
	ClientSecret      string
	StripeKey         string
	StripePublishable string
	StripeWebhookKey  string
	Port              string
}

type GitHubUser struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
	Email string `json:"email"`
}

type AuthResponse struct {
	AccessToken string     `json:"access_token"`
	User        GitHubUser `json:"user"`
}

type GitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type PremiumUser struct {
	GitHubID      int       `json:"github_id"`
	GitHubLogin   string    `json:"github_login"`
	Email         string    `json:"email"`
	APIToken      string    `json:"api_token"`
	SubscribedAt  time.Time `json:"subscribed_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	StripeSession string    `json:"stripe_session"`
}

var (
	premiumUsers = make(map[int]*PremiumUser)
	premiumMutex = sync.RWMutex{}
	// Track valid tokens - only tokens in this map are accepted.
	validTokens      = make(map[string]int) // token -> githubID
	validTokensMutex = sync.RWMutex{}
)

func loadConfig() *Config {
	return &Config{
		ClientID:          getEnv("GH_APP_ID", ""),
		ClientSecret:      getEnv("GH_APP_SECRET", ""),
		StripeKey:         getEnv("TEST_STRIPE", ""),
		StripePublishable: getEnv("TEST_STRIPE_PK", ""),
		StripeWebhookKey:  getEnv("TEST_STRIPE_WEBHOOK_SECRET", ""),
		Port:              getEnv("PORT", "8080"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return defaultValue
}

func generateAPIToken() string {
	bytes := make([]byte, APITokenLength)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("Failed to generate random bytes: %v", err)

		return "fs_fallback_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}

	return "fs_" + hex.EncodeToString(bytes)
}

func isPremiumUser(githubID int) bool {
	premiumMutex.RLock()
	defer premiumMutex.RUnlock()

	user, exists := premiumUsers[githubID]
	if !exists {
		return false
	}

	return time.Now().Before(user.ExpiresAt)
}

func isValidAPIToken(token string) (int, bool) {
	validTokensMutex.RLock()
	defer validTokensMutex.RUnlock()

	githubID, exists := validTokens[token]
	if !exists {
		return 0, false
	}

	// Check if user is still premium
	if !isPremiumUser(githubID) {
		return 0, false
	}

	return githubID, true
}

func addValidToken(token string, githubID int) {
	validTokensMutex.Lock()
	defer validTokensMutex.Unlock()

	validTokens[token] = githubID
}

func removeValidToken(token string) {
	validTokensMutex.Lock()
	defer validTokensMutex.Unlock()

	delete(validTokens, token)
}

func getPremiumUser(githubID int) *PremiumUser {
	premiumMutex.RLock()
	defer premiumMutex.RUnlock()

	return premiumUsers[githubID]
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)

			return
		}

		next.ServeHTTP(w, r)
	})
}

func (c *Config) githubAuth(w http.ResponseWriter, r *http.Request) {
	// Accept both GET (direct GitHub callback) and POST (from frontend)
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	code, err := c.extractAuthCode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	accessToken, err := c.exchangeCodeForToken(code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	user, err := c.fetchGitHubUserWithEmail(&http.Client{}, accessToken)
	if err != nil {
		log.Printf("Failed to fetch user data: %v", err)
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)

		return
	}

	response := AuthResponse{
		AccessToken: accessToken,
		User:        *user,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (c *Config) extractAuthCode(r *http.Request) (string, error) {
	var code string

	if r.Method == http.MethodGet {
		// GitHub callback with query parameter
		code = r.URL.Query().Get("code")
	} else {
		// POST request from frontend with JSON body
		var authRequest struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&authRequest); err != nil {
			return "", fmt.Errorf("Invalid request body")
		}
		code = authRequest.Code
	}

	if code == "" {
		return "", fmt.Errorf("Missing authorization code")
	}

	return code, nil
}

func (c *Config) exchangeCodeForToken(code string) (string, error) {
	tokenURL := fmt.Sprintf(
		"https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
		c.ClientID, c.ClientSecret, code,
	)

	tokenReq, err := http.NewRequest(http.MethodPost, tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("Failed to create token request")
	}
	tokenReq.Header.Set("Accept", "application/json")

	client := &http.Client{}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("Failed to exchange code for token")
	}
	defer tokenResp.Body.Close()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return "", fmt.Errorf("Failed to read token response")
	}

	var tokenData GitHubTokenResponse
	if err := json.Unmarshal(tokenBody, &tokenData); err != nil {
		return "", fmt.Errorf("Failed to parse token response")
	}

	if tokenData.AccessToken == "" {
		return "", fmt.Errorf("No access token received")
	}

	return tokenData.AccessToken, nil
}

func (c *Config) fetchGitHubUserWithEmail(client *http.Client, token string) (*GitHubUser, error) {
	// First, get basic user info
	user, err := c.fetchGitHubUser(client, token)
	if err != nil {
		return nil, err
	}

	// If email is not public, fetch primary email
	if user.Email == "" {
		email, err := c.fetchGitHubPrimaryEmail(client, token)
		if err != nil {
			log.Printf("Warning: Could not fetch email: %v", err)
			// Continue without email
		} else {
			user.Email = email
		}
	}

	return user, nil
}

func (c *Config) fetchGitHubUser(client *http.Client, token string) (*GitHubUser, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (c *Config) fetchGitHubPrimaryEmail(client *http.Client, token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var emails []struct {
		Email   string `json:"email"`
		Primary bool   `json:"primary"`
	}

	if err := json.Unmarshal(body, &emails); err != nil {
		return "", err
	}

	for _, email := range emails {
		if email.Primary {
			return email.Email, nil
		}
	}

	return "", fmt.Errorf("no primary email found")
}

func (c *Config) verifyToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

			return
		}

		token := authHeader[7:] // Remove "Bearer " prefix

		// Validate token with GitHub API
		url := fmt.Sprintf("https://api.github.com/applications/%s/token", c.ClientID)

		jsonStr := fmt.Sprintf(`{"access_token": "%s"}`, token)

		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(jsonStr))
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)

			return
		}

		// Use Basic Auth with client credentials
		req.SetBasicAuth(c.ClientID, c.ClientSecret)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "Token verification failed", http.StatusUnauthorized)

			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, "Invalid token", http.StatusUnauthorized)

			return
		}

		// Token is valid, continue to next handler
		next(w, r)
	}
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	// Serve the HTML file for root and index.html
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		http.ServeFile(w, r, filepath.Join("frontend", "index.html"))

		return
	}

	// Serve the token page
	if r.URL.Path == "/token" {
		http.ServeFile(w, r, filepath.Join("frontend", "token.html"))

		return
	}

	// For any other path, try to serve from frontend directory
	filePath := filepath.Join("frontend", r.URL.Path)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// Serve index.html for client-side routing (SPA)
		http.ServeFile(w, r, filepath.Join("frontend", "index.html"))

		return
	}
	http.ServeFile(w, r, filePath)
}

func (c *Config) getConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"client_id":          c.ClientID,
		"stripe_publishable": c.StripePublishable,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (c *Config) getUser(w http.ResponseWriter, r *http.Request) {
	// Token is already verified by middleware, extract it
	authHeader := r.Header.Get("Authorization")
	token := authHeader[7:] // Remove "Bearer " prefix

	// Fetch user data from GitHub using the verified token
	client := &http.Client{}
	user, err := c.fetchGitHubUserWithEmail(client, token)
	if err != nil {
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)

		return
	}

	isPremium := isPremiumUser(user.ID)
	premiumUser := getPremiumUser(user.ID)

	response := map[string]interface{}{
		"login":     user.Login,
		"id":        user.ID,
		"email":     user.Email,
		"isPremium": isPremium,
	}

	if premiumUser != nil {
		response["apiToken"] = premiumUser.APIToken
		response["expiresAt"] = premiumUser.ExpiresAt
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (c *Config) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Get user info from token
	authHeader := r.Header.Get("Authorization")
	token := authHeader[7:]
	client := &http.Client{}
	user, err := c.fetchGitHubUserWithEmail(client, token)
	if err != nil {
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)

		return
	}

	// Check if user is premium
	if !isPremiumUser(user.ID) {
		http.Error(w, "Premium subscription required", http.StatusForbidden)

		return
	}

	// Get current premium user
	premiumMutex.Lock()
	currentUser := premiumUsers[user.ID]
	if currentUser == nil {
		premiumMutex.Unlock()
		http.Error(w, "Premium user not found", http.StatusNotFound)

		return
	}

	// Remove old token from valid tokens
	oldToken := currentUser.APIToken
	removeValidToken(oldToken)

	// Generate new token but keep same expiry date
	newToken := generateAPIToken()
	currentUser.APIToken = newToken
	premiumUsers[user.ID] = currentUser
	premiumMutex.Unlock()

	// Add new token to valid tokens
	addValidToken(newToken, user.ID)

	log.Printf("Token revoked for user %s (%s). Old token invalidated, new token: %s",
		user.Login, user.Email, newToken[:10]+"...")

	response := map[string]interface{}{
		"success":   true,
		"apiToken":  newToken,
		"expiresAt": currentUser.ExpiresAt,
		"message":   "API token has been revoked and a new one generated",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (c *Config) createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Get user info from token
	authHeader := r.Header.Get("Authorization")
	token := authHeader[7:]
	client := &http.Client{}
	user, err := c.fetchGitHubUserWithEmail(client, token)
	if err != nil {
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)

		return
	}

	// Set Stripe API key
	stripe.Key = c.StripeKey

	// Create checkout session for €10
	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String("eur"),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String("flowsniffer Premium"),
					},
					UnitAmount: stripe.Int64(PriceInCents), // €10.00 in cents
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:    stripe.String("http://localhost:8080/"),
		CancelURL:     stripe.String("http://localhost:8080/"),
		CustomerEmail: stripe.String(user.Email),
		Metadata: map[string]string{
			"github_user":  user.Login,
			"github_email": user.Email,
			"github_id":    fmt.Sprintf("%d", user.ID),
		},
	}

	s, err := session.New(params)
	if err != nil {
		log.Printf("Stripe error: %v", err)
		http.Error(w, fmt.Sprintf("Failed to create checkout session: %v", err), http.StatusInternalServerError)

		return
	}

	// Return checkout session URL
	response := map[string]string{
		"checkout_url": s.URL,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func (c *Config) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	event, err := c.validateStripeWebhook(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	c.processStripeEvent(event)
	w.WriteHeader(http.StatusOK)
}

func (c *Config) validateStripeWebhook(r *http.Request) (stripe.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return stripe.Event{}, fmt.Errorf("Failed to read request body")
	}

	signature := r.Header.Get("Stripe-Signature")
	if signature == "" {
		return stripe.Event{}, fmt.Errorf("Missing Stripe signature")
	}

	event, err := webhook.ConstructEventWithOptions(body, signature, c.StripeWebhookKey, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		log.Printf("Webhook signature verification failed: %v", err)

		return stripe.Event{}, fmt.Errorf("Invalid signature")
	}

	return event, nil
}

func (c *Config) processStripeEvent(event stripe.Event) {
	switch event.Type {
	case "checkout.session.completed":
		c.handleCheckoutSessionCompleted(event)
	default:
		log.Printf("Unhandled event type: %s", event.Type)
	}
}

func (c *Config) handleCheckoutSessionCompleted(event stripe.Event) {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		log.Printf("Failed to parse checkout session: %v", err)

		return
	}

	log.Printf("Payment successful for session: %s", session.ID)

	// Use metadata to get GitHub user info
	githubEmail, emailExists := session.Metadata["github_email"]
	githubUser, userExists := session.Metadata["github_user"]
	githubIDStr, idExists := session.Metadata["github_id"]

	if emailExists && userExists && idExists {
		c.createPremiumUser(githubEmail, githubUser, githubIDStr, session.ID)
	}
}

func (c *Config) createPremiumUser(githubEmail, githubUser, githubIDStr, sessionID string) {
	// Parse GitHub ID
	var githubID int
	if _, err := fmt.Sscanf(githubIDStr, "%d", &githubID); err != nil {
		log.Printf("Failed to parse GitHub ID '%s': %v", githubIDStr, err)

		return
	}

	// Create premium user with 30-day subscription
	now := time.Now()
	expiresAt := now.AddDate(0, 1, 0) // Add 1 month

	apiToken := generateAPIToken()

	premiumUser := &PremiumUser{
		GitHubID:      githubID,
		GitHubLogin:   githubUser,
		Email:         githubEmail,
		APIToken:      apiToken,
		SubscribedAt:  now,
		ExpiresAt:     expiresAt,
		StripeSession: sessionID,
	}

	// Store the premium user
	premiumMutex.Lock()
	premiumUsers[githubID] = premiumUser
	premiumMutex.Unlock()

	// Add token to valid tokens list
	addValidToken(apiToken, githubID)

	log.Printf("User %s (%s) upgraded to premium. Token: %s, Expires: %s",
		githubUser, githubEmail, apiToken[:10]+"...", expiresAt.Format(time.RFC3339))
}

func (c *Config) validateAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

		return
	}

	// For testing: always return valid
	// Use real validation logic: isValidAPIToken(token)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]bool{"valid": true}); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func main() {
	config := loadConfig()

	if config.ClientID == "" || config.ClientSecret == "" {
		log.Fatal("Missing required environment variables: GH_APP_ID, GH_APP_SECRET")
	}

	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/auth/github", config.githubAuth)
	mux.HandleFunc("/api/config", config.getConfig)
	mux.HandleFunc("/api/user", config.verifyToken(config.getUser))
	mux.HandleFunc("/api/create-checkout-session", config.verifyToken(config.createCheckoutSession))
	mux.HandleFunc("/api/revoke-token", config.verifyToken(config.revokeAPIToken))
	mux.HandleFunc("/api/validate-token", config.validateAPIToken)
	mux.HandleFunc("/webhook/stripe", config.handleStripeWebhook)

	// Static file serving (landing page)
	mux.HandleFunc("/", serveStatic)

	// Wrap with CORS middleware
	handler := corsMiddleware(mux)

	server := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      handler,
		ReadTimeout:  ReadTimeoutSecs * time.Second,
		WriteTimeout: WriteTimeoutSecs * time.Second,
		IdleTimeout:  IdleTimeoutSecs * time.Second,
	}

	log.Printf("Server starting on port %s", config.Port)
	log.Printf("OAuth callback URL: http://localhost:%s/auth/github", config.Port)
	log.Fatal(server.ListenAndServe())
}
