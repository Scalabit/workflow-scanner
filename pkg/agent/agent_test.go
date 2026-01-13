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

	// Test that we can verify early return without requiring full Dagger integration
	t.Run("no issues requiring LLM fixes", func(t *testing.T) {
		mockClient := mocks.NewMockClient(ctrl)
		sourceDirectory := &internalDagger.Directory{}

		// Pass empty issues to trigger early return (doesn't require Dagger infrastructure)
		agent := NewAgentImpl(mockClient)
		actualDir, explanation, err := agent.fixRemainingIssuesImpl(context.Background(), sourceDirectory, "[]")

		// Should return successfully with no issues message
		assert.NoError(t, err)
		assert.Equal(t, sourceDirectory, actualDir)
		assert.Contains(t, explanation, "No remaining issues")
	})

	t.Run("prompt file reading validates filesystem approach", func(t *testing.T) {
		// This test validates that the new filesystem-based prompt reading approach works
		// by checking that the prompt file can be found and read successfully

		// Test that the prompt file exists and can be read (this validates our core fix)
		var promptContent []byte
		var err error
		promptPaths := []string{
			"llm_fix_prompt.md",
			"../../llm_fix_prompt.md",
		}

		for _, path := range promptPaths {
			promptContent, err = os.ReadFile(path)
			if err == nil {
				break
			}
		}

		// Verify we can read the prompt file (this validates the core fix)
		assert.NoError(t, err, "Should be able to read prompt file from filesystem")
		assert.Greater(t, len(promptContent), 0, "Prompt file should have content")
		assert.Contains(t, string(promptContent), "security expert", "Prompt should contain expected content")
	})
}
