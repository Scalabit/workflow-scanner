package github

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"workflow-scanner/pkg/zizmor"
)

const (
	title   = "Security Audit & Fixes for GitHub Actions Workflows"
	bodyFmt = `## Security Audit Summary

**Findings:** %d

### Files Auto-fixed by ZIZMOR
| File | Fixes |
| --- | ---: |
%s

---

**Validation:** %s %s

%s

---

### External Dependencies Scan
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

var fixLineRe = regexp.MustCompile(`^\s*(.+?):\s*(\d+)`)

func fixSummaryToTableRows(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return "| (none) | 0 |\n"
	}
	lines := strings.Split(summary, "\n")
	rows := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Successfully applied") {
			continue
		}
		const expectedRegexGroups = 3
		if m := fixLineRe.FindStringSubmatch(l); len(m) == expectedRegexGroups {
			file := strings.TrimSpace(m[1])
			count := m[2]
			rows = append(rows, fmt.Sprintf("| %s | %s |\n", file, count))

			continue
		}
		if idx := strings.Index(l, ":"); idx != -1 {
			file := strings.TrimSpace(l[:idx])
			rest := strings.TrimSpace(l[idx+1:])
			numRe := regexp.MustCompile(`\d+`)
			num := numRe.FindString(rest)
			if num == "" {
				rows = append(rows, fmt.Sprintf("| %s | - |\n", file))
			} else {
				rows = append(rows, fmt.Sprintf("| %s | %s |\n", file, num))
			}

			continue
		}
		rows = append(rows, fmt.Sprintf("| %s | - |\n", l))
	}

	return strings.Join(rows, "")
}

func formatRemainingIssues(finalValidation string) string {
	if finalValidation == "" || finalValidation == "[]" || finalValidation == "[]\n" {
		return ""
	}

	var findings []zizmor.Finding
	if err := json.Unmarshal([]byte(finalValidation), &findings); err != nil {
		return fmt.Sprintf("**Manual review needed - some issues remain:**\n```json\n%s\n```", finalValidation)
	}

	var result strings.Builder
	result.WriteString("**Manual review needed - some issues remain:**\n\n")

	fileIssues := make(map[string][]zizmor.Finding)
	for _, finding := range findings {
		for _, loc := range finding.Locations {
			if loc.Symbolic.Key.Local != nil {
				filePath := loc.Symbolic.Key.Local.GivenPath
				fileIssues[filePath] = append(fileIssues[filePath], finding)
				break
			}
		}
	}

	for filePath, issues := range fileIssues {
		result.WriteString(fmt.Sprintf("### 📄 `%s`\n", filePath))
		
		for _, issue := range issues {
			result.WriteString(fmt.Sprintf("**Issue:** %s\n", issue.Desc))
			result.WriteString(fmt.Sprintf("**Severity:** %s\n", issue.Determinations.Severity))
			
			for _, loc := range issue.Locations {
				if loc.Concrete.Location.StartPoint.Row > 0 {
					result.WriteString(fmt.Sprintf("**Location:** Line %d\n", loc.Concrete.Location.StartPoint.Row))
					
					if loc.Symbolic.Annotation != "" {
						result.WriteString(fmt.Sprintf("**Details:** %s\n", loc.Symbolic.Annotation))
					}
					break
				}
			}
			
			result.WriteString("**Manual Fix Needed:** Review the TODO comments added in the code changes for suggested fixes.\n")
			
			result.WriteString("---\n")
		}
	}

	return result.String()
}

func GetPrTitleBody(finalValidation string, zizmorFindings []zizmor.Finding, fixSummary string, llmOut string, summaryFindings string) (string, string) {
	var result Result
	success := finalValidation == "" || finalValidation == "[]" || finalValidation == "[]\n"

	validationStatus := ""
	if success {
		validationStatus = "**All security issues resolved!** No vulnerabilities detected."
		result = passed
	} else {
		validationStatus = formatRemainingIssues(finalValidation)
		result = failed
	}

	tableRows := fixSummaryToTableRows(fixSummary)

	body := fmt.Sprintf(bodyFmt,
		len(zizmorFindings),
		tableRows,
		// llmOut,
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
