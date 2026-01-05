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

	environment := agent.client.Env().
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

	// Pass API keys from host environment to LLM container environment
	log.Printf("DEBUG: Checking for API keys in host environment...")
	if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
		log.Printf("DEBUG: Found GEMINI_API_KEY in host environment (length: %d)", len(geminiKey))
		geminiSecret := agent.client.SetSecret("gemini-api-key", geminiKey)
		environment = environment.WithSecretInput("GEMINI_API_KEY", geminiSecret, "Gemini API key for LLM operations")
		log.Printf("DEBUG: Added GEMINI_API_KEY secret to environment")
	} else {
		log.Printf("DEBUG: GEMINI_API_KEY not found in host environment")
	}
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		log.Printf("DEBUG: Found OPENAI_API_KEY in host environment (length: %d)", len(openaiKey))
		openaiSecret := agent.client.SetSecret("openai-api-key", openaiKey)
		environment = environment.WithSecretInput("OPENAI_API_KEY", openaiSecret, "OpenAI API key for LLM operations")
		log.Printf("DEBUG: Added OPENAI_API_KEY secret to environment")
	} else {
		log.Printf("DEBUG: OPENAI_API_KEY not found in host environment")
	}
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		log.Printf("DEBUG: Found ANTHROPIC_API_KEY in host environment (length: %d)", len(anthropicKey))
		anthropicSecret := agent.client.SetSecret("anthropic-api-key", anthropicKey)
		environment = environment.WithSecretInput("ANTHROPIC_API_KEY", anthropicSecret, "Anthropic API key for LLM operations")
		log.Printf("DEBUG: Added ANTHROPIC_API_KEY secret to environment")
	} else {
		log.Printf("DEBUG: ANTHROPIC_API_KEY not found in host environment")
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

	sourceWithPrompt := source.WithNewFile("llm_fix_prompt.md", string(promptContent))
	promptFile := sourceWithPrompt.File("llm_fix_prompt.md")

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
