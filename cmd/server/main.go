package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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

	"workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/agent"
	daggerImpl "workflow-scanner/pkg/dagger"
	"workflow-scanner/pkg/github"
	"workflow-scanner/pkg/zizmor"
)

// Access to global dag from dagger.gen.go.
var dag = dagger.Connect()

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

// WorkflowScanner struct for Dagger integration.
type WorkflowScanner struct{}

// WorkflowScanRequest represents a workflow scanning request.
type WorkflowScanRequest struct {
	GithubToken  string `json:"github_token"`
	Repository   string `json:"repository"`
	SourceBase64 string `json:"source_base64"` // Base64 encoded source directory
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

// ScanAndFixWorkflows implements the Dagger workflow scanning.
func (m *WorkflowScanner) ScanAndFixWorkflows(ctx context.Context, apiToken *dagger.Secret, githubToken *dagger.Secret, repository string, source *dagger.Directory) (string, error) {
	// Extract and validate API token
	tokenValue, err := apiToken.Plaintext(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to extract API token: %w", err)
	}

	// Validate API token (temporarily disabled for testing)
	_ = tokenValue // API token validation temporarily disabled
	// if !validateAPIToken(tokenValue) {
	//	return "", fmt.Errorf("invalid or expired API token - please check your subscription")
	// }

	daggerClient := daggerImpl.NewClient(dag)
	zizmor := zizmor.NewZizmor(daggerClient)
	agentImpl := agent.NewAgent(daggerClient)
	githubClient := github.NewWrapperIssueClientImpl(dag.GithubIssue(dagger.GithubIssueOpts{Token: githubToken}))

	return scanAndFixWorkflowsImpl(ctx, repository, source, zizmor, agentImpl, githubClient)
}

func scanAndFixWorkflowsImpl(ctx context.Context, repository string, source *dagger.Directory, zizmor zizmor.Zizmor, agent agent.Agent, githubClient github.WrapperIssueClient) (string, error) {
	autoFixedDirectory, zizmorOutput, err := zizmor.RunZizmorAutoFix(ctx, source)
	if err != nil {
		return "", fmt.Errorf("failed to run ZIZMOR auto-fix: %w", err)
	}

	remainingIssues, err := zizmor.CheckRemainingIssues(ctx, autoFixedDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}

	finalDirectory := autoFixedDirectory

	llmExplanations := ""
	if remainingIssues != "" && remainingIssues != "[]" && remainingIssues != "[]\\n" {
		finalDirectory, llmExplanations, err = agent.FixRemainingIssues(ctx, autoFixedDirectory, remainingIssues)
		if err != nil {
			return "", fmt.Errorf("failed to fix remaining issues with LLM: %w", err)
		}
	} else {
		llmExplanations = "No remaining security issues found after ZIZMOR auto-fix"
	}

	// Run final validation scan on the fixed code
	finalValidation, err := zizmor.CheckRemainingIssues(ctx, finalDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to run final validation scan: %w", err)
	}

	// Scan external repositories used in workflows
	fullRepoFindings, err := zizmor.ScanExternalDependencies(ctx, finalDirectory)
	summaryExternalFindings := zizmor.SummarizeExternalFindings(fullRepoFindings)
	if err != nil {
		summaryExternalFindings = fmt.Sprintf("Failed to scan external dependencies: %s", err.Error())
	}

	// Truncate external findings if too long to fit GitHub's 65,536 char limit
	maxExternalLength := 20000 // Leave room for other content
	if len(summaryExternalFindings) > maxExternalLength {
		summaryExternalFindings = summaryExternalFindings[:maxExternalLength] +
			"\\n\\n... (truncated due to length - see full scan in workflow logs)"
	}

	prTitle, prBody := github.GetPrTitleBody(finalValidation, zizmorOutput, llmExplanations, summaryExternalFindings)

	return githubClient.CreatePullRequest(ctx, repository, prTitle, prBody, finalDirectory)
}

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

func validateAPIToken(w http.ResponseWriter, r *http.Request) {
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

func validateRequestMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return false
	}

	return true
}

func validateRequestBody(w http.ResponseWriter, r *http.Request) (*WorkflowScanRequest, bool) {
	var req WorkflowScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)

		return nil, false
	}

	if req.Repository == "" || req.GithubToken == "" || req.SourceBase64 == "" {
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Missing required fields: repository, github_token, source_base64",
		}
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return nil, false
	}

	return &req, true
}

func validateScanRequest(w http.ResponseWriter, r *http.Request) (string, *WorkflowScanRequest, int, bool) {
	if !validateRequestMethod(w, r) {
		return "", nil, 0, false
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

		return "", nil, 0, false
	}

	apiToken := authHeader[7:]
	githubID, valid := isValidAPIToken(apiToken)
	if !valid {
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Invalid or expired API token",
		}
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return "", nil, 0, false
	}

	req, ok := validateRequestBody(w, r)
	if !ok {
		return "", nil, 0, false
	}

	return apiToken, req, githubID, true
}

func decodeSourceData(w http.ResponseWriter, sourceBase64 string) ([]byte, bool) {
	sourceData, err := base64.StdEncoding.DecodeString(sourceBase64)
	if err != nil {
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Invalid source data encoding",
		}
		http.Error(w, "Invalid source data", http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return nil, false
	}

	return sourceData, true
}

func executeScan(w http.ResponseWriter, ctx context.Context, scanner *WorkflowScanner, apiToken, repository string, githubToken string, sourceDir *dagger.Directory, githubID int) {
	apiTokenSecret := dag.SetSecret("api-token", apiToken)
	githubTokenSecret := dag.SetSecret("github-token", githubToken)

	prURL, err := scanner.ScanAndFixWorkflows(ctx, apiTokenSecret, githubTokenSecret, repository, sourceDir)
	if err != nil {
		log.Printf("Workflow scan failed for user %d, repo %s: %v", githubID, repository, err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   fmt.Sprintf("Scan failed: %v", err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return
	}

	log.Printf("Workflow scan completed successfully for user %d, repo %s, PR: %s", githubID, repository, prURL)
	response := WorkflowScanResponse{
		Success:        true,
		Message:        "Workflow scan completed successfully",
		PullRequestURL: prURL,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func scanWorkflows(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	apiToken, req, githubID, ok := validateScanRequest(w, r)
	if !ok {
		return
	}

	log.Printf("Workflow scan requested by user %d for repository %s", githubID, req.Repository)

	sourceData, ok := decodeSourceData(w, req.SourceBase64)
	if !ok {
		return
	}

	ctx := context.Background()
	sourceDir := dag.Directory().WithNewFile("workflows.tar.gz", string(sourceData))
	scanner := &WorkflowScanner{}

	executeScan(w, ctx, scanner, apiToken, req.Repository, req.GithubToken, sourceDir, githubID)
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

	// Store the premium user
	premiumMutex.Lock()
	premiumUsers[githubID] = premiumUser
	premiumMutex.Unlock()

	// Add token to valid tokens list
	addValidToken(apiToken, githubID)

	log.Printf("User %s (%s) upgraded to premium. Token: %s, Expires: %s",
		githubUser, githubEmail, apiToken[:10]+"...", expiresAt.Format(time.RFC3339))
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
	mux.HandleFunc("/api/scan-workflows", func(w http.ResponseWriter, r *http.Request) {
		scanWorkflows(w, r)
	})
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
	log.Printf("OAuth callback URL: http://localhost:%s/auth/github", config.Port)
	log.Fatal(server.ListenAndServe())
}
