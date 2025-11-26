package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
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

var (
	premiumUsers = make(map[string]bool)
	premiumMutex = sync.RWMutex{}
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
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
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		code = authRequest.Code
	}

	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	tokenURL := fmt.Sprintf(
		"https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
		c.ClientID, c.ClientSecret, code,
	)

	tokenReq, err := http.NewRequest("POST", tokenURL, nil)
	if err != nil {
		http.Error(w, "Failed to create token request", http.StatusInternalServerError)
		return
	}
	tokenReq.Header.Set("Accept", "application/json")

	client := &http.Client{}
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		http.Error(w, "Failed to exchange code for token", http.StatusInternalServerError)
		return
	}
	defer tokenResp.Body.Close()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		http.Error(w, "Failed to read token response", http.StatusInternalServerError)
		return
	}

	var tokenData GitHubTokenResponse
	if err := json.Unmarshal(tokenBody, &tokenData); err != nil {
		http.Error(w, "Failed to parse token response", http.StatusInternalServerError)
		return
	}

	if tokenData.AccessToken == "" {
		http.Error(w, "No access token received", http.StatusUnauthorized)
		return
	}

	// Get user data
	user, err := c.fetchGitHubUserWithEmail(client, tokenData.AccessToken)
	if err != nil {
		log.Printf("Failed to fetch user data: %v", err)
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)
		return
	}

	// Return JSON response for both GET and POST
	response := AuthResponse{
		AccessToken: tokenData.AccessToken,
		User:        *user,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
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
	req, err := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
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
		"client_id":           c.ClientID,
		"stripe_publishable":  c.StripePublishable,
	}
	json.NewEncoder(w).Encode(response)
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

	premiumMutex.RLock()
	isPremium := premiumUsers[user.Email]
	premiumMutex.RUnlock()

	response := map[string]interface{}{
		"login":     user.Login,
		"id":        user.ID,
		"email":     user.Email,
		"isPremium": isPremium,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
					UnitAmount: stripe.Int64(1000), // €10.00 in cents
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String("http://localhost:8080/"),
		CancelURL:  stripe.String("http://localhost:8080/"),
		CustomerEmail: stripe.String(user.Email),
		Metadata: map[string]string{
			"github_user": user.Login,
			"github_email": user.Email,
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
	json.NewEncoder(w).Encode(response)
}

func (c *Config) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	signature := r.Header.Get("Stripe-Signature")
	if signature == "" {
		http.Error(w, "Missing Stripe signature", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEventWithOptions(body, signature, c.StripeWebhookKey, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		log.Printf("Webhook signature verification failed: %v", err)
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			log.Printf("Failed to parse checkout session: %v", err)
			http.Error(w, "Invalid event data", http.StatusBadRequest)
			return
		}
		
		log.Printf("Payment successful for session: %s", session.ID)
		
		// Use metadata to get GitHub user info
		if githubEmail, exists := session.Metadata["github_email"]; exists {
			premiumMutex.Lock()
			premiumUsers[githubEmail] = true
			premiumMutex.Unlock()
			log.Printf("User %s upgraded to premium", githubEmail)
		}
		
	default:
		log.Printf("Unhandled event type: %s", event.Type)
	}

	w.WriteHeader(http.StatusOK)
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
	mux.HandleFunc("/webhook/stripe", config.handleStripeWebhook)
	
	// Static file serving (landing page)
	mux.HandleFunc("/", serveStatic)
	
	// Wrap with CORS middleware
	handler := corsMiddleware(mux)
	
	log.Printf("Server starting on port %s", config.Port)
	log.Printf("OAuth callback URL: http://localhost:%s/auth/github", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, handler))
}