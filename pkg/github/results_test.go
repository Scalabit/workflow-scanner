package github

import (
	"testing"
	"workflow-scanner/pkg/zizmor"

	"github.com/stretchr/testify/assert"
)

func TestGetPrTitleBody(t *testing.T) {
	tests := []struct {
		name              string
		finalValidation   string
		zizmorFindings    []zizmor.Finding
		fixSummary        string
		llmOut            string
		summaryFindings   string
		expectedTitle     string
		expectedBodyParts []string
	}{
		{
			name:            "validation passed - no issues found",
			finalValidation: "",
			zizmorFindings:  []zizmor.Finding{},
			fixSummary:      "Fixed 2 security issues automatically",
			llmOut:          "Applied additional manual fixes",
			summaryFindings: "No issues in external dependencies",
			expectedTitle:   "Security Audit & Fixes for GitHub Actions Workflows",
			expectedBodyParts: []string{
				"Security Audit Summary",
				"Fixed 2 security issues automatically",
				"PASSED",
				"All security issues resolved!",
				"No issues in external dependencies",
				"Automated security audit by AI analysis",
			},
		},
		{
			name:            "validation passed - empty JSON array",
			finalValidation: "[]",
			zizmorFindings: []zizmor.Finding{
				{
					Ident:          "",
					Desc:           "",
					URL:            "",
					Determinations: zizmor.Determinations{},
					Locations:      []zizmor.Location{},
					Ignored:        false,
				},
			},
			fixSummary:      "No issues found",
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
			zizmorFindings: []zizmor.Finding{
				{
					Ident:          "1",
					Desc:           "fake issue",
					URL:            "fake url",
					Determinations: zizmor.Determinations{},
					Locations:      []zizmor.Location{},
					Ignored:        false,
				},
			},
			fixSummary:      "Fixed some issues",
			llmOut:          "Could not fix all issues",
			summaryFindings: "Some external issues found",
			expectedTitle:   "Security Audit & Fixes for GitHub Actions Workflows",
			expectedBodyParts: []string{
				"NEEDS REVIEW",
				"Manual review needed - some issues remain:",
				"Some external issues found",
			},
		},
		{
			name:            "validation passed - JSON array with newline",
			finalValidation: "[]\n",
			zizmorFindings: []zizmor.Finding{
				{
					Ident:          "1",
					Desc:           "fake issue",
					URL:            "fake url",
					Determinations: zizmor.Determinations{},
					Locations:      []zizmor.Location{},
					Ignored:        false,
				},
			},
			fixSummary:      "Clean workflows",
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
				tt.zizmorFindings,
				tt.fixSummary,
				tt.llmOut,
				tt.summaryFindings,
			)

			assert.Equal(t, tt.expectedTitle, title)

			for _, expectedPart := range tt.expectedBodyParts {
				assert.Contains(t, body, expectedPart,
					"Expected body to contain: %s", expectedPart)
			}

			assert.Contains(t, body, "Automatic Fixes Applied")
			assert.Contains(t, body, "External Dependencies Scan")
		})
	}
}
