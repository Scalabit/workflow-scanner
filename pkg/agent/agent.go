package agent

//go:generate mockgen -source=agent.go -destination=../../mocks/agent_mock.go -package=mocks Agent

import (
	"context"
	"fmt"
	internalDagger "workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/dagger"
)

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
	// Only skip LLM if truly no issues found
	if areThereIssues(issues) {
		return source, "No remaining issues found after ZIZMOR auto-fix", nil
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
		// If LLM fails completely, return error to caller
		return source, "", fmt.Errorf("LLM processing failed: %w", err)
	}

	// Get the completed workspace from LLM
	completedWorkspace := workEnv.Output("completed").AsWorkspace()
	completed := completedWorkspace.Source()

	return completed, explanations, nil
}
