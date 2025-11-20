package main

import (
	"context"
	"fmt"
	"os"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/agent"
	daggerImpl "workflow-scanner/pkg/dagger"
	"workflow-scanner/pkg/github"
	"workflow-scanner/pkg/zizmor"
)

type WorkflowScanner struct{}

func (m *WorkflowScanner) ScanAndFixWorkflows(ctx context.Context, githubToken *dagger.Secret, repository string, source *dagger.Directory) (string, error) {
	daggerClient := daggerImpl.NewClient(dag)
	zizmor := zizmor.NewZizmor(daggerClient)
	agent := agent.NewAgent(daggerClient)
	githubClient := github.NewWrapperIssueClientImpl(dag.GithubIssue(dagger.GithubIssueOpts{Token: githubToken}))

	return scanAndFixWorflowsImpl(ctx, repository, source, zizmor, agent, githubClient)
}

func (m *WorkflowScanner) RunZizmorAutoFix(ctx context.Context, source *dagger.Directory) (string, error) {
	daggerClient := daggerImpl.NewClient(dag)

	return runZizmorAutoFixImpl(ctx, source, zizmor.NewZizmor(daggerClient))
}

func (m *WorkflowScanner) RunZizmorCheck(ctx context.Context, source *dagger.Directory) (string, error) {
	daggerClient := daggerImpl.NewClient(dag)

	return runZizmorCheckImpl(ctx, source, zizmor.NewZizmor(daggerClient))
}

func (m *WorkflowScanner) RunAgent(ctx context.Context, source *dagger.Directory) (string, error) {
	daggerClient := daggerImpl.NewClient(dag)

	return runAgentImpl(ctx, source, agent.NewAgent(daggerClient))
}

func (m *WorkflowScanner) RunZizmorExternalDependencies(ctx context.Context, source *dagger.Directory) (string, error) {
	daggerClient := daggerImpl.NewClient(dag)

	return runZizmorExternalDependenciesImpl(ctx, source, zizmor.NewZizmor(daggerClient))
}

func (m *WorkflowScanner) SummarizeAndPush(ctx context.Context, repository string, source *dagger.Directory, githubToken *dagger.Secret) (string, error) {
	daggerClient := daggerImpl.NewClient(dag)

	return summarizeAndPushImpl(
		ctx,
		repository,
		source,
		zizmor.NewZizmor(daggerClient),
		github.NewWrapperIssueClientImpl(
			dag.GithubIssue(dagger.GithubIssueOpts{Token: githubToken}),
		),
	)
}

func scanAndFixWorflowsImpl(ctx context.Context, repository string, source *dagger.Directory, zizmor zizmor.Zizmor, agent agent.Agent, githubClient github.WrapperIssueClient) (string, error) {
	_, err := runZizmorAutoFixImpl(ctx, source, zizmor)
	if err != nil {
		return "", fmt.Errorf("failed to run ZIZMOR auto-fix: %w", err)
	}

	_, err = runZizmorCheckImpl(ctx, source, zizmor)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}

	_, err = runAgentImpl(ctx, source, agent)
	if err != nil {
		return "", fmt.Errorf("failed to fix issues with LLM: %w", err)
	}

	_, err = runZizmorExternalDependenciesImpl(ctx, source, zizmor)
	if err != nil {
		return "", fmt.Errorf("failed to check external dependencies: %w", err)
	}

	_, err = runZizmorCheckImpl(ctx, source, zizmor)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}

	return summarizeAndPushImpl(ctx, repository, source, zizmor, githubClient)
}

func runZizmorAutoFixImpl(ctx context.Context, source *dagger.Directory, zizmorClient zizmor.Zizmor) (string, error) {
	_, zizmorOutput, err := zizmorClient.Run(ctx, true, zizmor.Plain, source)
	if err != nil {
		return "", fmt.Errorf("failed to run ZIZMOR auto-fix: %w", err)
	}
	//_, err = autoFixedDirectory.Export(ctx, "/home/sabrina/git/workflow-scanner/")

	fmt.Println("LKJSDFHGKLJSDG")

	err = os.WriteFile("zizmor_autofix.out", []byte(zizmorOutput), os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to write zizmor_autofix.out: %w", err)
	}

	return zizmorOutput, err
}

func runZizmorCheckImpl(ctx context.Context, source *dagger.Directory, zizmorClient zizmor.Zizmor) (string, error) {
	_, zizmorOutput, err := zizmorClient.Run(ctx, false, zizmor.Json, source)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}

	err = os.WriteFile("zizmor.out", []byte(zizmorOutput), os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to write zizmor_precheck_remaining.out: %w", err)
	}

	return zizmorOutput, nil
}

func runAgentImpl(ctx context.Context, source *dagger.Directory, agent agent.Agent) (string, error) {
	bZizmorOut, err := os.ReadFile("zizmor.out")
	if err != nil {
		return "", fmt.Errorf("failed read zizmor reported issues: %w", err)
	}

	zizmorOut := string(bZizmorOut)

	llmOut := "No remaining security issues found after ZIZMOR auto-fix"
	if zizmorOut != "" && zizmorOut != "[]" && zizmorOut != "[]\n" {
		_, llmOut, err := agent.FixRemainingIssues(ctx, source, zizmorOut) // rename it to FixIssues
		if err != nil {
			return "", fmt.Errorf("failed to fix remaining issues with LLM: %w", err)
		}
		//_, err = llmDirectory.Export(ctx, ".")

		return llmOut, nil
	}

	err = os.WriteFile("llm.out", []byte(llmOut), os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to write llm.out: %w", err)
	}

	return llmOut, nil
}

func runZizmorExternalDependenciesImpl(ctx context.Context, source *dagger.Directory, zizmor zizmor.Zizmor) (string, error) {
	fullRepoFindings, err := zizmor.ScanExternalDependencies(ctx, source)
	if err != nil {
		return "", fmt.Errorf("current output: \"%s\"\nfailed to scan external dependencies: %w", fullRepoFindings, err)
	}

	err = os.WriteFile("zizmor_external.out", []byte(fullRepoFindings), os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("failed to write zizmor_external.out: %w", err)
	}

	return fullRepoFindings, nil
}

func readFilesForSummary() (string, string, string, string, error) {
	bExternalFindings, err := os.ReadFile("zizmor_external.out")
	if err != nil {
		return "", "", "", "", fmt.Errorf("external findings report file not found: %w", err)
	}

	bAutoFix, err := os.ReadFile("zizmor_autofix.out")
	if err != nil {
		return "", "", "", "", fmt.Errorf("auto fix report file not found: %w", err)
	}

	bRemainingIssues, err := os.ReadFile("zizmor.out")
	if err != nil {
		return "", "", "", "", fmt.Errorf("remaining issues report file not found: %w", err)
	}

	bLlm, err := os.ReadFile("llm.out")
	if err != nil {
		return "", "", "", "", fmt.Errorf("llm report file not found: %w", err)
	}

	return string(bExternalFindings), string(bAutoFix), string(bRemainingIssues), string(bLlm), nil
}

func summarizeAndPushImpl(ctx context.Context, repository string, source *dagger.Directory, zizmor zizmor.Zizmor, githubClient github.WrapperIssueClient) (string, error) {
	externalFindings, autoFix, remainingIssues, llm, err := readFilesForSummary()
	if err != nil {
		return "", fmt.Errorf("failed to read dependent files: %w", err)
	}

	summaryExternalFindings := zizmor.SummarizeExternalFindings(externalFindings)
	// Truncate external findings if too long to fit GitHub's 65,536 char limit
	maxExternalLength := 20000 // Leave room for other content
	if len(summaryExternalFindings) > maxExternalLength {
		summaryExternalFindings = summaryExternalFindings[:maxExternalLength] +
			"\n\n... (truncated due to length - see full scan in workflow logs)"
	}

	prTitle, prBody := github.GetPrTitleBody(remainingIssues, autoFix, llm, summaryExternalFindings)

	return githubClient.CreatePullRequest(ctx, repository, prTitle, prBody, source)
}
