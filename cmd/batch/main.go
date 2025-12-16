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
	log.Printf("Cloning repository %s", config.repository)

	cloneURL := fmt.Sprintf("https://%s@github.com/%s.git", config.githubToken, config.repository)

	container := dag.Container().
		From("alpine/git:latest").
		WithExec([]string{"git", "clone", cloneURL, "/workspace"})

	if config.commitSHA != "" && config.commitSHA != "undefined" {
		log.Printf("Checking out commit: %s", config.commitSHA)
		container = container.WithWorkdir("/workspace").
			WithExec([]string{"git", "checkout", config.commitSHA})
	}

	setupLLMEnvironment(config.llmAPIKey)

	return container.Directory("/workspace")
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

func runScan(ctx context.Context, dag *dagger.Client, config batchConfig, sourceDir *dagger.Directory) {
	daggerClient := daggerImpl.NewClient(dag)
	zizmor := zizmor.NewZizmor(daggerClient)
	agent := agent.NewAgent(daggerClient)
	githubTokenSecret := dag.SetSecret("github-token", config.githubToken)
	githubClient := github.NewWrapperIssueClientImpl(dag.GithubIssue(dagger.GithubIssueOpts{Token: githubTokenSecret}))

	prURL, err := scanAndFixWorkflows(ctx, config.repository, sourceDir, zizmor, agent, githubClient)
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
