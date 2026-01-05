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

	if areThereIssues(issues) {
		log.Printf("DEBUG: No issues found, skipping LLM processing")

		return source, "No remaining issues found after ZIZMOR auto-fix", nil
	}

	// Create .env file with API key for the LLM container
	envContent := ""
	if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
		envContent += fmt.Sprintf("GEMINI_API_KEY=%s\n", geminiKey)
		log.Printf("DEBUG: Adding GEMINI_API_KEY to .env file")
	}
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		envContent += fmt.Sprintf("OPENAI_API_KEY=%s\n", openaiKey)
		log.Printf("DEBUG: Adding OPENAI_API_KEY to .env file")
	}
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		envContent += fmt.Sprintf("ANTHROPIC_API_KEY=%s\n", anthropicKey)
		log.Printf("DEBUG: Adding ANTHROPIC_API_KEY to .env file")
	}

	var promptContent []byte

	if llmFixPrompt != "" {
		promptContent = []byte(llmFixPrompt)
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

	}

	sourceWithPromptAndEnv := source.
		WithNewFile("llm_fix_prompt.md", string(promptContent)).
		WithNewFile(".env", envContent)

	environment := agent.client.Env().
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithStringInput("GO111MODULE", "on", "Enable Go modules").
		WithStringInput("GOWORK", "off", "Disable Go workspace mode").
		WithDirectoryInput(
			"workspace",
			sourceWithPromptAndEnv,
			"the workspace containing GitHub Actions workflows with remaining issues").
		WithDirectoryOutput(
			"completed",
			"the workspace with remaining security vulnerabilities fixed").
		WithStringOutput(
			"explanations",
			"explanations of what fixes were applied and why")

	promptFile := sourceWithPromptAndEnv.File("llm_fix_prompt.md")

	work := agent.client.LLM(internalDagger.LLMOpts{Model: "gemini-2.0-flash"}).
		WithEnv(environment).
		WithPromptFile(promptFile)

	workEnv := work.Env()

	explanations, err := workEnv.Output("explanations").AsString(ctx)
	if err != nil {
		fmt.Println("LLM ERROR: ", err)
		return source, "", fmt.Errorf("LLM processing failed: %w", err)
	}

	completedWorkspace := workEnv.Output("completed").AsWorkspace()
	completed := completedWorkspace.Source()

	return completed, explanations, nil
}
