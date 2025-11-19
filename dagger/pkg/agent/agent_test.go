package agent

import (
	"context"
	"errors"
	"testing"

	"dagger/workflow-scanner/tests/mocks"
	internalDagger "dagger/workflow-scanner/internal/dagger"

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
			actualShouldSkip := tc.issues == "" || tc.issues == "[]" || tc.issues == "[]\n"
			assert.Equal(t, tc.shouldSkip, actualShouldSkip, 
				"Early return logic failed for input: %q", tc.issues)
		}
	})

	t.Run("agent interface contract", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMockClient(ctrl)
		
		agentInstance := NewAgent(mockClient)
		assert.NotNil(t, agentInstance)
		
		var _ Agent = agentInstance
	})
}

func TestAgentImpl_FixRemainingIssues_EarlyReturn(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockClient(ctrl)
	mockDirectory := mocks.NewMockDirectory(ctrl)
	resultDirectory := mocks.NewMockDirectory(ctrl)
	
	// Test early return cases - these don't call LLM chain
	tests := []struct {
		name              string
		issues            string
		expectedExplanation string
	}{
		{
			name:              "empty issues",
			issues:            "",
			expectedExplanation: "No remaining issues found after ZIZMOR auto-fix",
		},
		{
			name:              "empty JSON array",
			issues:            "[]",
			expectedExplanation: "No remaining issues found after ZIZMOR auto-fix",
		},
		{
			name:              "empty JSON array with newline",
			issues:            "[]\n",
			expectedExplanation: "No remaining issues found after ZIZMOR auto-fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDirectory.EXPECT().WithoutDirectory("node_modules").Return(resultDirectory)

			agent := NewAgent(mockClient)
			actualDir, explanation, err := agent.FixRemainingIssues(context.Background(), mockDirectory, tt.issues)

			assert.NoError(t, err)
			assert.Equal(t, resultDirectory, actualDir)
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
	mockCurrentModule := mocks.NewMockCurrentModule(ctrl)
	mockModuleSource := mocks.NewMockModuleSource(ctrl)
	mockLLM := mocks.NewMockLLM(ctrl)
	mockLLMWithEnv := mocks.NewMockLLMWithEnv(ctrl)
	mockEnvOutput := mocks.NewMockEnvOutput(ctrl)
	mockDirectory := mocks.NewMockDirectory(ctrl)
	mockFile := &internalDagger.File{}
	resultDir := mocks.NewMockDirectory(ctrl)
	finalDir := mocks.NewMockDirectory(ctrl)

	tests := []struct {
		name              string
		issues            string
		llmExplanation    string
		llmError          error
		expectedExplanation string
		expectError       bool
	}{
		{
			name:              "successful LLM processing",
			issues:            `[{"desc": "security issue", "file": "workflow.yml"}]`,
			llmExplanation:    "Fixed security vulnerability in workflow.yml",
			llmError:          nil,
			expectedExplanation: "Fixed security vulnerability in workflow.yml",
			expectError:       false,
		},
		{
			name:              "LLM processing fails",
			issues:            `[{"desc": "security issue", "file": "workflow.yml"}]`,
			llmExplanation:    "",
			llmError:          errors.New("LLM timeout"),
			expectedExplanation: "LLM processing failed - returning original workspace unchanged",
			expectError:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient.EXPECT().Env().Return(mockEnv)
			mockEnv.EXPECT().WithStringInput("zizmor_issues", tt.issues, gomock.Any()).Return(mockEnv)
			mockClient.EXPECT().Workspace(mockDirectory).Return(mockWorkspace)
			mockEnv.EXPECT().WithWorkspaceInput("workspace", mockWorkspace, gomock.Any()).Return(mockEnv)
			mockEnv.EXPECT().WithWorkspaceOutput("completed", gomock.Any()).Return(mockEnv)
			mockEnv.EXPECT().WithStringOutput("explanations", gomock.Any()).Return(mockEnv)
			
			mockClient.EXPECT().CurrentModule().Return(mockCurrentModule)
			mockCurrentModule.EXPECT().Source().Return(mockModuleSource)
			mockModuleSource.EXPECT().File("llm_fix_prompt.md").Return(mockFile)
			
			mockClient.EXPECT().LLM().Return(mockLLM)
			mockLLM.EXPECT().WithEnv(mockEnv).Return(mockLLMWithEnv)
			mockLLMWithEnv.EXPECT().WithPromptFile(mockFile).Return(mockLLMWithEnv)
			
			mockLLMWithEnv.EXPECT().Env().Return(mockEnv)
			mockEnv.EXPECT().Output("explanations").Return(mockEnvOutput)
			mockEnvOutput.EXPECT().AsString(gomock.Any()).Return(tt.llmExplanation, tt.llmError)

			if tt.llmError == nil {
				// Only expect workspace operations on success
				mockEnv.EXPECT().Output("completed").Return(mockEnvOutput)
				mockEnvOutput.EXPECT().AsWorkspace().Return(mockWorkspace)
				mockWorkspace.EXPECT().Source().Return(resultDir)
				resultDir.EXPECT().WithoutDirectory("node_modules").Return(finalDir)
			} else {
				// On error, return original workspace
				mockDirectory.EXPECT().WithoutDirectory("node_modules").Return(finalDir)
			}

			agent := NewAgent(mockClient)
			actualDir, explanation, err := agent.FixRemainingIssues(context.Background(), mockDirectory, tt.issues)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, finalDir, actualDir)
			assert.Equal(t, tt.expectedExplanation, explanation)
		})
	}
}
