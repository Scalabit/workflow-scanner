package github

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"workflow-scanner/pkg/zizmor"
)

const (
	title   = "Security Audit & Fixes for GitHub Actions Workflows"
	bodyFmt = `## Security Audit Summary

**Findings:** %d

### Automatic Fixes Applied
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
*Automated security audit by AI analysis*`
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

func shouldSkipLine(l string) bool {
	if l == "" || strings.HasPrefix(l, "Successfully applied") {
		return true
	}

	return l == "}" || l == "Fix Summary" || l == "]"
}

func parseTableRow(l string) string {
	const expectedRegexGroups = 3
	if m := fixLineRe.FindStringSubmatch(l); len(m) == expectedRegexGroups {
		file := strings.TrimSpace(m[1])
		count := m[2]

		return fmt.Sprintf("| %s | %s |\n", file, count)
	}

	if idx := strings.Index(l, ":"); idx != -1 {
		file := strings.TrimSpace(l[:idx])
		rest := strings.TrimSpace(l[idx+1:])
		numRe := regexp.MustCompile(`\d+`)
		num := numRe.FindString(rest)
		if num == "" {
			return fmt.Sprintf("| %s | - |\n", file)
		}

		return fmt.Sprintf("| %s | %s |\n", file, num)
	}

	return fmt.Sprintf("| %s | - |\n", l)
}

func fixSummaryToTableRows(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return "| (none) | 0 |\n"
	}
	lines := strings.Split(summary, "\n")
	rows := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if shouldSkipLine(l) {
			continue
		}
		rows = append(rows, parseTableRow(l))
	}

	return strings.Join(rows, "")
}

func formatRemainingIssues(finalValidation string) string {
	if finalValidation == "" || finalValidation == "[]" || finalValidation == "[]\n" {
		return ""
	}

	allFindings := parseZizmorFindings(finalValidation)
	if allFindings == nil {
		return fmt.Sprintf("**Manual review needed - some issues remain:**\n```json\n%s\n```", finalValidation)
	}

	log.Printf("DEBUG: Successfully parsed %d findings", len(allFindings))

	var result strings.Builder
	result.WriteString("**Manual review needed - some issues remain:**\n\n")

	fileIssues := groupFindingsByFile(allFindings)

	for filePath, issues := range fileIssues {
		result.WriteString("<details>\n")
		result.WriteString(fmt.Sprintf("<summary>📄 <b>%s</b></summary>\n\n", filePath))

		for _, issue := range issues {
			formatIssueDetails(&result, issue)
			result.WriteString("---\n\n")
		}

		result.WriteString("</details>\n\n")
	}

	return result.String()
}

func parseZizmorFindings(finalValidation string) []zizmor.Finding {
	var allFindings []zizmor.Finding

	cleanedInput := strings.TrimSuffix(finalValidation, "[]")

	if err := json.Unmarshal([]byte(cleanedInput), &allFindings); err != nil {
		log.Printf("ERROR: Failed to unmarshal ZIZMOR findings: %v", err)
		const maxLogChars = 500
		log.Printf("DEBUG: Raw input (first %d chars): %s", maxLogChars, finalValidation[:min(maxLogChars, len(finalValidation))])

		return nil
	}

	return allFindings
}

func groupFindingsByFile(findings []zizmor.Finding) map[string][]zizmor.Finding {
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

	return fileIssues
}

func formatIssueDetails(result *strings.Builder, issue zizmor.Finding) {
	result.WriteString(fmt.Sprintf("- **Issue:** %s\n", issue.Desc))
	result.WriteString(fmt.Sprintf("- **Severity:** %s\n", issue.Determinations.Severity))

	for _, loc := range issue.Locations {
		if loc.Concrete.Location.StartPoint.Row > 0 {
			result.WriteString(fmt.Sprintf("- **Location:** Line %d\n", loc.Concrete.Location.StartPoint.Row))

			if loc.Symbolic.Annotation != "" {
				result.WriteString(fmt.Sprintf("- **Details:** %s\n", loc.Symbolic.Annotation))
			}

			break
		}
	}

	result.WriteString("- **Manual Fix Needed:** Review the TODO comments added in the code changes for suggested fixes.\n\n")
}

type externalDepData struct {
	repoStats   map[string]int
	repoFiles   map[string][]string
	repoDetails map[string][]string
}

func parseExternalDependencyLines(lines []string) *externalDepData {
	data := &externalDepData{
		repoStats:   make(map[string]int),
		repoFiles:   make(map[string][]string),
		repoDetails: make(map[string][]string),
	}

	currentRepo := ""
	currentFindingBlock := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isRepoHeader(line) {
			currentRepo, currentFindingBlock = processRepoHeader(line, currentRepo, currentFindingBlock, data)
		} else if strings.HasPrefix(line, "- File:") && currentRepo != "" {
			currentFindingBlock = processFileLine(line, currentRepo, currentFindingBlock, data)
		}
	}

	if currentRepo != "" && currentFindingBlock != "" {
		data.repoDetails[currentRepo] = append(data.repoDetails[currentRepo], currentFindingBlock)
	}

	return data
}

func isRepoHeader(line string) bool {
	return strings.HasPrefix(line, "- **") && strings.Contains(line, "**:")
}

func processRepoHeader(line, currentRepo, currentFindingBlock string, data *externalDepData) (string, string) {
	if currentRepo != "" && currentFindingBlock != "" {
		data.repoDetails[currentRepo] = append(data.repoDetails[currentRepo], currentFindingBlock)
		currentFindingBlock = ""
	}

	parts := strings.Split(line, "**:")
	if len(parts) >= 1 {
		currentRepo = strings.TrimPrefix(parts[0], "- **")
		data.repoStats[currentRepo]++

		if len(parts) > 1 {
			desc := strings.TrimSpace(parts[1])
			currentFindingBlock = fmt.Sprintf("- **Issue:** %s\n", desc)
		}
	}

	return currentRepo, currentFindingBlock
}

func processFileLine(line, currentRepo, currentFindingBlock string, data *externalDepData) string {
	filePath := strings.TrimPrefix(line, "- File:")
	filePath = strings.TrimSpace(filePath)
	data.repoFiles[currentRepo] = append(data.repoFiles[currentRepo], filePath)

	return currentFindingBlock + fmt.Sprintf("- **File:** %s\n", filePath)
}

func buildExternalSummaryTable(data *externalDepData) string {
	var result strings.Builder

	result.WriteString("**Summary:** ")
	totalFindings := 0
	for _, count := range data.repoStats {
		totalFindings += count
	}
	result.WriteString(fmt.Sprintf("%d findings across %d actions\n\n", totalFindings, len(data.repoStats)))

	result.WriteString("| Action/Repo | Files | Findings |\n")
	result.WriteString("| --- | ---: | ---: |\n")

	for repo, count := range data.repoStats {
		fileCount := len(data.repoFiles[repo])
		if fileCount == 0 {
			fileCount = 1
		}
		result.WriteString(fmt.Sprintf("| %s | %d | %d |\n", repo, fileCount, count))
	}

	result.WriteString("\n")

	return result.String()
}

func buildExternalDetailedFindings(data *externalDepData) string {
	var result strings.Builder

	result.WriteString("<details>\n")
	result.WriteString("<summary>📋 <b>Detailed Findings</b> (click to expand)</summary>\n\n")

	for repo, details := range data.repoDetails {
		result.WriteString(fmt.Sprintf("#### 📦 %s\n\n", repo))
		for _, finding := range details {
			result.WriteString(finding)
			result.WriteString("\n---\n\n")
		}
	}

	result.WriteString("</details>\n")

	return result.String()
}

func formatExternalDependencies(summaryFindings string) string {
	if strings.TrimSpace(summaryFindings) == "" {
		return "No external dependencies scanned."
	}

	lines := strings.Split(summaryFindings, "\n")
	data := parseExternalDependencyLines(lines)

	if len(data.repoStats) == 0 {
		return summaryFindings
	}

	return buildExternalSummaryTable(data) + buildExternalDetailedFindings(data)
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
	formattedExternal := formatExternalDependencies(summaryFindings)

	body := fmt.Sprintf(bodyFmt,
		len(zizmorFindings),
		tableRows,
		// llmOut,
		result.status,
		result.text,
		validationStatus,
		formattedExternal,
	)

	// GitHub PR body limit is 65,536 characters
	const maxPRBodyLength = 65000 // Leave some margin
	if len(body) > maxPRBodyLength {
		body = body[:maxPRBodyLength] + "\n\n... (truncated due to length - see full results in workflow logs)"
	}

	return title, body
}
