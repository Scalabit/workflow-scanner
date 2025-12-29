package agent

import (
	"context"
	"errors"
	"testing"

	internalDagger "workflow-scanner/internal/dagger"
	"workflow-scanner/mocks"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestAgentImpl_BusinessLogic(t *testing.T) {
	t.Run("early return condition logic", func(t *testing.T) {
		testCases := []struct {
			name       string
			issues     string
			shouldSkip bool
		}{
			{"empty issues", "", true},
			{"empty JSON array", "[]", true},
			{"empty JSON array with newline", "[]\n", true},
			{"whitespace only", "   ", false},
			{"real issues", `[{"desc": "security issue"}]`, false},
			{"non-JSON output", "some text output", false},
		}

		for _, tc := range testCases {
			actualShouldSkip := areThereIssues(tc.issues)
			assert.Equal(t, tc.shouldSkip, actualShouldSkip,
				"Early return logic failed for input: %q", tc.issues)
		}
	})
}

func TestAgentImpl_FixRemainingIssues_EarlyReturn(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	sourceDirectory := &internalDagger.Directory{}

	// Test early return cases - these don't call LLM chain
	tests := []struct {
		name                string
		issues              string
		expectedExplanation string
	}{
		{
			name:                "empty issues",
			issues:              "",
			expectedExplanation: "No remaining issues found after ZIZMOR auto-fix",
		},
		{
			name:                "empty JSON array",
			issues:              "[]",
			expectedExplanation: "No remaining issues found after ZIZMOR auto-fix",
		},
		{
			name:                "empty JSON array with newline",
			issues:              "[]\n",
			expectedExplanation: "No remaining issues found after ZIZMOR auto-fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := NewAgentImpl(mockClient)
			actualDir, explanation, err := agent.fixRemainingIssuesImpl(context.Background(), sourceDirectory, tt.issues)

			assert.NoError(t, err)
			assert.Equal(t, sourceDirectory, actualDir)
			assert.Equal(t, tt.expectedExplanation, explanation)
		})
	}
}

func TestAgentImpl_FixRemainingIssues_LLMChain(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockEnv := mocks.NewMockEnv(ctrl)
	mockWorkspace := mocks.NewMockWorkspace(ctrl)
	mockLLM := mocks.NewMockLLM(ctrl)
	mockBinding := mocks.NewMockBinding(ctrl)
	mockFile := &internalDagger.File{}
	sourceDirectory := &internalDagger.Directory{}
	completedDirectory := &internalDagger.Directory{}

	tests := []struct {
		name                string
		issues              string
		llmExplanation      string
		llmError            error
		expectedExplanation string
		expectError         bool
	}{
		{
			name:                "successful LLM processing",
			issues:              `[{"desc": "security issue", "file": "workflow.yml"}]`,
			llmExplanation:      "Fixed security vulnerability in workflow.yml",
			llmError:            nil,
			expectedExplanation: "Fixed security vulnerability in workflow.yml",
			expectError:         false,
		},
		{
			name:                "LLM processing fails",
			issues:              `[{"desc": "security issue", "file": "workflow.yml"}]`,
			llmExplanation:      "",
			llmError:            errors.New("LLM timeout"),
			expectedExplanation: "",
			expectError:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.EXPECT().Env().Return(mockEnv)
			mockEnv.EXPECT().WithStringInput("zizmor_issues", tt.issues, gomock.Any()).Return(mockEnv)
			mockEnv.EXPECT().WithStringInput("GO111MODULE", "on", gomock.Any()).Return(mockEnv)
			mockEnv.EXPECT().WithStringInput("GOWORK", "off", gomock.Any()).Return(mockEnv)
			mockClient.EXPECT().Workspace(gomock.Any()).Return(mockWorkspace)
			mockEnv.EXPECT().WithWorkspaceInput("workspace", mockWorkspace, gomock.Any()).Return(mockEnv)
			mockEnv.EXPECT().WithWorkspaceOutput("completed", gomock.Any()).Return(mockEnv)
			mockEnv.EXPECT().WithStringOutput("explanations", gomock.Any()).Return(mockEnv)
			mockClient.EXPECT().LLM().Return(mockLLM)
			mockLLM.EXPECT().WithEnv(mockEnv).Return(mockLLM)
			mockLLM.EXPECT().WithPromptFile(mockFile).Return(mockLLM)

			mockLLM.EXPECT().Env().Return(mockEnv)
			mockEnv.EXPECT().Output("explanations").Return(mockBinding)
			mockBinding.EXPECT().AsString(gomock.Any()).Return(tt.llmExplanation, tt.llmError)

			var expectedDir *internalDagger.Directory
			if tt.llmError == nil {
				// Only expect workspace operations on success
				mockEnv.EXPECT().Output("completed").Return(mockBinding)
				mockBinding.EXPECT().AsWorkspace().Return(mockWorkspace)
				mockWorkspace.EXPECT().Source().Return(completedDirectory)
				expectedDir = completedDirectory
			} else {
				expectedDir = sourceDirectory
			}

			agent := NewAgentImpl(mockClient)
			actualDir, explanation, err := agent.fixRemainingIssuesImpl(context.Background(), sourceDirectory, tt.issues)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, expectedDir, actualDir)
			assert.Equal(t, tt.expectedExplanation, explanation)
		})
	}
}
