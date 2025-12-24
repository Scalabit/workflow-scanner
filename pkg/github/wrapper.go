package github

//go:generate mockgen -source=wrapper.go -destination=../../mocks/github_wrapper_mock.go -package=mocks WrapperIssueClient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"workflow-scanner/internal/dagger"
)

type WrapperIssueClient interface {
	CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory) (string, error)
}

type WrapperIssueClientImpl struct {
	daggerClient *dagger.Client
	githubToken  string
}

func NewWrapperIssueClientImpl(daggerClient *dagger.Client, githubToken string) WrapperIssueClient {
	return &WrapperIssueClientImpl{
		daggerClient: daggerClient,
		githubToken:  githubToken,
	}
}

func (w *WrapperIssueClientImpl) CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory) (string, error) {
	// Generate branch name with timestamp
	branchName := fmt.Sprintf("workflow-security-fixes-%d", time.Now().Unix())

	// Step 1: Clone the repository
	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)
	gitRepo := w.daggerClient.Git(repoURL, dagger.GitOpts{
		HTTPAuthToken:    w.daggerClient.SetSecret("github-token", w.githubToken),
		HTTPAuthUsername: "x-access-token",
	})

	// Step 2: Get the main branch
	mainBranch := gitRepo.Branch("main")

	// Step 3: Create a new branch from main
	workingDir := mainBranch.Tree()

	// Step 4: Replace the contents with our fixed files
	// Copy all files from source directory to the working directory
	finalDir := workingDir.WithDirectory(".", source, dagger.DirectoryWithDirectoryOpts{
		Exclude: []string{".git"},
	})

	// Step 5: Commit and push changes using git container
	// First, get better error visibility by running git operations step by step
	gitContainer := w.daggerClient.Container().
		From("alpine/git:latest").
		WithDirectory("/workspace", finalDir).
		WithWorkdir("/workspace").
		WithExec([]string{"git", "config", "--global", "user.name", "Workflow Security Bot"}).
		WithExec([]string{"git", "config", "--global", "user.email", "noreply@github.com"}).
		WithExec([]string{"git", "init"})

	// Set up remote with authentication
	remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", w.githubToken, repo)
	gitContainer = gitContainer.WithExec([]string{"git", "remote", "add", "origin", remoteURL})

	// Fetch and create branch
	gitContainer = gitContainer.
		WithExec([]string{"git", "fetch", "origin", "main"}).
		WithExec([]string{"git", "checkout", "-b", branchName, "origin/main"}).
		WithExec([]string{"git", "add", "."})

	// Check if there are any changes to commit
	statusOutput, err := gitContainer.WithExec([]string{"git", "status", "--porcelain"}).Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(statusOutput) == "" {
		return "", fmt.Errorf("no changes to commit - all security issues may already be fixed")
	}

	// Commit and push
	commitMsg := fmt.Sprintf("Security fixes: %s\n\n%s", title, "Automated security fixes applied by Workflow Scanner")
	gitContainer = gitContainer.
		WithExec([]string{"git", "commit", "-m", commitMsg}).
		WithExec([]string{"git", "push", "origin", branchName})

	// Execute the final push
	pushOutput, err := gitContainer.Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to push branch %s: %w\nOutput: %s", branchName, err, pushOutput)
	}

	// Step 6: Create the pull request using GitHub API
	prData := map[string]interface{}{
		"title": title,
		"body":  body,
		"head":  branchName,
		"base":  "main",
	}

	jsonData, err := json.Marshal(prData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal PR data: %w", err)
	}

	// Make the GitHub API call
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls", repo)

	response := w.daggerClient.Container().
		From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "curl"}).
		WithSecretVariable("GITHUB_TOKEN", w.daggerClient.SetSecret("github-token", w.githubToken)).
		WithNewFile("/tmp/pr_data.json", string(jsonData)).
		WithExec([]string{"curl", "-X", "POST",
			"-H", "Authorization: Bearer " + w.githubToken,
			"-H", "Accept: application/vnd.github.v3+json",
			"-H", "Content-Type: application/json",
			"-d", "@/tmp/pr_data.json",
			apiURL})

	responseContent, err := response.Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	// Parse the response to get the PR URL
	var prResponse struct {
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
		Number  int    `json:"number"`
	}

	if err := json.Unmarshal([]byte(responseContent), &prResponse); err != nil {
		return "", fmt.Errorf("failed to parse PR response: %w", err)
	}

	if prResponse.HTMLURL != "" {
		return prResponse.HTMLURL, nil
	}

	return "", fmt.Errorf("PR creation failed, response: %s", responseContent)
}
