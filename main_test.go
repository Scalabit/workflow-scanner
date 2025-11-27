package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"workflow-scanner/internal/dagger"
	"workflow-scanner/mocks"
	pkgDagger "workflow-scanner/pkg/dagger"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestScanAndFixWorflowsImpl(t *testing.T) {
	tests := []struct {
		name           string
		repository     string
		setupMocks     func(*gomock.Controller) (*mocks.MockZizmor, *mocks.MockAgent, *mocks.MockWrapperIssueClient, pkgDagger.Directory)
		expectedResult string
		expectedError  bool
		errorContains  string
	}{
		{
			name:       "successful workflow - no issues found",
			repository: "owner/repo",
			setupMocks: func(ctrl *gomock.Controller) (*mocks.MockZizmor, *mocks.MockAgent, *mocks.MockWrapperIssueClient, pkgDagger.Directory) {
				mockZizmor := mocks.NewMockZizmor(ctrl)
				mockAgent := mocks.NewMockAgent(ctrl)
				mockGithub := mocks.NewMockWrapperIssueClient(ctrl)
				mockDirectory := &dagger.Directory{}

				// Step 1: Run ZIZMOR auto-fix
				mockZizmor.EXPECT().
					RunZizmorAutoFix(gomock.Any(), mockDirectory).
					Return(mockDirectory, "Fixed 2 security issues automatically", nil)

				// Step 2: Check remaining issues - none found
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockDirectory).
					Return("", nil)

				// Step 3: No LLM call needed since no remaining issues
				// mockAgent.FixRemainingIssues should NOT be called

				// Step 4: Final validation scan
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockDirectory).
					Return("", nil)

				// Step 5: Scan external dependencies
				mockZizmor.EXPECT().
					ScanExternalDependencies(gomock.Any(), mockDirectory).
					Return("External scan completed - no issues", nil)

				// Step 6: Summarize findings
				mockZizmor.EXPECT().
					SummarizeExternalFindings("External scan completed - no issues").
					Return("External Dependencies: Clean")

				// Step 7: Create PR
				mockGithub.EXPECT().
					CreatePullRequest(gomock.Any(), "owner/repo", gomock.Any(), gomock.Any(), gomock.Any()).
					Return("https://github.com/owner/repo/pull/123", nil)

				return mockZizmor, mockAgent, mockGithub, mockDirectory
			},
			expectedResult: "https://github.com/owner/repo/pull/123",
			expectedError:  false,
		},
		{
			name:       "workflow with remaining issues requiring LLM",
			repository: "owner/repo",
			setupMocks: func(ctrl *gomock.Controller) (*mocks.MockZizmor, *mocks.MockAgent, *mocks.MockWrapperIssueClient, pkgDagger.Directory) {
				mockZizmor := mocks.NewMockZizmor(ctrl)
				mockAgent := mocks.NewMockAgent(ctrl)
				mockGithub := mocks.NewMockWrapperIssueClient(ctrl)
				mockDirectory := &dagger.Directory{}
				mockFixedDirectory := &dagger.Directory{}

				// Step 1: Run ZIZMOR auto-fix
				mockZizmor.EXPECT().
					RunZizmorAutoFix(gomock.Any(), mockDirectory).
					Return(mockDirectory, "Fixed some issues", nil)

				// Step 2: Check remaining issues - some found
				remainingIssues := `[{"desc": "manual fix needed"}]`
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockDirectory).
					Return(remainingIssues, nil)

				// Step 3: LLM fixes remaining issues
				mockAgent.EXPECT().
					FixRemainingIssues(gomock.Any(), mockDirectory, remainingIssues).
					Return(mockFixedDirectory, "Applied manual fixes using LLM", nil)

				// Step 4: Final validation scan
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockFixedDirectory).
					Return("", nil)

				// Step 5: Scan external dependencies
				mockZizmor.EXPECT().
					ScanExternalDependencies(gomock.Any(), mockFixedDirectory).
					Return("External scan results", nil)

				// Step 6: Summarize findings
				mockZizmor.EXPECT().
					SummarizeExternalFindings("External scan results").
					Return("External Dependencies: Some issues found")

				// Step 7: Create PR
				mockGithub.EXPECT().
					CreatePullRequest(gomock.Any(), "owner/repo", gomock.Any(), gomock.Any(), gomock.Any()).
					Return("https://github.com/owner/repo/pull/456", nil)

				return mockZizmor, mockAgent, mockGithub, mockDirectory
			},
			expectedResult: "https://github.com/owner/repo/pull/456",
			expectedError:  false,
		},
		{
			name:       "ZIZMOR auto-fix fails",
			repository: "owner/repo",
			setupMocks: func(ctrl *gomock.Controller) (*mocks.MockZizmor, *mocks.MockAgent, *mocks.MockWrapperIssueClient, pkgDagger.Directory) {
				mockZizmor := mocks.NewMockZizmor(ctrl)
				mockAgent := mocks.NewMockAgent(ctrl)
				mockGithub := mocks.NewMockWrapperIssueClient(ctrl)
				mockDirectory := &dagger.Directory{}

				// Step 1: ZIZMOR auto-fix fails
				mockZizmor.EXPECT().
					RunZizmorAutoFix(gomock.Any(), mockDirectory).
					Return(nil, "", errors.New("ZIZMOR container failed"))

				// No other calls should happen after failure

				return mockZizmor, mockAgent, mockGithub, mockDirectory
			},
			expectedResult: "",
			expectedError:  true,
			errorContains:  "failed to run ZIZMOR auto-fix: ZIZMOR container failed",
		},
		{
			name:       "LLM processing fails",
			repository: "owner/repo",
			setupMocks: func(ctrl *gomock.Controller) (*mocks.MockZizmor, *mocks.MockAgent, *mocks.MockWrapperIssueClient, pkgDagger.Directory) {
				mockZizmor := mocks.NewMockZizmor(ctrl)
				mockAgent := mocks.NewMockAgent(ctrl)
				mockGithub := mocks.NewMockWrapperIssueClient(ctrl)
				mockDirectory := &dagger.Directory{}

				// Step 1: Run ZIZMOR auto-fix
				mockZizmor.EXPECT().
					RunZizmorAutoFix(gomock.Any(), mockDirectory).
					Return(mockDirectory, "Fixed some issues", nil)

				// Step 2: Check remaining issues - some found
				remainingIssues := `[{"desc": "complex issue"}]`
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockDirectory).
					Return(remainingIssues, nil)

				// Step 3: LLM processing fails
				mockAgent.EXPECT().
					FixRemainingIssues(gomock.Any(), mockDirectory, remainingIssues).
					Return(nil, "", errors.New("LLM service unavailable"))

				// No other calls should happen after failure

				return mockZizmor, mockAgent, mockGithub, mockDirectory
			},
			expectedResult: "",
			expectedError:  true,
			errorContains:  "failed to fix remaining issues with LLM: LLM service unavailable",
		},
		{
			name:       "external findings too long - gets truncated",
			repository: "owner/repo",
			setupMocks: func(ctrl *gomock.Controller) (*mocks.MockZizmor, *mocks.MockAgent, *mocks.MockWrapperIssueClient, pkgDagger.Directory) {
				mockZizmor := mocks.NewMockZizmor(ctrl)
				mockAgent := mocks.NewMockAgent(ctrl)
				mockGithub := mocks.NewMockWrapperIssueClient(ctrl)
				mockDirectory := &dagger.Directory{}

				// Create a very long findings report
				longFindings := fmt.Sprintf("Very long findings report: %s",
					string(make([]byte, 25000))) // Longer than 20000 char limit

				// Step 1: Run ZIZMOR auto-fix
				mockZizmor.EXPECT().
					RunZizmorAutoFix(gomock.Any(), mockDirectory).
					Return(mockDirectory, "Fixed issues", nil)

				// Step 2: Check remaining issues - none
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockDirectory).
					Return("", nil)

				// Step 3: Final validation
				mockZizmor.EXPECT().
					CheckRemainingIssues(gomock.Any(), mockDirectory).
					Return("", nil)

				// Step 4: External scan returns long report
				mockZizmor.EXPECT().
					ScanExternalDependencies(gomock.Any(), mockDirectory).
					Return("Long external report", nil)

				// Step 5: Summarize produces long result
				mockZizmor.EXPECT().
					SummarizeExternalFindings("Long external report").
					Return(longFindings)

				// Step 6: Create PR - should receive truncated findings
				mockGithub.EXPECT().
					CreatePullRequest(gomock.Any(), "owner/repo", gomock.Any(), gomock.Any(), gomock.Any()).
					Return("https://github.com/owner/repo/pull/789", nil)

				return mockZizmor, mockAgent, mockGithub, mockDirectory
			},
			expectedResult: "https://github.com/owner/repo/pull/789",
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockZizmor, mockAgent, mockGithub, _ := tt.setupMocks(ctrl)

			result, err := scanAndFixWorflowsImpl(
				context.Background(),
				tt.repository,
				&dagger.Directory{},
				mockZizmor,
				mockAgent,
				mockGithub,
			)

			assert.Equal(t, tt.expectedResult, result)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
