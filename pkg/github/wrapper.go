package github

//go:generate mockgen -source=wrapper.go -destination=../../mocks/github_wrapper_mock.go -package=mocks WrapperIssueClient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"workflow-scanner/internal/dagger"
)

type WrapperIssueClient interface {
	CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory, targetBranch string) (string, error)
}

type WrapperIssueClientImpl struct {
	daggerClient *dagger.Client
	githubToken  string
}

func NewWrapperIssueClientImpl(daggerClient *dagger.Client, githubToken string) WrapperIssueClient {
	return &WrapperIssueClientImpl{
		daggerClient: daggerClient,
		githubToken:  githubToken,
	}
}

func (w *WrapperIssueClientImpl) CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory, targetBranch string) (string, error) {
	branchName := fmt.Sprintf("workflow-security-fixes-%d", time.Now().Unix())

	finalDir, err := w.prepareRepository(repo, targetBranch, source)
	if err != nil {
		return "", err
	}

	err = w.commitAndPushChanges(ctx, finalDir, repo, branchName, targetBranch, title)
	if err != nil {
		return "", err
	}

	prURL, prNumber, err := w.createPullRequestAPI(ctx, repo, title, body, branchName, targetBranch)
	if err != nil {
		return "", err
	}

	w.addSemverLabelHelper(ctx, repo, prNumber)

	return prURL, nil
}

func (w *WrapperIssueClientImpl) prepareRepository(repo, targetBranch string, source *dagger.Directory) (*dagger.Directory, error) {
	repoURL := fmt.Sprintf("https://github.com/%s.git", repo)
	gitRepo := w.daggerClient.Git(repoURL, dagger.GitOpts{
		HTTPAuthToken:    w.daggerClient.SetSecret("github-token", w.githubToken),
		HTTPAuthUsername: "x-access-token",
	})

	mainBranch := gitRepo.Branch(targetBranch)
	workingDir := mainBranch.Tree().WithoutDirectory(".git")

	finalDir := workingDir.WithDirectory(".", source, dagger.DirectoryWithDirectoryOpts{
		Exclude: []string{".git"},
	})

	return finalDir, nil
}

func (w *WrapperIssueClientImpl) commitAndPushChanges(ctx context.Context, finalDir *dagger.Directory, repo, branchName, targetBranch, title string) error {
	gitContainer := w.daggerClient.Container().
		From("alpine/git:latest").
		WithDirectory("/workspace", finalDir).
		WithWorkdir("/workspace").
		WithExec([]string{"git", "config", "--global", "user.name", "Workflow Security Bot"}).
		WithExec([]string{"git", "config", "--global", "user.email", "noreply@github.com"}).
		WithExec([]string{"git", "init"})

	remoteURL := fmt.Sprintf("https://%s@github.com/%s.git", w.githubToken, repo)
	gitContainer = gitContainer.WithExec([]string{"git", "remote", "add", "origin", remoteURL})

	gitContainer = gitContainer.
		WithExec([]string{"git", "fetch", "origin", targetBranch}).
		WithExec([]string{"git", "branch", branchName, "origin/" + targetBranch}).
		WithExec([]string{"git", "symbolic-ref", "HEAD", "refs/heads/" + branchName}).
		WithExec([]string{"git", "reset"}).
		WithExec([]string{"git", "add", "."})

	statusOutput, err := gitContainer.WithExec([]string{"git", "status", "--porcelain"}).Stdout(ctx)
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(statusOutput) == "" {
		return fmt.Errorf("no changes to commit - all security issues may already be fixed")
	}

	commitMsg := fmt.Sprintf("Security fixes: %s\n\n%s", title, "Automated security fixes applied by Workflow Scanner")
	gitContainer = gitContainer.
		WithExec([]string{"git", "commit", "-m", commitMsg}).
		WithExec([]string{"git", "push", "origin", branchName})

	pushOutput, err := gitContainer.Stdout(ctx)
	if err != nil {
		return fmt.Errorf("failed to push branch %s: %w\nOutput: %s", branchName, err, pushOutput)
	}

	return nil
}

func (w *WrapperIssueClientImpl) createPullRequestAPI(ctx context.Context, repo, title, body, branchName, targetBranch string) (string, int, error) {
	prData := map[string]interface{}{
		"title": title,
		"body":  body,
		"head":  branchName,
		"base":  targetBranch,
	}

	jsonData, err := json.Marshal(prData)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal PR data: %w", err)
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls", repo)

	response := w.daggerClient.Container().
		From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "curl"}).
		WithSecretVariable("GITHUB_TOKEN", w.daggerClient.SetSecret("github-token", w.githubToken)).
		WithNewFile("/tmp/pr_data.json", string(jsonData)).
		WithExec([]string{"curl", "-X", "POST",
			"-H", "Authorization: Bearer " + w.githubToken,
			"-H", "Accept: application/vnd.github.v3+json",
			"-H", "Content-Type: application/json",
			"-d", "@/tmp/pr_data.json",
			apiURL})

	responseContent, err := response.Stdout(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create PR: %w", err)
	}

	var prResponse struct {
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
		Number  int    `json:"number"`
	}

	if err := json.Unmarshal([]byte(responseContent), &prResponse); err != nil {
		return "", 0, fmt.Errorf("failed to parse PR response: %w", err)
	}

	if prResponse.HTMLURL == "" {
		return "", 0, fmt.Errorf("PR creation failed, response: %s", responseContent)
	}

	return prResponse.HTMLURL, prResponse.Number, nil
}

func (w *WrapperIssueClientImpl) addSemverLabelHelper(ctx context.Context, repo string, prNumber int) {
	if prNumber <= 0 {
		return
	}

	labelData := map[string]interface{}{
		"labels": []string{"semver-minor"},
	}
	labelJSON, err := json.Marshal(labelData)
	if err != nil {
		fmt.Printf("Warning: failed to marshal label data: %v", err)

		return
	}

	labelURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/labels", repo, prNumber)

	labelResponse := w.daggerClient.Container().
		From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "curl"}).
		WithNewFile("/tmp/label_data.json", string(labelJSON)).
		WithExec([]string{"curl", "-X", "POST",
			"-H", "Authorization: Bearer " + w.githubToken,
			"-H", "Accept: application/vnd.github.v3+json",
			"-H", "Content-Type: application/json",
			"-d", "@/tmp/label_data.json",
			labelURL})

	_, labelErr := labelResponse.Stdout(ctx)
	if labelErr != nil {
		fmt.Printf("Warning: failed to add semver-minor label to PR #%d: %v", prNumber, labelErr)
	}
}
