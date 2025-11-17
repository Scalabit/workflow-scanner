package main

import (
	"context"
	"fmt"
	"strings"

	"dagger/workflow-scanner/internal/dagger"
)

type WorkflowScanner struct{}

// Scan GitHub Actions workflows for security vulnerabilities and create a PR with fixes
func (m *WorkflowScanner) ScanAndFixWorkflows(
	ctx context.Context,
	// Github Token with permissions to write issues and contents
	githubToken *dagger.Secret,
	// Github repository url
	repository string,
	// +defaultPath="/"
	source *dagger.Directory,
) (string, error) {
	autoFixedDirectory, zizmorOutput, err := m.runZizmorAutoFix(ctx, source)
	if err != nil {
		return "", fmt.Errorf("failed to run ZIZMOR auto-fix: %w", err)
	}

	remainingIssues, err := m.checkRemainingIssues(ctx, autoFixedDirectory)
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
	finalValidation, err := m.checkRemainingIssues(ctx, finalDirectory)
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
	externalRepoFindings, err := m.scanExternalDependencies(ctx, finalDirectory)
	if err != nil {
		externalRepoFindings = fmt.Sprintf("Failed to scan external dependencies: %s", err.Error())
	}

	// Truncate external findings if too long to fit GitHub's 65,536 char limit
	maxExternalLength := 20000 // Leave room for other content
	if len(externalRepoFindings) > maxExternalLength {
		externalRepoFindings = externalRepoFindings[:maxExternalLength] + "\n\n... (truncated due to length - see full scan in workflow logs)"
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
*Automated security audit by ZIZMOR + AI analysis*`, zizmorOutput, llmExplanations, statusEmoji, statusText, validationStatus, externalRepoFindings)
	
	pr := issueClient.CreatePullRequest(repository, title, body, finalDirectory)

	return pr.URL(ctx)
}

// getZizmorContainer returns a container with ZIZMOR pre-installed and workspace mounted
func (m *WorkflowScanner) getZizmorContainer(source *dagger.Directory) *dagger.Container {
	// Use Python slim for reliable pip install - more predictable than Rust compilation
	return dag.Container().
		From("python:3.12-slim").
		WithExec([]string{"pip", "install", "zizmor"}).
		WithExec([]string{"sh", "-c", "which zizmor && zizmor --version"}).  // Verify installation
		WithDirectory("/workspace", source).
		WithWorkdir("/workspace")
}

func (m *WorkflowScanner) runZizmorAutoFix(ctx context.Context, source *dagger.Directory) (*dagger.Directory, string, error) {
	container := m.getZizmorContainer(source).
		WithExec([]string{"sh", "-c", "zizmor --fix=all .github/workflows/ 2>&1 || true"})
	
	output, err := container.Stdout(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to run ZIZMOR: %w", err)
	}
	
	fixedDirectory := container.Directory("/workspace")
	return fixedDirectory, output, nil
}

func (m *WorkflowScanner) checkRemainingIssues(ctx context.Context, source *dagger.Directory) (string, error) {
	container := m.getZizmorContainer(source).
		WithExec([]string{"sh", "-c", "zizmor --format=json .github/workflows/ 2>/dev/null || echo '[]'"})
	
	output, err := container.Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}
	
	output = strings.TrimSpace(output)

	if output == "" || output == "[]" || output == "[]\n" {
		return "", nil
	}
	
	return output, nil
}

func (m *WorkflowScanner) fixRemainingIssuesWithLLM(ctx context.Context, source *dagger.Directory, issues string) (*dagger.Directory, string, error) {
	// Only skip LLM if truly no issues found
	if issues == "" || issues == "[]" || issues == "[]\n" {
		return source.WithoutDirectory("node_modules"), "No remaining issues found after ZIZMOR auto-fix", nil
	}

	// Break the container chain by copying through a fresh container
	// This prevents workspace module from inheriting the zizmor auto-fix container lineage
	cleanSource := dag.Container().
		From("alpine:latest").
		WithDirectory("/clean", source).
		Directory("/clean")

	environment := dag.Env().
		WithStringInput("zizmor_issues", issues, "ZIZMOR scan results showing remaining security issues to fix").
		WithWorkspaceInput(
			"workspace",
			dag.Workspace(cleanSource),
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
	
	// Only try to get workspace if explanations succeeded
	completedWorkspace := workEnv.Output("completed").AsWorkspace()
	completed := completedWorkspace.Source()
	
	// Break the container chain on OUTPUT by copying through fresh container
	// This ensures LLM's writes are materialized and independent
	cleanCompleted := dag.Container().
		From("alpine:latest").
		WithDirectory("/output", completed).
		Directory("/output")
	
	return cleanCompleted.WithoutDirectory("node_modules"), explanations, nil
}

func (m *WorkflowScanner) scanExternalDependencies(ctx context.Context, source *dagger.Directory) (string, error) {
	// Create a container with git and ZIZMOR but NOT mount the source (we'll clone external repos)
	baseContainer := dag.Container().
		From("python:3.12-slim").
		WithExec([]string{"pip", "install", "zizmor"}).
		WithExec([]string{"apt-get", "update"}).
		WithExec([]string{"apt-get", "install", "-y", "git"}).
		WithDirectory("/workspace", source).  // Only for extracting repo list
		WithWorkdir("/tmp/external-scans")    // Work in temp dir for clones

	// Find all external repositories used in workflows (run in workspace dir)
	findReposCmd := `cd /workspace && find .github/workflows -name "*.yml" -o -name "*.yaml" | xargs grep -h "uses:" | grep -v "^#" | sed 's/.*uses: *//g' | sed 's/@.*//g' | grep "/" | sort -u`
	
	reposOutput, err := baseContainer.WithExec([]string{"sh", "-c", findReposCmd}).Stdout(ctx)
	if err != nil {
		return "Failed to extract external repositories", err
	}

	if reposOutput == "" {
		return "No external repositories found in workflows", nil
	}

	// Scan each external repository
	var allFindings strings.Builder
	repos := strings.Split(strings.TrimSpace(reposOutput), "\n")
	
	for _, repo := range repos {
		if repo == "" {
			continue
		}
		
		// Clone external repo and scan with our pre-built ZIZMOR container
		repoURL := fmt.Sprintf("https://github.com/%s", repo)
		tempDir := strings.ReplaceAll(repo, "/", "-")
		
		// Step 1: Clone the repo with error handling
		cloneContainer := baseContainer.WithExec([]string{"sh", "-c", fmt.Sprintf("git clone --depth=1 %s %s 2>&1 || echo 'Clone failed for %s'", repoURL, tempDir, repo)})
		
		// Step 2: Check what was cloned and scan
		scanCmd := fmt.Sprintf(`
			if [ -d %s ]; then
				echo "=== Successfully cloned %s ==="
				cd %s
				if [ -d .github/workflows ]; then 
					echo "Found workflows in %s, scanning..."
					zizmor --format=json .github/workflows/ 2>&1 || echo "ZIZMOR scan failed"
				else 
					echo "No .github/workflows directory in %s (normal for action repos)"
					echo "Contents:" && ls -la | head -10
				fi
			else
				echo "Failed to clone %s"
			fi
		`, tempDir, repo, tempDir, repo, repo, repo)
		
		findings, err := cloneContainer.WithExec([]string{"sh", "-c", scanCmd}).Stdout(ctx)
		if err != nil {
			allFindings.WriteString(fmt.Sprintf("### %s\nFailed to scan: %s\n\n", repo, err.Error()))
			continue
		}
		
		if findings != "" && findings != "No workflows found" && findings != "No .github/workflows directory" {
			allFindings.WriteString(fmt.Sprintf("### %s\n```json\n%s\n```\n\n", repo, findings))
		} else {
			allFindings.WriteString(fmt.Sprintf("### %s\nNo security issues found or no workflows present\n\n", repo))
		}
	}

	if allFindings.Len() == 0 {
		return "No external dependencies scanned", nil
	}

	// Summarize extensive findings
	fullReport := allFindings.String()
	summary := m.summarizeExternalFindings(fullReport)
	
	return summary, nil
}

// summarizeExternalFindings extracts key info from ZIZMOR JSON findings
func (m *WorkflowScanner) summarizeExternalFindings(fullReport string) string {
	var summary strings.Builder
	
	// Count total repos scanned
	repoCount := strings.Count(fullReport, "###")
	summary.WriteString(fmt.Sprintf("## External Dependencies Security Summary (%d repos scanned)\n\n", repoCount))
	
	lines := strings.Split(fullReport, "\n")
	currentRepo := ""
	
	for _, line := range lines {
		if strings.HasPrefix(line, "### ") {
			currentRepo = strings.TrimPrefix(line, "### ")
			continue
		}
		
		// Look for issue descriptions in JSON
		if strings.Contains(line, `"desc":`) {
			desc := strings.Split(line, `"desc": "`)[1]
			desc = strings.Split(desc, `"`)[0]
			summary.WriteString(fmt.Sprintf("- **%s**: %s\n", currentRepo, desc))
		}
		
		// Look for file paths in JSON
		if strings.Contains(line, `"given_path":`) {
			path := strings.Split(line, `"given_path": "`)[1] 
			path = strings.Split(path, `"`)[0]
			summary.WriteString(fmt.Sprintf("  - File: %s\n", path))
		}
	}
	
	// Truncate if too long
	result := summary.String()
	if len(result) > 40000 {
		result = result[:40000] + "\n\n... (truncated)"
	}
	
	return result
}

