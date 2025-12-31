package agent

//go:generate mockgen -source=agent.go -destination=../../mocks/agent_mock.go -package=mocks Agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
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

	log.Printf("DEBUG: Creating workspace with Go module context...")
	workspace := agent.client.Workspace(source)
	log.Printf("DEBUG: Workspace created")

	environment := agent.client.Env().
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithStringInput("GO111MODULE", "on", "Enable Go modules").
		WithStringInput("GOWORK", "off", "Disable Go workspace mode").
		WithWorkspaceInput(
			"workspace",
			workspace,
			"the workspace containing GitHub Actions workflows with remaining issues").
		WithWorkspaceOutput(
			"completed",
			"the workspace with remaining security vulnerabilities fixed").
		WithStringOutput(
			"explanations",
			"explanations of what fixes were applied and why")

	log.Printf("DEBUG: Environment created successfully")

	log.Printf("DEBUG: Passing embedded prompt to LLM as a string input")

	promptContent := llmFixPrompt
	environment = environment.WithStringInput("llm_fix_prompt", promptContent, "LLM prompt for fixing remaining issues")

	log.Printf("DEBUG: Creating LLM work instance...")
	work := agent.client.LLM().WithEnv(environment)
	log.Printf("DEBUG: LLM work instance created successfully")

	log.Printf("DEBUG: Getting LLM work environment...")
	workEnv := work.Env()
	log.Printf("DEBUG: LLM work environment obtained")

	log.Printf("DEBUG: Requesting explanations from LLM...")
	explanations, err := workEnv.Output("explanations").AsString(ctx)
	if err != nil {
		return source, "", fmt.Errorf("LLM processing failed: %w", err)
	}
	log.Printf("DEBUG: LLM explanations received: %d chars", len(explanations))

	log.Printf("DEBUG: Requesting completed output from LLM (string)")
	completedStr, err := workEnv.Output("completed").AsString(ctx)
	if err != nil {
		return source, "", fmt.Errorf("LLM processing failed: %w", err)
	}
	log.Printf("DEBUG: LLM completed output received: %d chars", len(completedStr))

	type fileEdit struct {
		Path     string `json:"path"`
		Contents string `json:"contents"`
	}

	var edits []fileEdit
	if err := json.Unmarshal([]byte(completedStr), &edits); err != nil {
		log.Printf("DEBUG: completed output is not JSON edits: %v", err)
		return source, explanations, nil
	}

	if len(edits) == 0 {
		log.Printf("DEBUG: completed output contained zero edits")
		return source, explanations, nil
	}

	src := source
	for _, e := range edits {
		log.Printf("DEBUG: Applying edit to %s (len %d)", e.Path, len(e.Contents))
		src = src.WithNewFile(e.Path, e.Contents)

		back, err := src.File(e.Path).Contents(ctx)
		if err != nil {
			return source, "", fmt.Errorf("failed to verify written file %s: %w", e.Path, err)
		}
		if back != e.Contents {
			log.Printf("ERROR: verification mismatch for %s: expected %d got %d", e.Path, len(e.Contents), len(back))
			return source, "", fmt.Errorf("verification mismatch for %s", e.Path)
		}
		log.Printf("DEBUG: Verified write for %s", e.Path)
	}

	log.Printf("DEBUG: LLM processing completed and edits applied successfully")

	return src, explanations, nil
}
