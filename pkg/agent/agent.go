package agent

//go:generate mockgen -source=agent.go -destination=../../mocks/agent_mock.go -package=mocks Agent

import (
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	internalDagger "workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/dagger"
)

//go:embed llm_fix_prompt.md
var llmFixPrompt string

type Agent interface {
	FixRemainingIssues(ctx context.Context, source *internalDagger.Directory, issues string) (*internalDagger.Directory, string, error)
}

type AgentImpl struct {
	client dagger.Client
}

func NewAgentImpl(client dagger.Client) *AgentImpl {
	return &AgentImpl{
		client: client,
	}
}

func NewAgent(client dagger.Client) Agent {
	return &AgentImpl{
		client: client,
	}
}

func areThereIssues(issuesOut string) bool {
	return issuesOut == "" || issuesOut == "[]" || issuesOut == "[]\n"
}

func (agent *AgentImpl) FixRemainingIssues(ctx context.Context, source *internalDagger.Directory, issues string) (*internalDagger.Directory, string, error) {
	directory, llmOut, err := agent.fixRemainingIssuesImpl(ctx, source, issues)

	return directory.WithoutDirectory("node_modules"), llmOut, err
}

func (agent *AgentImpl) fixRemainingIssuesImpl(ctx context.Context, source *internalDagger.Directory, issues string) (*internalDagger.Directory, string, error) {
	log.Printf("DEBUG: Starting fixRemainingIssuesImpl with %d chars of issues", len(issues))

	// Only skip LLM if truly no issues found
	if areThereIssues(issues) {
		log.Printf("DEBUG: No issues found, skipping LLM processing")

		return source, "No remaining issues found after ZIZMOR auto-fix", nil
	}

	log.Printf("DEBUG: Setting up LLM environment with issues to fix")

	log.Printf("DEBUG: Creating Dagger environment...")

	// Set Gemini API key in process environment
	if apiKey := os.Getenv("LLM_API_KEY"); apiKey != "" {
		os.Setenv("GEMINI_API_KEY", apiKey)
	}

	geminiSecret := agent.client.SetSecret("GEMINI_API_KEY", os.Getenv("GEMINI_API_KEY"))
	llmApiKey := agent.client.SetSecret("LLM_API_KEY", os.Getenv("LLM_API_KEY"))

	environment := agent.client.Env().
		WithSecretInput("GEMINI_API_KEY", geminiSecret, "gemini API key").
		WithSecretInput("LLM_API_KEY", llmApiKey, "test key").
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithStringInput("GO111MODULE", "on", "Enable Go modules").
		WithStringInput("GOWORK", "off", "Disable Go workspace mode").
		WithDirectoryInput(
			"workspace",
			source,
			"the workspace containing GitHub Actions workflows with remaining issues").
		WithDirectoryOutput(
			"completed",
			"the workspace with remaining security vulnerabilities fixed").
		WithStringOutput(
			"explanations",
			"explanations of what fixes were applied and why")

	log.Printf("DEBUG: Environment created successfully")

	log.Printf("DEBUG: Obtaining prompt content (embedded or from project root)...")

	var promptContent []byte

	if llmFixPrompt != "" {
		promptContent = []byte(llmFixPrompt)
		log.Printf("DEBUG: Using embedded prompt: %d chars", len(promptContent))
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return source, "", fmt.Errorf("failed to get current working directory: %w", err)
		}

		projectRoot := cwd
		for {
			if _, err := os.Stat(projectRoot + "/go.mod"); err == nil {
				break
			}
			parent := projectRoot + "/.."
			if abs, err := filepath.Abs(parent); err != nil || abs == projectRoot {
				// Can't find project root, use current directory
				break
			} else {
				projectRoot = abs
			}
		}

		rootFS := os.DirFS(projectRoot)

		promptContent, err = fs.ReadFile(rootFS, "llm_fix_prompt.md")
		if err != nil {
			return source, "", fmt.Errorf("failed to read prompt file from project root: %w", err)
		}
		log.Printf("DEBUG: Successfully read prompt file: %d chars", len(promptContent))
	}

	log.Printf("DEBUG: Creating prompt file in source directory...")

	sourceWithPrompt := source.WithNewFile("llm_fix_prompt.md", string(promptContent))
	promptFile := sourceWithPrompt.File("llm_fix_prompt.md")
	log.Printf("DEBUG: Prompt file created in source directory")

	log.Printf("DEBUG: Creating LLM work instance...")
	work := agent.client.LLM(internalDagger.LLMOpts{Model: "gemini-2.0-flash"}).
		WithEnv(environment).
		WithPromptFile(promptFile)

	log.Printf("DEBUG: LLM work instance created successfully")

	log.Printf("DEBUG: Getting LLM work environment...")
	workEnv := work.Env().GetEnv()
	log.Printf("DEBUG: LLM work environment obtained")

	log.Printf("DEBUG: Requesting explanations from LLM...")
	explanations, err := workEnv.Output("explanations").AsString(ctx)
	if err != nil {
		log.Printf("ERROR: LLM explanations failed: %v", err)
		return source, "", fmt.Errorf("LLM processing failed: %w", err)
	}
	log.Printf("DEBUG: LLM explanations received: %d chars", len(explanations))

	log.Printf("DEBUG: Requesting completed workspace from LLM...")
	completedWorkspace := workEnv.Output("completed").AsWorkspace()
	completed := completedWorkspace.Source()
	log.Printf("DEBUG: LLM completed workspace obtained")

	log.Printf("DEBUG: LLM processing completed successfully")

	return completed, explanations, nil
}
