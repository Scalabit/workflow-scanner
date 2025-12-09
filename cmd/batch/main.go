package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/agent"
	daggerImpl "workflow-scanner/pkg/dagger"
	"workflow-scanner/pkg/github"
	"workflow-scanner/pkg/zizmor"
)

func main() {
	// Get parameters from environment variables set by Cloud Batch
	repository := os.Getenv("REPOSITORY")
	githubToken := os.Getenv("GITHUB_TOKEN")
	sourceBase64 := os.Getenv("SOURCE_BASE64")

	if repository == "" || githubToken == "" || sourceBase64 == "" {
		log.Fatal("Missing required environment variables: REPOSITORY, GITHUB_TOKEN, SOURCE_BASE64")
	}

	log.Printf("Batch scanner processing repository: %s", repository)

	ctx := context.Background()

	// Connect to Dagger (works in VM with Docker)
	dag := dagger.Connect()

	// Decode source data and create directory
	sourceData, err := base64.StdEncoding.DecodeString(sourceBase64)
	if err != nil {
		log.Fatalf("Failed to decode source data: %v", err)
	}
	sourceDir := dag.Directory().WithNewFile("workflows.tar.gz", string(sourceData))

	// Create clients (same as server implementation)
	daggerClient := daggerImpl.NewClient(dag)
	zizmor := zizmor.NewZizmor(daggerClient)
	agent := agent.NewAgent(daggerClient)
	githubTokenSecret := dag.SetSecret("github-token", githubToken)
	githubClient := github.NewWrapperIssueClientImpl(dag.GithubIssue(dagger.GithubIssueOpts{Token: githubTokenSecret}))

	// Run scan workflow (same logic as scanAndFixWorflowsImpl)
	prURL, err := scanAndFixWorkflows(ctx, repository, sourceDir, zizmor, agent, githubClient)
	if err != nil {
		log.Fatalf("Scan failed: %v", err)
	}

	log.Printf("Scan completed successfully - PR: %s", prURL)
}

// Same as scanAndFixWorflowsImpl from main.go.
func scanAndFixWorkflows(ctx context.Context, repository string, source *dagger.Directory, zizmor zizmor.Zizmor, agent agent.Agent, githubClient github.WrapperIssueClient) (string, error) {
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
	if remainingIssues != "" && remainingIssues != "[]" && remainingIssues != "[]\n" {
		finalDirectory, llmExplanations, err = agent.FixRemainingIssues(ctx, autoFixedDirectory, remainingIssues)
		if err != nil {
			return "", fmt.Errorf("failed to fix remaining issues with LLM: %w", err)
		}
	} else {
		llmExplanations = "No remaining security issues found after ZIZMOR auto-fix"
	}

	finalValidation, err := zizmor.CheckRemainingIssues(ctx, finalDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to run final validation scan: %w", err)
	}

	fullRepoFindings, err := zizmor.ScanExternalDependencies(ctx, finalDirectory)
	summaryExternalFindings := zizmor.SummarizeExternalFindings(fullRepoFindings)
	if err != nil {
		summaryExternalFindings = fmt.Sprintf("Failed to scan external dependencies: %s", err.Error())
	}

	maxExternalLength := 20000
	if len(summaryExternalFindings) > maxExternalLength {
		summaryExternalFindings = summaryExternalFindings[:maxExternalLength] +
			"\n\n... (truncated due to length - see full scan in workflow logs)"
	}

	prTitle, prBody := github.GetPrTitleBody(finalValidation, zizmorOutput, llmExplanations, summaryExternalFindings)

	return githubClient.CreatePullRequest(ctx, repository, prTitle, prBody, finalDirectory)
}
