package agent

import (
	"context"
	internalDagger "dagger/workflow-scanner/internal/dagger"
	"dagger/workflow-scanner/pkg/dagger"
)

type Agent interface {
	FixRemainingIssues(ctx context.Context, source *internalDagger.Directory, issues string) (*internalDagger.Directory, string, error)
}

type AgentImpl struct {
	client dagger.Client
}

func NewAgent(client dagger.Client) Agent {
	return &AgentImpl{
		client: client,
	}
}

func (agent *AgentImpl) FixRemainingIssues(ctx context.Context, source *internalDagger.Directory, issues string) (*internalDagger.Directory, string, error) {
	// Only skip LLM if truly no issues found
	if issues == "" || issues == "[]" || issues == "[]\n" {
		return source.WithoutDirectory("node_modules"), "No remaining issues found after ZIZMOR auto-fix", nil
	}

	environment := agent.client.Env().
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithWorkspaceInput(
			"workspace",
			agent.client.Workspace(source),
			"the workspace containing GitHub Actions workflows with remaining issues").
		WithWorkspaceOutput(
			"completed",
			"the workspace with remaining security vulnerabilities fixed").
		WithStringOutput(
			"explanations",
			"explanations of what fixes were applied and why")

	promptFile := agent.client.CurrentModule().Source().File("llm_fix_prompt.md")

	work := agent.client.LLM().
		WithEnv(environment).
		WithPromptFile(promptFile)

	// Try to execute the LLM and catch any failures early
	workEnv := work.Env()

	// Get explanations first (safer string operation)
	explanations, err := workEnv.Output("explanations").AsString(ctx)
	if err != nil {
		// If LLM fails completely, return original workspace
		return source.WithoutDirectory("node_modules"), "LLM processing failed - returning original workspace unchanged", nil
	}

	// Get the completed workspace from LLM
	completedWorkspace := workEnv.Output("completed").AsWorkspace()
	completed := completedWorkspace.Source()

	return completed.WithoutDirectory("node_modules"), explanations, nil
}
