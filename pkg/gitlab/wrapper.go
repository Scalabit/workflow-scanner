package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/github"
)

// NewWrapperIssueClientImpl returns an implementation of github.WrapperIssueClient backed by GitLab
func NewWrapperIssueClientImpl(daggerClient *dagger.Client, gitlabToken string) github.WrapperIssueClient {
	return &WrapperIssueClientImpl{
		daggerClient: daggerClient,
		gitlabToken:  gitlabToken,
	}
}

type WrapperIssueClientImpl struct {
	daggerClient *dagger.Client
	gitlabToken  string
}

func (w *WrapperIssueClientImpl) CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory) (string, error) {
	branchName := fmt.Sprintf("workflow-security-fixes-%d", time.Now().Unix())

	repoURL := fmt.Sprintf("https://gitlab.com/%s.git", repo)
	gitAuth := w.daggerClient.SetSecret("gitlab-token", w.gitlabToken)
	gitRepo := w.daggerClient.Git(repoURL, dagger.GitOpts{
		KeepGitDir:       true,
		HTTPAuthUsername: "oauth2",
		HTTPAuthToken:    gitAuth,
	})

	mainBranch := gitRepo.Branch("main")

	workingDir := mainBranch.Tree().WithoutDirectory(".git")

	finalDir := workingDir.WithDirectory(".", source, dagger.DirectoryWithDirectoryOpts{
		Exclude: []string{".git"},
	})

	gitContainer := w.daggerClient.Container().From("alpine/git:latest").
		WithDirectory("/workspace", finalDir).
		WithWorkdir("/workspace").
		WithExec([]string{"git", "config", "--global", "user.name", "Workflow Security Bot"}).
		WithExec([]string{"git", "config", "--global", "user.email", "noreply@workflow-scanner.local"}).
		WithExec([]string{"git", "init"})

	remoteURL := fmt.Sprintf("https://oauth2:%s@gitlab.com/%s.git", w.gitlabToken, repo)
	gitContainer = gitContainer.WithExec([]string{"git", "remote", "add", "origin", remoteURL})

	gitContainer = gitContainer.
		WithExec([]string{"git", "fetch", "origin", "main"}).
		WithExec([]string{"git", "branch", branchName, "origin/main"}).
		WithExec([]string{"git", "symbolic-ref", "HEAD", "refs/heads/" + branchName}).
		WithExec([]string{"git", "reset"}).
		WithExec([]string{"git", "add", "."})

	statusOutput, err := gitContainer.WithExec([]string{"git", "status", "--porcelain"}).Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check git status: %w", err)
	}

	if strings.TrimSpace(statusOutput) == "" {
		return "", fmt.Errorf("no changes to commit - all security issues may already be fixed")
	}

	commitMsg := fmt.Sprintf("Security fixes: %s\n\n%s", title, "Automated security fixes applied by Workflow Scanner")
	gitContainer = gitContainer.WithExec([]string{"git", "commit", "-m", commitMsg}).
		WithExec([]string{"git", "push", "origin", branchName})

	pushOutput, err := gitContainer.Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to push branch %s: %w\nOutput: %s", branchName, err, pushOutput)
	}

	// Create merge request via GitLab API
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/merge_requests", url.PathEscape(repo))
	mrData := map[string]interface{}{
		"source_branch": branchName,
		"target_branch": "main",
		"title":         title,
		"description":   body,
	}
	jsonData, _ := json.Marshal(mrData)

	curlContainer := w.daggerClient.Container().From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "curl", "jq"}).
		WithNewFile("/tmp/mr.json", string(jsonData)).
		WithExec([]string{"curl", "-s", "-X", "POST", "-H", "Authorization: Bearer " + w.gitlabToken, "-H", "Content-Type: application/json", "-d", "@/tmp/mr.json", apiURL})

	resp, err := curlContainer.Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to create MR: %w", err)
	}

	// Try to parse web_url
	var mrResp struct {
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal([]byte(resp), &mrResp); err != nil {
		return "", fmt.Errorf("failed to parse MR response: %w - raw: %s", err, resp)
	}

	if mrResp.WebURL != "" {
		return mrResp.WebURL, nil
	}

	return "", fmt.Errorf("MR creation failed, response: %s", resp)
}

func timeNowUnix() int64 {
	return int64(0)
}
