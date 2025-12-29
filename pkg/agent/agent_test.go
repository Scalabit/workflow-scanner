package agent

import (
	"context"
	"os"
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
	
	// Use a real directory for the source since we're testing early return that doesn't use LLM
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
	// Skip this test if the prompt file doesn't exist since the implementation now reads from filesystem
	promptPaths := []string{
		"llm_fix_prompt.md",
		"../../llm_fix_prompt.md",
	}
	var promptExists bool
	for _, path := range promptPaths {
		if _, err := os.ReadFile(path); err == nil {
			promptExists = true
			break
		}
	}
	if !promptExists {
		t.Skip("Skipping LLM test - prompt file not found in expected locations")
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Test that we can read the prompt file and create error scenarios
	// without requiring full Dagger integration
	t.Run("prompt file not found", func(t *testing.T) {
		mockClient := mocks.NewMockClient(ctrl)
		mockEnv := mocks.NewMockEnv(ctrl)
		mockWorkspace := mocks.NewMockWorkspace(ctrl)
		sourceDirectory := &internalDagger.Directory{}
		
		// Set up the mocks for the initial setup that happens before prompt file reading
		mockClient.EXPECT().Workspace(sourceDirectory).Return(mockWorkspace)
		mockClient.EXPECT().Env().Return(mockEnv)
		mockEnv.EXPECT().WithStringInput("zizmor_issues", `[{"desc": "security issue"}]`, gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithStringInput("GO111MODULE", "on", gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithStringInput("GOWORK", "off", gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithWorkspaceInput("workspace", mockWorkspace, gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithWorkspaceOutput("completed", gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithStringOutput("explanations", gomock.Any()).Return(mockEnv)
		
		// Create a temporary directory without the prompt file
		tempDir := t.TempDir()
		originalWd, _ := os.Getwd()
		defer os.Chdir(originalWd)
		
		// Change to temp directory so prompt file won't be found
		os.Chdir(tempDir)
		
		agent := NewAgentImpl(mockClient)
		actualDir, explanation, err := agent.fixRemainingIssuesImpl(context.Background(), sourceDirectory, `[{"desc": "security issue"}]`)
		
		// Should return error when prompt file can't be found
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read prompt file")
		assert.Equal(t, sourceDirectory, actualDir)
		assert.Equal(t, "", explanation)
	})
	
	t.Run("validates prompt file reading and environment setup", func(t *testing.T) {
		// This test validates that the prompt file is read and environment is set up correctly
		// up to the point where LLM processing would begin. We can't easily test the full
		// LLM workflow without a running Dagger session, but we can test the setup logic.
		
		mockClient := mocks.NewMockClient(ctrl)
		mockEnv := mocks.NewMockEnv(ctrl)
		mockWorkspace := mocks.NewMockWorkspace(ctrl)
		sourceDirectory := &internalDagger.Directory{}
		
		// Set up mocks for the environment creation part
		mockClient.EXPECT().Workspace(sourceDirectory).Return(mockWorkspace)
		mockClient.EXPECT().Env().Return(mockEnv)
		mockEnv.EXPECT().WithStringInput("zizmor_issues", `[{"desc": "security issue"}]`, gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithStringInput("GO111MODULE", "on", gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithStringInput("GOWORK", "off", gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithWorkspaceInput("workspace", mockWorkspace, gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithWorkspaceOutput("completed", gomock.Any()).Return(mockEnv)
		mockEnv.EXPECT().WithStringOutput("explanations", gomock.Any()).Return(mockEnv)
		
		// The test will fail when trying to call WithNewFile on the real directory,
		// which is expected behavior - the rest of the logic would need integration testing
		agent := NewAgentImpl(mockClient)
		_, _, err := agent.fixRemainingIssuesImpl(context.Background(), sourceDirectory, `[{"desc": "security issue"}]`)
		
		// We expect this to fail at the WithNewFile step, which proves the setup logic ran correctly
		assert.Error(t, err)
	})
}
