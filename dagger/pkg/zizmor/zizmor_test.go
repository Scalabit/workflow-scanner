package zizmor

import (
	"context"
	"errors"
	"testing"

	"dagger/workflow-scanner/tests/mocks"
	internalDagger "dagger/workflow-scanner/internal/dagger"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestZizmorImpl_CheckRemainingIssues(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockContainer := mocks.NewMockContainer(ctrl)
	mockDirectory := &internalDagger.Directory{}

	tests := []struct {
		name           string
		containerOutput string
		containerError  error
		expectedResult string
		expectedError  string
	}{
		{
			name:           "no issues found - empty JSON array",
			containerOutput: "[]",
			containerError:  nil,
			expectedResult: "",
			expectedError:  "",
		},
		{
			name:           "issues found - valid JSON",
			containerOutput: `[{"desc": "security issue", "file": "workflow.yml"}]`,
			containerError:  nil,
			expectedResult: `[{"desc": "security issue", "file": "workflow.yml"}]`,
			expectedError:  "",
		},
		{
			name:           "container execution fails",
			containerOutput: "",
			containerError:  errors.New("container failed"),
			expectedResult: "",
			expectedError:  "failed to check remaining issues: container failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.EXPECT().Container().Return(mockContainer)
			mockContainer.EXPECT().From("python:3.12-slim").Return(mockContainer)
			mockContainer.EXPECT().WithExec([]string{"pip", "install", "zizmor"}).Return(mockContainer)
			mockContainer.EXPECT().WithExec([]string{"sh", "-c", "which zizmor && zizmor --version"}).Return(mockContainer)
			mockContainer.EXPECT().WithDirectory("/workspace", mockDirectory).Return(mockContainer)
			mockContainer.EXPECT().WithWorkdir("/workspace").Return(mockContainer)
			
			mockContainer.EXPECT().WithExec([]string{"sh", "-c", "zizmor --format=json .github/workflows/ 2>/dev/null || echo '[]'"}).Return(mockContainer)
			mockContainer.EXPECT().Stdout(gomock.Any()).Return(tt.containerOutput, tt.containerError)

			zizmor := NewZizmor(mockClient)
			result, err := zizmor.CheckRemainingIssues(context.Background(), mockDirectory)

			assert.Equal(t, tt.expectedResult, result)
			
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError, err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestZizmorImpl_RunZizmorAutoFix(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockContainer := mocks.NewMockContainer(ctrl)
	mockDirectory := &internalDagger.Directory{}

	tests := []struct {
		name           string
		containerOutput string
		containerError  error
		expectedOutput string
		expectedError  string
	}{
		{
			name:           "successful auto-fix execution",
			containerOutput: "Fixed 3 security vulnerabilities in workflows",
			containerError:  nil,
			expectedOutput: "Fixed 3 security vulnerabilities in workflows",
			expectedError:  "",
		},
		{
			name:           "auto-fix with no changes",
			containerOutput: "No security issues found to fix",
			containerError:  nil,
			expectedOutput: "No security issues found to fix",
			expectedError:  "",
		},
		{
			name:           "container execution fails",
			containerOutput: "",
			containerError:  errors.New("auto-fix execution failed"),
			expectedOutput: "",
			expectedError:  "failed to run ZIZMOR: auto-fix execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.EXPECT().Container().Return(mockContainer)
			mockContainer.EXPECT().From("python:3.12-slim").Return(mockContainer)
			mockContainer.EXPECT().WithExec([]string{"pip", "install", "zizmor"}).Return(mockContainer)
			mockContainer.EXPECT().WithExec([]string{"sh", "-c", "which zizmor && zizmor --version"}).Return(mockContainer)
			mockContainer.EXPECT().WithDirectory("/workspace", mockDirectory).Return(mockContainer)
			mockContainer.EXPECT().WithWorkdir("/workspace").Return(mockContainer)
			
			// Setup the auto-fix execution
			mockContainer.EXPECT().WithExec([]string{"sh", "-c", "zizmor --fix=all .github/workflows/ 2>&1 || true"}).Return(mockContainer)
			mockContainer.EXPECT().Stdout(gomock.Any()).Return(tt.containerOutput, tt.containerError)
			
			if tt.containerError == nil {
				mockContainer.EXPECT().Directory("/workspace").Return(mockDirectory)
			}

			zizmor := NewZizmor(mockClient)
			fixedDirectory, output, err := zizmor.RunZizmorAutoFix(context.Background(), mockDirectory)

			assert.Equal(t, tt.expectedOutput, output)
			
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Equal(t, tt.expectedError, err.Error())
				assert.Nil(t, fixedDirectory)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, fixedDirectory)
				assert.Equal(t, mockDirectory, fixedDirectory)
			}
		})
	}
}

func TestSummarizeExternalFindings(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	zizmor := NewZizmor(mockClient)

	tests := []struct {
		name           string
		fullReport     string
		expectedResult string
	}{
		{
			name:           "empty report",
			fullReport:     "",
			expectedResult: "## External Dependencies Security Summary (0 repos scanned)\n\n",
		},
		{
			name: "single repo with issue",
			fullReport: `### actions/checkout
{"desc": "Potential security vulnerability", "given_path": ".github/workflows/ci.yml"}`,
			expectedResult: "## External Dependencies Security Summary (1 repos scanned)\n\n- **actions/checkout**: Potential security vulnerability\n  - File: .github/workflows/ci.yml\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := zizmor.SummarizeExternalFindings(tt.fullReport)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}