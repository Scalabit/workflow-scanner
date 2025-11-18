package main

import (
	"context"
	"fmt"

	"dagger/workflow-scanner/internal/dagger"
	"dagger/workflow-scanner/pkg/zizmor"
)

type WorkflowScanner struct{}

// Scan GitHub Actions workflows for security vulnerabilities and create a PR with fixes
func (m *WorkflowScanner) ScanAndFixWorkflows(ctx context.Context, githubToken *dagger.Secret, repository string, source *dagger.Directory) (string, error) {
	return scanAndFixWorflowsImpl(ctx, githubToken, repository, source, m)
}

func scanAndFixWorflowsImpl(ctx context.Context, githubToken *dagger.Secret, repository string, source *dagger.Directory, m *WorkflowScanner) (string, error) {
	zizmor := zizmor.NewZizmor(dag)

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
		finalDirectory, llmExplanations, err = m.fixRemainingIssuesWithLLM(ctx, autoFixedDirectory, remainingIssues)
		if err != nil {
			return "", fmt.Errorf("failed to fix remaining issues with LLM: %w", err)
		}
	} else {
		llmExplanations = "No remaining security issues found after ZIZMOR auto-fix"
	}

	// Run final validation scan on the fixed code
	finalValidation, err := zizmor.CheckRemainingIssues(ctx, finalDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to run final validation scan: %w", err)
	}

	validationStatus := ""
	if finalValidation == "" || finalValidation == "[]" || finalValidation == "[]\n" {
		validationStatus = "**All security issues resolved!** No vulnerabilities detected."
	} else {
		validationStatus = fmt.Sprintf("**Manual review needed - some issues remain:**\n```json\n%s\n```", finalValidation)
	}

	// Scan external repositories used in workflows
	fullRepoFindings, err := zizmor.ScanExternalDependencies(ctx, finalDirectory)
	summaryExternalFindings := zizmor.SummarizeExternalFindings(fullRepoFindings)
	if err != nil {
		summaryExternalFindings = fmt.Sprintf("Failed to scan external dependencies: %s", err.Error())
	}

	// Truncate external findings if too long to fit GitHub's 65,536 char limit
	maxExternalLength := 20000 // Leave room for other content
	if len(summaryExternalFindings) > maxExternalLength {
		summaryExternalFindings = summaryExternalFindings[:maxExternalLength] + "\n\n... (truncated due to length - see full scan in workflow logs)"
	}

	issueClient := dag.GithubIssue(dagger.GithubIssueOpts{Token: githubToken})

	// Determine pass/fail status for PR comment
	passed := finalValidation == "" || finalValidation == "[]" || finalValidation == "[]\n"
	statusEmoji := "✅"
	statusText := "PASSED"
	if !passed {
		statusEmoji = "❌"
		statusText = "NEEDS REVIEW"
	}

	title := "Security Audit & Fixes for GitHub Actions Workflows"
	body := fmt.Sprintf(`## Complete Security Audit Report

This PR contains comprehensive security analysis and fixes for GitHub Actions workflows.

### Auto-fixed by ZIZMOR
%s

### Manual Security Fixes Applied
%s

---

## %s Validation Report: %s

%s

---

### External Dependencies Security Scan
%s

---
*Automated security audit by ZIZMOR + AI analysis*`,
		zizmorOutput,
		llmExplanations,
		statusEmoji,
		statusText,
		validationStatus,
		summaryExternalFindings,
	)

	pr := issueClient.CreatePullRequest(repository, title, body, finalDirectory)

	return pr.URL(ctx)
}

func (m *WorkflowScanner) fixRemainingIssuesWithLLM(ctx context.Context, source *dagger.Directory, issues string) (*dagger.Directory, string, error) {
	// Only skip LLM if truly no issues found
	if issues == "" || issues == "[]" || issues == "[]\n" {
		return source.WithoutDirectory("node_modules"), "No remaining issues found after ZIZMOR auto-fix", nil
	}

	environment := dag.Env().
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithWorkspaceInput(
			"workspace",
			dag.Workspace(source),
			"the workspace containing GitHub Actions workflows with remaining issues").
		WithWorkspaceOutput(
			"completed",
			"the workspace with remaining security vulnerabilities fixed").
		WithStringOutput(
			"explanations",
			"explanations of what fixes were applied and why")

	promptFile := dag.CurrentModule().Source().File("llm_fix_prompt.md")

	work := dag.LLM().
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
