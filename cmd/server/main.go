package main

import (
	"crypto/rand"
	"database/sql"
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

	_ "github.com/lib/pq" // PostgreSQL driver
	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

// Lazy initialization of database.
var (
	db     *sql.DB
	dbOnce sync.Once
	dbErr  error
)

func getDatabase() (*sql.DB, error) {
	dbOnce.Do(func() {
		databaseURL := os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			dbErr = fmt.Errorf("DATABASE_URL environment variable not set")

			return
		}

		db, dbErr = sql.Open("postgres", databaseURL)
		if dbErr != nil {
			return
		}

		// Test the connection
		if dbErr = db.Ping(); dbErr != nil {
			return
		}

		log.Printf("Connected to CloudSQL database")
	})

	return db, dbErr
}

// Constants for magic numbers.
const (
	APITokenLength   = 32
	PriceInCents     = 1000 // €10.00 in cents
	ReadTimeoutSecs  = 15   // HTTP read timeout in seconds
	WriteTimeoutSecs = 15   // HTTP write timeout in seconds
	IdleTimeoutSecs  = 60   // HTTP idle timeout in seconds

	ScannerCPUMilli    = 2000 // 2 CPU
	ScannerMemoryMib   = 4096 // 4GB RAM
	ScannerTimeoutSecs = 3600 // 1 hour timeout
	ScannerDiskSizeGB  = 20   // 20GB boot disk

	DummyNum = 12345 //this is only for testing purposes
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

// WorkflowScanner struct for Dagger integration.
type WorkflowScanner struct{}

// WorkflowScanRequest represents a workflow scanning request.
type WorkflowScanRequest struct {
	GithubToken  string `json:"github_token"`
	Repository   string `json:"repository"`
	SourceBase64 string `json:"source_base64"` // Base64 encoded source directory
}

type WorkflowScanGitCloneRequest struct {
	Repository  string `json:"repository"`
	GithubToken string `json:"github_token"`
	LLMAPIKey   string `json:"llm_api_key"`
	CommitSHA   string `json:"commit_sha"`
}

type WorkflowScanResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message,omitempty"`
	PullRequestURL string `json:"pull_request_url,omitempty"`
	Error          string `json:"error,omitempty"`
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
	// CloudSQL validation: Check token against database of valid premium user tokens
	// For now, disable validation and return a dummy user ID
	_ = token // Suppress unused parameter warning

	return DummyNum, true // Always valid for testing
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

// WorkflowScanner struct is now only used for API compatibility (no longer uses Dagger directly)

// HTTP Handlers

func githubAuth(config *Config, w http.ResponseWriter, r *http.Request) {
	// Accept both GET (direct GitHub callback) and POST (from frontend)
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	code, err := extractAuthCode(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	accessToken, err := exchangeCodeForToken(config, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	user, err := fetchGitHubUserWithEmail(&http.Client{}, accessToken)
	if err != nil {
		log.Printf("Failed to fetch user data: %v", err)
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)

		return
	}

	// Handle GET requests (GitHub callback) - redirect to homepage with auth data
	if r.Method == http.MethodGet {
		// Redirect to homepage with auth data in URL fragment for frontend to handle
		redirectURL := fmt.Sprintf("/#access_token=%s&user_login=%s&user_id=%d&user_email=%s", accessToken, user.Login, user.ID, user.Email)
		http.Redirect(w, r, redirectURL, http.StatusFound)

		return
	}

	// Handle POST requests (frontend API calls) - return JSON
	response := AuthResponse{
		AccessToken: accessToken,
		User:        *user,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func extractAuthCode(r *http.Request) (string, error) {
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

func exchangeCodeForToken(config *Config, code string) (string, error) {
	tokenURL := fmt.Sprintf(
		"https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
		config.ClientID, config.ClientSecret, code,
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

func fetchGitHubUserWithEmail(client *http.Client, token string) (*GitHubUser, error) {
	// First, get basic user info
	user, err := fetchGitHubUser(client, token)
	if err != nil {
		return nil, err
	}

	// If email is not public, fetch primary email
	if user.Email == "" {
		email, err := fetchGitHubPrimaryEmail(client, token)
		if err != nil {
			log.Printf("Warning: Could not fetch email: %v", err)
			// Continue without email
		} else {
			user.Email = email
		}
	}

	return user, nil
}

func fetchGitHubUser(client *http.Client, token string) (*GitHubUser, error) {
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

func fetchGitHubPrimaryEmail(client *http.Client, token string) (string, error) {
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

func verifyToken(config *Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

			return
		}

		token := authHeader[7:] // Remove "Bearer " prefix

		// Validate token with GitHub API
		url := fmt.Sprintf("https://api.github.com/applications/%s/token", config.ClientID)

		jsonStr := fmt.Sprintf(`{"access_token": "%s"}`, token)

		req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(jsonStr))
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)

			return
		}

		// Use Basic Auth with client credentials
		req.SetBasicAuth(config.ClientID, config.ClientSecret)
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

func getConfigHandler(config *Config, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]string{
		"client_id":          config.ClientID,
		"stripe_publishable": config.StripePublishable,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func getUserHandler(config *Config, w http.ResponseWriter, r *http.Request) {
	// Token is already verified by middleware, extract it
	authHeader := r.Header.Get("Authorization")
	token := authHeader[7:] // Remove "Bearer " prefix

	// Fetch user data from GitHub using the verified token
	client := &http.Client{}
	user, err := fetchGitHubUserWithEmail(client, token)
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

func incrementAPIKeyUsage(apiKey, repository string, success bool) error {
	database, err := getDatabase()
	if err != nil {
		return fmt.Errorf("database connection error: %w", err)
	}

	// Start a transaction for atomic operations
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Increment usage count in api_keys table
	updateQuery := `
		UPDATE api_keys 
		SET usage_count = usage_count + 1 
		WHERE api_key = $1 AND is_active = true
	`

	result, err := tx.Exec(updateQuery, apiKey)
	if err != nil {
		return fmt.Errorf("failed to update usage count: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("no active API key found or usage not incremented")
	}

	// Log usage in api_usage table for detailed tracking
	logQuery := `
		INSERT INTO api_usage (api_key, repository, used_at, success)
		VALUES ($1, $2, CURRENT_TIMESTAMP, $3)
	`

	_, err = tx.Exec(logQuery, apiKey, repository, success)
	if err != nil {
		return fmt.Errorf("failed to log usage: %w", err)
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Usage incremented for API key %s, repository: %s, success: %t", apiKey, repository, success)

	return nil
}

func incrementUsageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Parse request body
	var req struct {
		Repository string `json:"repository"`
		Success    bool   `json:"success"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)

		return
	}

	if req.Repository == "" {
		http.Error(w, "Repository is required", http.StatusBadRequest)

		return
	}

	// Increment usage
	if err := incrementAPIKeyUsage(token, req.Repository, req.Success); err != nil {
		log.Printf("Failed to increment usage: %v", err)
		http.Error(w, "Failed to increment usage", http.StatusInternalServerError)

		return
	}

	// Return success
	response := map[string]interface{}{
		"success": true,
		"message": "Usage incremented successfully",
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func validateAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")

	// Get database connection
	database, err := getDatabase()
	if err != nil {
		log.Printf("Database connection error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)

		return
	}

	// Check if API key exists and is active with available usage
	var usageCount, usageLimit int
	var isActive bool
	query := `
		SELECT usage_count, usage_limit, is_active 
		FROM api_keys 
		WHERE api_key = $1
	`

	err = database.QueryRow(query, token).Scan(&usageCount, &usageLimit, &isActive)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Invalid API key", http.StatusUnauthorized)

			return
		}
		log.Printf("Database query error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)

		return
	}

	// Check if key is active and has usage remaining
	if !isActive {
		http.Error(w, "API key is inactive", http.StatusForbidden)

		return
	}

	if usageCount >= usageLimit {
		http.Error(w, "API key usage limit exceeded", http.StatusTooManyRequests)

		return
	}

	// Return success with usage info
	response := map[string]interface{}{
		"valid":       true,
		"usage_count": usageCount,
		"usage_limit": usageLimit,
		"remaining":   usageLimit - usageCount,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func revokeAPIToken(config *Config, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Get user info from token
	authHeader := r.Header.Get("Authorization")
	token := authHeader[7:]
	client := &http.Client{}
	user, err := fetchGitHubUserWithEmail(client, token)
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

	// Update API key in CloudSQL database
	updateTokenInDatabase(oldToken, newToken, currentUser.StripeSession, user.Login)

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

func createCheckoutSession(config *Config, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	// Get user info from token
	authHeader := r.Header.Get("Authorization")
	token := authHeader[7:]
	client := &http.Client{}
	user, err := fetchGitHubUserWithEmail(client, token)
	if err != nil {
		http.Error(w, "Failed to fetch user data", http.StatusInternalServerError)

		return
	}

	// Set Stripe API key
	stripe.Key = config.StripeKey

	// Create checkout session for 10 euros
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "https://workflow-scanner-36bg3tpnra-lz.a.run.app"
	}

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String("eur"),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String("remediator Premium"),
					},
					UnitAmount: stripe.Int64(PriceInCents), // €10.00 in cents
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:    stripe.String(baseURL + "/"),
		CancelURL:     stripe.String(baseURL + "/"),
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

func handleStripeWebhook(config *Config, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	event, err := validateStripeWebhook(config, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	processStripeEvent(event)
	w.WriteHeader(http.StatusOK)
}

func validateStripeWebhook(config *Config, r *http.Request) (stripe.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return stripe.Event{}, fmt.Errorf("Failed to read request body")
	}

	signature := r.Header.Get("Stripe-Signature")
	if signature == "" {
		return stripe.Event{}, fmt.Errorf("Missing Stripe signature")
	}

	event, err := webhook.ConstructEventWithOptions(body, signature, config.StripeWebhookKey, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		log.Printf("Webhook signature verification failed: %v", err)

		return stripe.Event{}, fmt.Errorf("Invalid signature")
	}

	return event, nil
}

func processStripeEvent(event stripe.Event) {
	switch event.Type {
	case "checkout.session.completed":
		handleCheckoutSessionCompleted(event)
	default:
		log.Printf("Unhandled event type: %s", event.Type)
	}
}

func handleCheckoutSessionCompleted(event stripe.Event) {
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
		createPremiumUser(githubEmail, githubUser, githubIDStr, session.ID)
	}
}

func createPremiumUser(githubEmail, githubUser, githubIDStr, sessionID string) {
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

	// Store the premium user in memory (for backward compatibility)
	premiumMutex.Lock()
	premiumUsers[githubID] = premiumUser
	premiumMutex.Unlock()

	// Add token to valid tokens list (for backward compatibility)
	addValidToken(apiToken, githubID)

	// Store API key in CloudSQL database
	database, err := getDatabase()
	if err != nil {
		log.Printf("Database connection error when creating premium user: %v", err)
	} else {
		insertQuery := `
			INSERT INTO api_keys (api_key, subscription_id, usage_count, usage_limit, is_active, created_at)
			VALUES ($1, $2, 0, 100, true, CURRENT_TIMESTAMP)
			ON CONFLICT (api_key) DO UPDATE SET
				subscription_id = EXCLUDED.subscription_id,
				is_active = EXCLUDED.is_active
		`
		_, err = database.Exec(insertQuery, apiToken, sessionID)
		if err != nil {
			log.Printf("Failed to store API key in database: %v", err)
		} else {
			log.Printf("API key stored in CloudSQL for user %s", githubUser)
		}
	}

	log.Printf("User %s (%s) upgraded to premium. Token: %s, Expires: %s",
		githubUser, githubEmail, apiToken[:10]+"...", expiresAt.Format(time.RFC3339))
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
		// Serve index.html for client-side routing
		http.ServeFile(w, r, filepath.Join("frontend", "index.html"))

		return
	}
	http.ServeFile(w, r, filePath)
}

func main() {
	config := loadConfig()

	if config.ClientID == "" || config.ClientSecret == "" {
		log.Fatal("Missing required environment variables: GH_APP_ID, GH_APP_SECRET")
	}

	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/auth/github", func(w http.ResponseWriter, r *http.Request) {
		githubAuth(config, w, r)
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		getConfigHandler(config, w, r)
	})
	mux.HandleFunc("/api/user", verifyToken(config, func(w http.ResponseWriter, r *http.Request) {
		getUserHandler(config, w, r)
	}))
	mux.HandleFunc("/api/create-checkout-session", verifyToken(config, func(w http.ResponseWriter, r *http.Request) {
		createCheckoutSession(config, w, r)
	}))
	mux.HandleFunc("/api/revoke-token", verifyToken(config, func(w http.ResponseWriter, r *http.Request) {
		revokeAPIToken(config, w, r)
	}))
	mux.HandleFunc("/api/validate-token", func(w http.ResponseWriter, r *http.Request) {
		validateAPIToken(w, r)
	})
	mux.HandleFunc("/api/increment-usage", func(w http.ResponseWriter, r *http.Request) {
		incrementUsageHandler(w, r)
	})
	// Scan endpoints removed - scanning is now handled by binary via GitHub Actions
	mux.HandleFunc("/webhook/stripe", func(w http.ResponseWriter, r *http.Request) {
		handleStripeWebhook(config, w, r)
	})

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
	log.Printf("OAuth callback URL: /auth/github")
	log.Fatal(server.ListenAndServe())
}

// updateTokenInDatabase handles the database operations for token revocation.
func updateTokenInDatabase(oldToken, newToken, subscriptionID, userLogin string) {
	database, err := getDatabase()
	if err != nil {
		log.Printf("Database connection error when revoking token: %v", err)

		return
	}

	err = revokeAndReplaceToken(database, oldToken, newToken, subscriptionID)
	if err != nil {
		log.Printf("Failed to revoke and replace token: %v", err)

		return
	}

	log.Printf("Token revocation updated in CloudSQL for user %s", userLogin)
}

// revokeAndReplaceToken performs the actual database transaction.
func revokeAndReplaceToken(db *sql.DB, oldToken, newToken, subscriptionID string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// Deactivate old token
	_, err = tx.Exec("UPDATE api_keys SET is_active = false WHERE api_key = $1", oldToken)
	if err != nil {
		return fmt.Errorf("failed to deactivate old token: %w", err)
	}

	// Insert new token
	insertQuery := `
		INSERT INTO api_keys (api_key, subscription_id, usage_count, usage_limit, is_active, created_at)
		VALUES ($1, $2, 0, 100, true, CURRENT_TIMESTAMP)
	`
	_, err = tx.Exec(insertQuery, newToken, subscriptionID)
	if err != nil {
		return fmt.Errorf("failed to insert new token: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
