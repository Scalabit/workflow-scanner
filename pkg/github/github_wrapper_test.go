package github

import (
	"context"
	"errors"
	"testing"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/mocks"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestWrapperIssueClient_Interface(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockGithub := mocks.NewMockWrapperIssueClient(ctrl)

	tests := []struct {
		name          string
		repo          string
		title         string
		body          string
		mockURL       string
		mockError     error
		expectedURL   string
		expectedError bool
		errorMessage  string
	}{
		{
			name:          "successful PR creation",
			repo:          "owner/repo",
			title:         "Security fixes",
			body:          "Fixed security issues",
			mockURL:       "https://github.com/owner/repo/pull/123",
			mockError:     nil,
			expectedURL:   "https://github.com/owner/repo/pull/123",
			expectedError: false,
		},
		{
			name:          "PR creation fails",
			repo:          "owner/repo",
			title:         "Security fixes",
			body:          "Fixed security issues",
			mockURL:       "",
			mockError:     errors.New("API rate limit exceeded"),
			expectedURL:   "",
			expectedError: true,
			errorMessage:  "API rate limit exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGithub.EXPECT().
				CreatePullRequest(gomock.Any(), tt.repo, tt.title, tt.body, gomock.Any()).
				Return(tt.mockURL, tt.mockError)

			result, err := mockGithub.CreatePullRequest(
				context.Background(),
				tt.repo,
				tt.title,
				tt.body,
				&dagger.Directory{},
			)

			assert.Equal(t, tt.expectedURL, result)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
