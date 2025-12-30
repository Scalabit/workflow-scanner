package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/agent"
	daggerImpl "workflow-scanner/pkg/dagger"
	"workflow-scanner/pkg/github"
	"workflow-scanner/pkg/zizmor"
)

type WorkflowScanner struct{}

// TokenValidationResponse represents the API token validation response.
type TokenValidationResponse struct {
	Valid bool `json:"valid"`
}

// validateAPIToken checks if the API token is valid by calling the web server.
func validateAPIToken(token string) bool {
	// Get the server URL from environment, default to localhost for development
	serverURL := os.Getenv("TOKEN_VALIDATION_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080" // Default for local development
	}

	// Create request to validate token
	req, err := http.NewRequest(http.MethodGet, serverURL+"/api/validate-token", nil)
	if err != nil {
		return false
	}

	// Set authorization header
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Check if token is valid (200 status means valid)
	return resp.StatusCode == http.StatusOK
}

// ScanAndFixWorkflows scans and fixes workflows with API token validation.
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

	// Extract GitHub token string
	githubTokenStr, err := githubToken.Plaintext(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to extract GitHub token: %w", err)
	}

	daggerClient := daggerImpl.NewClient(dag)
	zizmor := zizmor.NewZizmor(daggerClient)
	agent := agent.NewAgent(daggerClient)
	githubClient := github.NewWrapperIssueClientImpl(dag, githubTokenStr)

	return scanAndFixWorflowsImpl(ctx, repository, source, zizmor, agent, githubClient)
}

func scanAndFixWorflowsImpl(ctx context.Context, repository string, source *dagger.Directory, zizmor zizmor.Zizmor, agent agent.Agent, githubClient github.WrapperIssueClient) (string, error) {
	autoFixedDirectory, zizmorFindings, fixSummary, err := zizmor.RunZizmorAutoFix(ctx, source)
	if err != nil {
		return "", fmt.Errorf("failed to run ZIZMOR auto-fix: %w", err)
	}

	remainingIssues, err := zizmor.CheckRemainingIssues(ctx, autoFixedDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}

	finalDirectory := autoFixedDirectory

	llmExplanations := ""
	if remainingIssues != "" && remainingIssues != "[]" && remainingIssues != "[]\n" {
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
			"\n\n... (truncated due to length - see full scan in workflow logs)"
	}

	prTitle, prBody := github.GetPrTitleBody(finalValidation, zizmorFindings, fixSummary, llmExplanations, summaryExternalFindings)

	return githubClient.CreatePullRequest(ctx, repository, prTitle, prBody, finalDirectory)
}
