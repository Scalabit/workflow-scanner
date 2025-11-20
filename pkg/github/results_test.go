package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetPrTitleBody(t *testing.T) {
	tests := []struct {
		name              string
		finalValidation   string
		zizmorOut         string
		llmOut            string
		summaryFindings   string
		expectedTitle     string
		expectedBodyParts []string
	}{
		{
			name:            "validation passed - no issues found",
			finalValidation: "",
			zizmorOut:       "Fixed 2 security issues automatically",
			llmOut:          "Applied additional manual fixes",
			summaryFindings: "No issues in external dependencies",
			expectedTitle:   "Security Audit & Fixes for GitHub Actions Workflows",
			expectedBodyParts: []string{
				"Complete Security Audit Report",
				"Fixed 2 security issues automatically",
				"Applied additional manual fixes",
				"PASSED",
				"All security issues resolved!",
				"No issues in external dependencies",
				"Automated security audit by ZIZMOR + AI analysis",
			},
		},
		{
			name:            "validation passed - empty JSON array",
			finalValidation: "[]",
			zizmorOut:       "No issues found",
			llmOut:          "No manual fixes needed",
			summaryFindings: "External deps clean",
			expectedTitle:   "Security Audit & Fixes for GitHub Actions Workflows",
			expectedBodyParts: []string{
				"PASSED",
				"All security issues resolved!",
			},
		},
		{
			name:            "validation failed - issues remain",
			finalValidation: `[{"desc": "remaining issue", "file": "test.yml"}]`,
			zizmorOut:       "Fixed some issues",
			llmOut:          "Could not fix all issues",
			summaryFindings: "Some external issues found",
			expectedTitle:   "Security Audit & Fixes for GitHub Actions Workflows",
			expectedBodyParts: []string{
				"NEEDS REVIEW",
				"Manual review needed - some issues remain:",
				`[{"desc": "remaining issue", "file": "test.yml"}]`,
				"Some external issues found",
			},
		},
		{
			name:            "validation passed - JSON array with newline",
			finalValidation: "[]\n",
			zizmorOut:       "Clean workflows",
			llmOut:          "Nothing to fix",
			summaryFindings: "All good",
			expectedTitle:   "Security Audit & Fixes for GitHub Actions Workflows",
			expectedBodyParts: []string{
				"PASSED",
				"All security issues resolved!",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body := GetPrTitleBody(
				tt.finalValidation,
				tt.zizmorOut,
				tt.llmOut,
				tt.summaryFindings,
			)

			assert.Equal(t, tt.expectedTitle, title)
			
			for _, expectedPart := range tt.expectedBodyParts {
				assert.Contains(t, body, expectedPart,
					"Expected body to contain: %s", expectedPart)
			}

			assert.Contains(t, body, "Auto-fixed by ZIZMOR")
			assert.Contains(t, body, "Manual Security Fixes Applied")
			assert.Contains(t, body, "Validation Report:")
			assert.Contains(t, body, "External Dependencies Security Scan")
		})
	}
}