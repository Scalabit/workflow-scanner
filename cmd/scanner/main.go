package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/agent"
	daggerImpl "workflow-scanner/pkg/dagger"
	"workflow-scanner/pkg/github"
	"workflow-scanner/pkg/zizmor"
)

type batchConfig struct {
	repository   string
	githubToken  string
	llmAPIKey    string
	commitSHA    string
	sourceBase64 string
	useGitClone  bool
}

func main() {
	config := loadConfig()
	validateConfig(config)

	ctx := context.Background()
	dag := dagger.Connect()

	sourceDir := getSourceDirectory(dag, config)
	runScan(ctx, dag, config, sourceDir)
}

func loadConfig() batchConfig {
	repository := os.Getenv("REPOSITORY")
	githubToken := os.Getenv("GITHUB_TOKEN")
	llmAPIKey := os.Getenv("LLM_API_KEY")
	commitSHA := os.Getenv("COMMIT_SHA")
	sourceBase64 := os.Getenv("SOURCE_BASE64")

	useGitClone := sourceBase64 == "" && llmAPIKey != ""

	return batchConfig{
		repository:   repository,
		githubToken:  githubToken,
		llmAPIKey:    llmAPIKey,
		commitSHA:    commitSHA,
		sourceBase64: sourceBase64,
		useGitClone:  useGitClone,
	}
}

func validateConfig(config batchConfig) {
	if config.repository == "" || config.githubToken == "" {
		log.Fatal("Missing required environment variables: REPOSITORY, GITHUB_TOKEN")
	}

	if !config.useGitClone && config.sourceBase64 == "" {
		log.Fatal("Missing SOURCE_BASE64 for legacy mode")
	}

	if config.useGitClone && config.llmAPIKey == "" {
		log.Fatal("Missing LLM_API_KEY for git clone mode")
	}
}

func getSourceDirectory(dag *dagger.Client, config batchConfig) *dagger.Directory {
	mode := "source-upload"
	if config.useGitClone {
		mode = "git-clone"
	}

	log.Printf("Batch scanner processing repository: %s (mode: %s)", config.repository, mode)

	if config.useGitClone {
		return cloneRepository(dag, config)
	}

	return decodeSourceData(dag, config.sourceBase64)
}

func cloneRepository(dag *dagger.Client, config batchConfig) *dagger.Directory {
	log.Printf("Cloning repository %s using Dagger Git", config.repository)

	setupLLMEnvironment(config.llmAPIKey)

	// Use Dagger's built-in git functionality with HTTP auth
	cloneURL := fmt.Sprintf("https://github.com/%s.git", config.repository)

	gitAuth := dag.SetSecret("git-auth", config.githubToken)
	gitRepo := dag.Git(cloneURL, dagger.GitOpts{
		KeepGitDir:       true,
		HTTPAuthUsername: config.githubToken, // For GitHub, token can be username
		HTTPAuthToken:    gitAuth,
	})

	if config.commitSHA != "" && config.commitSHA != "undefined" {
		log.Printf("Checking out commit: %s", config.commitSHA)

		return gitRepo.Commit(config.commitSHA).Tree()
	}

	return gitRepo.Branch("HEAD").Tree()
}

func decodeSourceData(dag *dagger.Client, sourceBase64 string) *dagger.Directory {
	log.Printf("Using uploaded source data")
	sourceData, err := base64.StdEncoding.DecodeString(sourceBase64)
	if err != nil {
		log.Fatalf("Failed to decode source data: %v", err)
	}

	return dag.Directory().WithNewFile("workflows.tar.gz", string(sourceData))
}

func setupLLMEnvironment(llmAPIKey string) {
	if err := os.Setenv("OPENAI_API_KEY", llmAPIKey); err != nil {
		log.Printf("Warning: Failed to set OPENAI_API_KEY: %v", err)
	}
	if err := os.Setenv("ANTHROPIC_API_KEY", llmAPIKey); err != nil {
		log.Printf("Warning: Failed to set ANTHROPIC_API_KEY: %v", err)
	}
	if err := os.Setenv("GEMINI_API_KEY", llmAPIKey); err != nil {
		log.Printf("Warning: Failed to set GEMINI_API_KEY: %v", err)
	}
}

func incrementUsage(repository string, success bool) error {
	serviceURL := os.Getenv("SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "https://workflow-scanner-36bg3tpnra-lz.a.run.app"
	}

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		return fmt.Errorf("API_KEY environment variable not set")
	}

	requestBody := map[string]interface{}{
		"repository": repository,
		"success":    success,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", serviceURL+"/api/increment-usage", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("increment usage failed with status: %d", resp.StatusCode)
	}

	log.Printf("Usage incremented successfully for repository: %s", repository)
	return nil
}

func runScan(ctx context.Context, dag *dagger.Client, config batchConfig, sourceDir *dagger.Directory) {
	daggerClient := daggerImpl.NewClient(dag)
	zizmor := zizmor.NewZizmor(daggerClient)
	agent := agent.NewAgent(daggerClient)
	githubTokenSecret := dag.SetSecret("github-token", config.githubToken)
	githubClient := github.NewWrapperIssueClientImpl(dag.GithubIssue(dagger.GithubIssueOpts{Token: githubTokenSecret}))

	prURL, err := scanAndFixWorkflows(ctx, config.repository, sourceDir, zizmor, agent, githubClient)
	
	// Increment usage regardless of scan success/failure (user still consumed quota)
	usageErr := incrementUsage(config.repository, err == nil)
	if usageErr != nil {
		log.Printf("Warning: Failed to increment usage: %v", usageErr)
		// Don't fail the scan for usage tracking errors
	}

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
