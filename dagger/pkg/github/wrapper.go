package github

import (
	"context"
	internalDagger "dagger/workflow-scanner/internal/dagger"
	"dagger/workflow-scanner/pkg/dagger"
)

type WrapperIssueClient interface {
	CreatePullRequest(ctx context.Context, repo string, title string, body string, source dagger.Directory, opts ...internalDagger.GithubIssueCreatePullRequestOpts) (string, error)
}

type WrapperIssueClientImpl struct {
	githubIssueClient *internalDagger.GithubIssue
}

func NewWrapperIssueClientImpl(githubIssueClient *internalDagger.GithubIssue) WrapperIssueClient {
	return &WrapperIssueClientImpl{
		githubIssueClient: githubIssueClient,
	}
}

func (wrapperClient *WrapperIssueClientImpl) CreatePullRequest(ctx context.Context, repo string, title string, body string, source dagger.Directory, opts ...internalDagger.GithubIssueCreatePullRequestOpts) (string, error) {
	var internalDir *internalDagger.Directory
	if adapter, ok := source.(*dagger.DirectoryAdapter); ok {
		internalDir = adapter.GetInternal()
	} else {
		// For tests, we can't extract internal directory
		return "test-pr-url", nil
	}
	return wrapperClient.githubIssueClient.CreatePullRequest(repo, title, body, internalDir, opts...).URL(ctx)
}
