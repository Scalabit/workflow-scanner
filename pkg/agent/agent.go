package agent

//go:generate mockgen -source=agent.go -destination=../../mocks/agent_mock.go -package=mocks Agent

import (
	"context"
	"fmt"
	"log"
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
	log.Printf("DEBUG: Starting fixRemainingIssuesImpl with %d chars of issues", len(issues))
	// Only skip LLM if truly no issues found
	if areThereIssues(issues) {
		log.Printf("DEBUG: No issues found, skipping LLM processing")
		return source, "No remaining issues found after ZIZMOR auto-fix", nil
	}
	log.Printf("DEBUG: Setting up LLM environment with issues to fix")

	log.Printf("DEBUG: Creating Dagger environment...")
	environment := agent.client.Env().
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithDirectoryInput(
			"source",
			source,
			"the directory containing GitHub Actions workflows with remaining issues").
		WithWorkspaceOutput(
			"completed",
			"the workspace with remaining security vulnerabilities fixed").
		WithStringOutput(
			"explanations",
			"explanations of what fixes were applied and why")

	log.Printf("DEBUG: Environment created successfully")

	log.Printf("DEBUG: Attempting to get current module and prompt file...")
	var promptFile *internalDagger.File
	var err error

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("ERROR: Panic during CurrentModule() call: %v", r)
				err = fmt.Errorf("panic during CurrentModule(): %v", r)
			}
		}()

		currentModule := agent.client.CurrentModule()
		log.Printf("DEBUG: CurrentModule() call succeeded")

		moduleSource := currentModule.Source()
		log.Printf("DEBUG: CurrentModule().Source() call succeeded")

		promptFile = moduleSource.File("llm_fix_prompt.md")
		log.Printf("DEBUG: Successfully got prompt file reference")
	}()

	if err != nil {
		return source, "", fmt.Errorf("failed to get current module or prompt file: %w", err)
	}

	log.Printf("DEBUG: Creating LLM work instance...")
	work := agent.client.LLM().WithEnv(environment).
		WithPromptFile(promptFile)
	log.Printf("DEBUG: LLM work instance created successfully")
	log.Printf("DEBUG: Getting LLM work environment...")
	// Try to execute the LLM and catch any failures early
	log.Printf("DEBUG: LLM work environment obtained")
	log.Printf("DEBUG: Requesting explanations from LLM...")
	// Get explanations first (safer string operation)
	explanations, err := work.Env().Output("explanations").AsString(ctx)
	if err != nil {
		log.Printf("ERROR: LLM explanations failed: %v", err)
		// If LLM fails completely, return error to caller
		return source, "", fmt.Errorf("LLM processing failed: %w", err)
	}

	log.Printf("DEBUG: LLM explanations received: %d chars", len(explanations))
	log.Printf("DEBUG: Requesting completed workspace from LLM...")
	// Get the completed workspace from LLM
	completedWorkspace := work.Env().Output("completed").AsWorkspace()
	completed := completedWorkspace.Source()
	log.Printf("DEBUG: LLM completed workspace obtained")
	log.Printf("DEBUG: LLM processing completed successfully")

	return completed, explanations, nil
}
