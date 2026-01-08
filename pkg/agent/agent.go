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
	"strings"
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

	promptContent, err := agent.loadPromptContent()
	if err != nil {
		return source, "", err
	}

	sourceWithPrompt := source.WithNewFile("llm_fix_prompt.md", string(promptContent))

	llmAPIKey, err := agent.getLLMAPIKey()
	if err != nil {
		return source, "", err
	}

	llmContainer := agent.createLLMContainer(sourceWithPrompt, llmAPIKey, issues)

	explanations, err := agent.executeLLMContainer(ctx, llmContainer)
	if err != nil {
		return source, "", err
	}

	modifiedDirectory := llmContainer.Directory("/workspace")

	return modifiedDirectory, explanations, nil
}

func (agent *AgentImpl) loadPromptContent() ([]byte, error) {
	if llmFixPrompt != "" {
		return []byte(llmFixPrompt), nil
	}

	projectRoot, err := agent.findProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}

	rootFS := os.DirFS(projectRoot)
	promptContent, err := fs.ReadFile(rootFS, "llm_fix_prompt.md")
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt file from project root: %w", err)
	}

	return promptContent, nil
}

func (agent *AgentImpl) findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
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

	return projectRoot, nil
}

func (agent *AgentImpl) getLLMAPIKey() (string, error) {
	// Check for specific API keys and validate their formats
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		if !strings.HasPrefix(openaiKey, "sk-") {
			return "", fmt.Errorf("OPENAI_API_KEY appears to be invalid format (should start with 'sk-')")
		}
		return openaiKey, nil
	}
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		if !strings.HasPrefix(anthropicKey, "sk-ant-") {
			return "", fmt.Errorf("ANTHROPIC_API_KEY appears to be invalid format (should start with 'sk-ant-')")
		}
		return anthropicKey, nil
	}
	if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
		if strings.HasPrefix(geminiKey, "sk-") {
			if strings.HasPrefix(geminiKey, "sk-ant-") {
				return "", fmt.Errorf("GEMINI_API_KEY appears to be an Anthropic key (starts with 'sk-ant-'), please use anthropic-api-key input instead")
			}
			return "", fmt.Errorf("GEMINI_API_KEY appears to be an OpenAI key (starts with 'sk-'), please use openai-api-key input instead")
		}
		if !strings.HasPrefix(geminiKey, "AIza") {
			return "", fmt.Errorf("GEMINI_API_KEY appears to be invalid format (should start with 'AIza')")
		}
		return geminiKey, nil
	}

	return "", fmt.Errorf("no API key found (need OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY)")
}

func (agent *AgentImpl) createLLMContainer(sourceWithPrompt *internalDagger.Directory, llmAPIKey, issues string) dagger.Container {
	log.Printf("DEBUG: Using custom container approach with LLM API key")
	log.Printf("DEBUG: Creating container with API key (length: %d)", len(llmAPIKey))
	log.Printf("DEBUG: ZIZMOR issues length: %d", len(issues))

	llmProcessorContent := GetLLMProcessorCode()

	container := agent.client.Container().
		From("golang:1.25-alpine").
		WithExec([]string{"apk", "add", "--no-cache", "git"}).
		WithEnvVariable("ZIZMOR_ISSUES", issues).
		WithDirectory("/workspace", sourceWithPrompt).
		WithWorkdir("/workspace").
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Workspace contents:' && ls -la"})

	// Set all API keys from environment
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		container = container.WithEnvVariable("OPENAI_API_KEY", openaiKey)
	}
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		container = container.WithEnvVariable("ANTHROPIC_API_KEY", anthropicKey)
	}
	if geminiKey := os.Getenv("GEMINI_API_KEY"); geminiKey != "" {
		container = container.WithEnvVariable("GEMINI_API_KEY", geminiKey)
	}
	if model := os.Getenv("MODEL"); model != "" {
		container = container.WithEnvVariable("MODEL", model)
	}

	return container.
		WithExec([]string{"rm", "-f", "go.mod", "go.sum"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Initializing Go module' && go mod init llm-processor"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Getting OpenAI Go client' && go get github.com/sashabaranov/go-openai"}).
		WithNewFile("/workspace/main.go", llmProcessorContent).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Running go mod tidy' && go mod tidy"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Environment variables:' && printenv | grep -E '(OPENAI|ZIZMOR)'"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: main.go size:' && wc -l main.go"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Running Go program' && go run main.go 2>&1"})
}

func (agent *AgentImpl) executeLLMContainer(ctx context.Context, llmContainer dagger.Container) (string, error) {
	log.Printf("DEBUG: Container pipeline created, executing...")

	explanations, err := llmContainer.Stdout(ctx)
	if err != nil {
		log.Printf("ERROR: Custom LLM container failed: %v", err)

		return "", fmt.Errorf("custom LLM processing failed: %w", err)
	}

	log.Printf("DEBUG: Custom LLM container completed successfully")

	return explanations, nil
}
