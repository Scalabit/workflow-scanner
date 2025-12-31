package github

import (
	"fmt"
	"workflow-scanner/pkg/zizmor"
)

const (
	title   = "Security Audit & Fixes for GitHub Actions Workflows"
	bodyFmt = `## Complete Security Audit Report

This PR contains comprehensive security analysis and fixes for GitHub Actions workflows.
Number of Findings: %d

### Files Auto-fixed by ZIZMOR
%s

### Manual Security Fixes Applied
%s

---

## %s Validation Report: %s

%s

---

### External Dependencies Security Scan
%s

---
*Automated security audit by ZIZMOR + AI analysis*`
)

type Result struct {
	status string
	text   string
}

var (
	passed = Result{
		status: "✅",
		text:   "PASSED",
	}
	failed = Result{
		status: "❌",
		text:   "NEEDS REVIEW",
	}
)

func GetPrTitleBody(finalValidation string, zizmorFindings []zizmor.Finding, fixSummary string, llmOut string, summaryFindings string) (string, string) {
	var result Result
	success := finalValidation == "" || finalValidation == "[]" || finalValidation == "[]\n"

	validationStatus := ""
	if success {
		validationStatus = "**All security issues resolved!** No vulnerabilities detected."
		result = passed
	} else {
		validationStatus = fmt.Sprintf("**Manual review needed - some issues remain:**\n```json\n%s\n```", finalValidation)
		result = failed
	}

	body := fmt.Sprintf(bodyFmt,
		len(zizmorFindings),
		fixSummary,
		llmOut,
		result.status,
		result.text,
		validationStatus,
		summaryFindings,
	)

	// GitHub PR body limit is 65,536 characters
	const maxPRBodyLength = 65000 // Leave some margin
	if len(body) > maxPRBodyLength {
		body = body[:maxPRBodyLength] + "\n\n... (truncated due to length - see full results in workflow logs)"
	}

	return title, body
}
