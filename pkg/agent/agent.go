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

	// Use custom container approach instead of Dagger's LLM module
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		return source, "", fmt.Errorf("GEMINI_API_KEY not found in environment")
	}

	log.Printf("DEBUG: Using custom container approach with Gemini API key")

	// Get the LLM processor Go code
	llmProcessorContent := GetLLMProcessorCode()

	// Create custom container with Go and Gemini client
	log.Printf("DEBUG: Creating container with Gemini API key (length: %d)", len(geminiKey))
	log.Printf("DEBUG: ZIZMOR issues length: %d", len(issues))
	
	llmContainer := agent.client.Container().
		From("golang:1.25-alpine").
		WithExec([]string{"apk", "add", "--no-cache", "git"}).
		WithEnvVariable("GEMINI_API_KEY", geminiKey).
		WithEnvVariable("ZIZMOR_ISSUES", issues).
		WithDirectory("/workspace", sourceWithPrompt).
		WithWorkdir("/workspace").
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Workspace contents:' && ls -la"}).
		WithExec([]string{"rm", "-f", "go.mod", "go.sum"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Initializing Go module' && go mod init llm-processor"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Getting Gemini dependencies' && go get github.com/google/generative-ai-go/genai"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Getting Google API option' && go get google.golang.org/api/option"}).
		WithNewFile("/workspace/main.go", llmProcessorContent).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Environment variables:' && printenv | grep -E '(GEMINI|ZIZMOR)'"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: main.go size:' && wc -l main.go"}).
		WithExec([]string{"sh", "-c", "echo 'DEBUG: Running Go program' && go run main.go 2>&1"})
		
	log.Printf("DEBUG: Container pipeline created, executing...")

	explanations, err := llmContainer.Stdout(ctx)
	if err != nil {
		log.Printf("ERROR: Custom LLM container failed: %v", err)
		return source, "", fmt.Errorf("custom LLM processing failed: %w", err)
	}

	log.Printf("DEBUG: Custom LLM container completed successfully")

	// Get the modified workspace directory
	modifiedDirectory := llmContainer.Directory("/workspace")

	return modifiedDirectory, explanations, nil
}
