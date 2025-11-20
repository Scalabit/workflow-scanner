package github

import (
	"context"
	"dagger/workflow-scanner/internal/dagger"
)

type WrapperIssueClient interface {
	CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory, opts ...dagger.GithubIssueCreatePullRequestOpts) (string, error)
}

type WrapperIssueClientImpl struct {
	githubIssueClient *dagger.GithubIssue
}

func NewWrapperIssueClientImpl(githubIssueClient *dagger.GithubIssue) WrapperIssueClient {
	return &WrapperIssueClientImpl{
		githubIssueClient: githubIssueClient,
	}
}

func (wrapperClient *WrapperIssueClientImpl) CreatePullRequest(ctx context.Context, repo string, title string, body string, source *dagger.Directory, opts ...dagger.GithubIssueCreatePullRequestOpts) (string, error) {
	return wrapperClient.githubIssueClient.CreatePullRequest(repo, title, body, source, opts...).URL(ctx)
}
