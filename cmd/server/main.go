package main

import (
	"context"
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

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
	"google.golang.org/protobuf/proto"
)

// Lazy initialization of Compute client.
var (
	computeClient *compute.InstancesClient
	computeOnce   sync.Once
	computeErr    error
)

func getComputeClient() (*compute.InstancesClient, error) {
	computeOnce.Do(func() {
		ctx := context.Background()
		computeClient, computeErr = compute.NewInstancesRESTClient(ctx)
	})

	return computeClient, computeErr
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

func validateAPIToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

		return
	}

	// CloudSQL validation: Check token against database of valid premium user tokens
	// For now, always return valid (validation disabled)
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

func validateTokenAndMethod(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	if !validateRequestMethod(w, r) {
		return "", 0, false
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid authorization header", http.StatusUnauthorized)

		return "", 0, false
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

		return "", 0, false
	}

	return apiToken, githubID, true
}

func validateScanRequest(w http.ResponseWriter, r *http.Request) (string, *WorkflowScanRequest, int, bool) {
	apiToken, githubID, ok := validateTokenAndMethod(w, r)
	if !ok {
		return "", nil, 0, false
	}

	req, ok := validateRequestBody(w, r)
	if !ok {
		return "", nil, 0, false
	}

	return apiToken, req, githubID, true
}

func executeScan(w http.ResponseWriter, ctx context.Context, apiToken, repository, githubToken, sourceBase64 string, githubID int) {
	// Get Compute client
	computeClient, err := getComputeClient()
	if err != nil {
		log.Printf("Compute Engine not available for user %d: %v", githubID, err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Workflow scanning not available in this environment",
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return
	}

	// Submit Compute Engine VM instance
	instanceID, err := createComputeInstance(ctx, computeClient, repository, githubToken, sourceBase64)
	if err != nil {
		log.Printf("Failed to create compute instance for user %d, repo %s: %v", githubID, repository, err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to create scan instance: %v", err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return
	}

	log.Printf("Compute instance created for user %d, repo %s, instance ID: %s", githubID, repository, instanceID)
	response := WorkflowScanResponse{
		Success: true,
		Message: fmt.Sprintf("Scan instance created successfully. Instance ID: %s", instanceID),
	}

	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func createComputeInstance(ctx context.Context, computeClient *compute.InstancesClient, repository, githubToken, sourceBase64 string) (string, error) {
	envVars := map[string]string{
		"REPOSITORY":    repository,
		"GITHUB_TOKEN":  githubToken,
		"SOURCE_BASE64": sourceBase64,
	}

	return createComputeInstanceWithEnv(ctx, computeClient, envVars)
}

func createComputeInstanceWithEnv(ctx context.Context, computeClient *compute.InstancesClient, envVars map[string]string) (string, error) {
	// Get configuration from environment
	projectID := os.Getenv("COMPUTE_PROJECT_ID")
	region := os.Getenv("COMPUTE_REGION")
	scannerImage := os.Getenv("SCANNER_IMAGE")
	serviceAccount := os.Getenv("COMPUTE_SERVICE_ACCOUNT")

	if projectID == "" || region == "" || scannerImage == "" || serviceAccount == "" {
		return "", fmt.Errorf("missing compute configuration environment variables")
	}

	// Generate unique instance ID
	instanceID := fmt.Sprintf("workflow-scan-%d", time.Now().Unix())
	zone := region + "-a" // Use first zone in region

	// Build environment variables for startup script
	var envArgs strings.Builder
	for key, value := range envVars {
		envArgs.WriteString(fmt.Sprintf("  -e %s=%s \\\n", key, value))
	}

	// Create startup script for container-optimized VM
	startupScript := fmt.Sprintf(`#!/bin/bash
docker run --privileged --rm \
%s  %s && shutdown -h now`, envArgs.String(), scannerImage)

	// Create VM instance with container-optimized OS
	instance := &computepb.Instance{
		Name:        &instanceID,
		MachineType: proto.String(fmt.Sprintf("zones/%s/machineTypes/e2-standard-2", zone)),
		Disks: []*computepb.AttachedDisk{
			{
				Boot:       proto.Bool(true),
				AutoDelete: proto.Bool(true),
				InitializeParams: &computepb.AttachedDiskInitializeParams{
					SourceImage: proto.String("projects/cos-cloud/global/images/family/cos-stable"),
					DiskSizeGb:  proto.Int64(ScannerDiskSizeGB),
				},
			},
		},
		NetworkInterfaces: []*computepb.NetworkInterface{
			{
				Network: proto.String(fmt.Sprintf("projects/%s/global/networks/default", projectID)),
				AccessConfigs: []*computepb.AccessConfig{
					{
						Type: proto.String("ONE_TO_ONE_NAT"),
						Name: proto.String("External NAT"),
					},
				},
			},
		},
		Metadata: &computepb.Metadata{
			Items: []*computepb.Items{
				{
					Key:   proto.String("startup-script"),
					Value: proto.String(startupScript),
				},
			},
		},
		Scheduling: &computepb.Scheduling{
			Preemptible: proto.Bool(false),
		},
		ServiceAccounts: []*computepb.ServiceAccount{
			{
				Email: proto.String(serviceAccount),
				Scopes: []string{
					"https://www.googleapis.com/auth/cloud-platform",
				},
			},
		},
	}

	req := &computepb.InsertInstanceRequest{
		Project:          projectID,
		Zone:             zone,
		InstanceResource: instance,
	}

	_, err := computeClient.Insert(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create compute instance: %w", err)
	}

	// Log operation status
	log.Printf("Creating compute instance %s", instanceID)

	return instanceID, nil
}

func executeScanGitClone(w http.ResponseWriter, ctx context.Context, apiToken, repository, githubToken, llmAPIKey, commitSHA string, githubID int) {
	// Get Compute client
	computeClient, err := getComputeClient()
	if err != nil {
		log.Printf("Compute Engine not available for user %d: %v", githubID, err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Workflow scanning not available in this environment",
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return
	}

	// Submit Compute Engine VM instance with git clone approach
	instanceID, err := createComputeInstanceGitClone(ctx, computeClient, repository, githubToken, llmAPIKey, commitSHA)
	if err != nil {
		log.Printf("Failed to create compute instance for user %d, repo %s: %v", githubID, repository, err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to create scan instance: %v", err),
		}
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return
	}

	log.Printf("Compute instance created for user %d, repo %s, instance ID: %s", githubID, repository, instanceID)
	response := WorkflowScanResponse{
		Success: true,
		Message: fmt.Sprintf("Scan instance created successfully. Instance ID: %s", instanceID),
	}

	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func createComputeInstanceGitClone(ctx context.Context, computeClient *compute.InstancesClient, repository, githubToken, llmAPIKey, commitSHA string) (string, error) {
	envVars := map[string]string{
		"REPOSITORY":   repository,
		"GITHUB_TOKEN": githubToken,
		"LLM_API_KEY":  llmAPIKey,
		"COMMIT_SHA":   commitSHA,
	}

	return createComputeInstanceWithEnv(ctx, computeClient, envVars)
}

func validateGitCloneRequest(w http.ResponseWriter, r *http.Request) (string, *WorkflowScanGitCloneRequest, int, bool) {
	apiToken, githubID, ok := validateTokenAndMethod(w, r)
	if !ok {
		return "", nil, 0, false
	}

	req, ok := parseGitCloneRequestBody(w, r)
	if !ok {
		return "", nil, 0, false
	}

	return apiToken, req, githubID, true
}

func parseGitCloneRequestBody(w http.ResponseWriter, r *http.Request) (*WorkflowScanGitCloneRequest, bool) {
	// Read and log the raw request body for debugging
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Failed to read request body",
		}
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return nil, false
	}

	log.Printf("Raw request body: %s", string(bodyBytes))

	var req WorkflowScanGitCloneRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		log.Printf("JSON decode error: %v", err)
		response := WorkflowScanResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		}
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return nil, false
	}

	if req.Repository == "" || req.GithubToken == "" || req.LLMAPIKey == "" {
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Missing required fields: repository, github_token, llm_api_key",
		}
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return nil, false
	}

	return &req, true
}

func scanWorkflowsHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	log.Printf("scanWorkflowsHeaders called - Method: %s, Content-Type: %s, Authorization: %s",
		r.Method, r.Header.Get("Content-Type"), r.Header.Get("Authorization")[:20]+"...")

	apiToken, req, githubID, ok := validateGitCloneRequest(w, r)
	if !ok {
		log.Printf("validateGitCloneRequest failed")

		return
	}

	log.Printf("Workflow scan requested by user %d for repository %s", githubID, req.Repository)

	ctx := context.Background()
	executeScanGitClone(w, ctx, apiToken, req.Repository, req.GithubToken, req.LLMAPIKey, req.CommitSHA, githubID)
}

func scanWorkflows(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	apiToken, req, githubID, ok := validateScanRequest(w, r)
	if !ok {
		return
	}

	log.Printf("Workflow scan requested by user %d for repository %s", githubID, req.Repository)

	// Validate source data is present
	if req.SourceBase64 == "" {
		response := WorkflowScanResponse{
			Success: false,
			Error:   "Missing source data",
		}
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Failed to encode JSON response: %v", err)
		}

		return
	}

	ctx := context.Background()

	// Submit to Cloud Batch (no need to decode source data here, pass it directly to batch job)
	executeScan(w, ctx, apiToken, req.Repository, req.GithubToken, req.SourceBase64, githubID)
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
						Name: stripe.String("flowsniffer Premium"),
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
		scanWorkflows(w, r) // Legacy JSON format with base64 source
	})
	mux.HandleFunc("/api/scan-workflows-git", func(w http.ResponseWriter, r *http.Request) {
		scanWorkflowsHeaders(w, r) // Git clone approach using request body
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
	log.Printf("OAuth callback URL: /auth/github")
	log.Fatal(server.ListenAndServe())
}
