package github

import "fmt"

const (
	title   = "Security Audit & Fixes for GitHub Actions Workflows"
	bodyFmt = `## Complete Security Audit Report

This PR contains comprehensive security analysis and fixes for GitHub Actions workflows.

### Auto-fixed by ZIZMOR
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

func GetPrTitleBody(finalValidation string, zizmorOut, llmOut, summaryFindings string) (string, string) {
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

	return title, fmt.Sprintf(bodyFmt,
		zizmorOut,
		llmOut,
		result.status,
		result.text,
		validationStatus,
		summaryFindings,
	)
}
