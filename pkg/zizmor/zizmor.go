package zizmor

//go:generate mockgen -source=zizmor.go -destination=../../mocks/zizmor_mock.go -package=mocks Zizmor

import (
	"context"
	"fmt"
	"strings"
	internalDagger "workflow-scanner/internal/dagger"
	"workflow-scanner/pkg/dagger"
)

const (
	RESULT_SIZE_LIMIT = 40000
)

type Zizmor interface {
	CheckRemainingIssues(ctx context.Context, source *internalDagger.Directory) (string, error)
	RunZizmorAutoFix(ctx context.Context, source *internalDagger.Directory) (*internalDagger.Directory, []Finding, string, error)
	ScanExternalDependencies(ctx context.Context, source *internalDagger.Directory) (string, error)
	SummarizeExternalFindings(fullReport string) string
}

type ZizmorImpl struct {
	client dagger.Client
}

func NewZizmor(client dagger.Client) Zizmor {
	return &ZizmorImpl{
		client: client,
	}
}

func (ziz *ZizmorImpl) RunZizmorAutoFix(ctx context.Context, source *internalDagger.Directory) (*internalDagger.Directory, []Finding, string, error) {
	container := ziz.getZizmorContainer(source).
		WithExec([]string{"sh", "-c", "zizmor -q --format=json --fix=all .github/workflows/ 2>&1 || true"})

	fmt.Println("LS -LA :")
	output, err := container.Stdout(ctx)
	if err != nil {
		return nil, []Finding{}, "", fmt.Errorf("failed to run ZIZMOR: %w", err)
	}

	findings, fixSummary, err := ParseZizmorOutput(output)
	if err != nil {
		return nil, []Finding{}, "", fmt.Errorf("failed to run structure output: %w", err)
	}

	return container.Directory("."), findings, fixSummary, nil
}

func (ziz *ZizmorImpl) CheckRemainingIssues(ctx context.Context, source *internalDagger.Directory) (string, error) {
	container := ziz.getZizmorContainer(source).
		WithExec([]string{"sh", "-c", "zizmor --format=json .github/workflows/ 2>/dev/null || echo '[]'"})

	output, err := container.Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to check remaining issues: %w", err)
	}

	output = strings.TrimSpace(output)

	if output == "" || output == "[]" || output == "[]\n" {
		return "", nil
	}

	return output, nil
}

func (ziz *ZizmorImpl) ScanExternalDependencies(ctx context.Context, source *internalDagger.Directory) (string, error) {
	// Create a container with git and ZIZMOR using official image
	baseContainer := ziz.client.Container().
		From("ghcr.io/zizmorcore/zizmor:1.18.0").
		WithExec([]string{"apk", "add", "--no-cache", "git"}). // Add git to official image
		WithDirectory("/workspace", source).                   // Only for extracting repo list
		WithWorkdir("/tmp/external-scans")                     // Work in temp dir for clones

	// Find all external repositories used in workflows (run in workspace dir)
	findReposCmd := `cd /workspace && find .github/workflows -name "*.yml" -o -name "*.yaml" | xargs grep -h "uses:" | grep -v "^#" | sed 's/.*uses: *//g' | sed 's/@.*//g' | grep "/" | sort -u`

	reposOutput, err := baseContainer.WithExec([]string{"sh", "-c", findReposCmd}).Stdout(ctx)
	if err != nil {
		return "Failed to extract external repositories", err
	}

	if reposOutput == "" {
		return "No external repositories found in workflows", nil
	}

	// Scan each external repository
	var allFindings strings.Builder
	repos := strings.Split(strings.TrimSpace(reposOutput), "\n")

	for _, repo := range repos {
		if repo == "" {
			continue
		}

		// Clone external repo and scan with our pre-built ZIZMOR container
		repoURL := fmt.Sprintf("https://github.com/%s", repo)
		tempDir := strings.ReplaceAll(repo, "/", "-")

		// Step 1: Clone the repo with error handling
		cloneContainer := baseContainer.WithExec([]string{"sh", "-c", fmt.Sprintf("git clone --depth=1 %s %s 2>&1 || echo 'Clone failed for %s'", repoURL, tempDir, repo)})

		// Step 2: Check what was cloned and scan
		scanCmd := fmt.Sprintf(`
			if [ -d %s ]; then
				echo "=== Successfully cloned %s ==="
				cd %s
				if [ -d .github/workflows ]; then 
					echo "Found workflows in %s, scanning..."
					zizmor --format=json .github/workflows/ 2>&1 || echo "ZIZMOR scan failed"
				else 
					echo "No .github/workflows directory in %s (normal for action repos)"
					echo "Contents:" && ls -la | head -10
				fi
			else
				echo "Failed to clone %s"
			fi
		`, tempDir, repo, tempDir, repo, repo, repo)

		findings, err := cloneContainer.WithExec([]string{"sh", "-c", scanCmd}).Stdout(ctx)
		if err != nil {
			allFindings.WriteString(fmt.Sprintf("### %s\nFailed to scan: %s\n\n", repo, err.Error()))

			continue
		}

		if findings != "" && findings != "No workflows found" && findings != "No .github/workflows directory" {
			allFindings.WriteString(fmt.Sprintf("### %s\n```json\n%s\n```\n\n", repo, findings))
		} else {
			allFindings.WriteString(fmt.Sprintf("### %s\nNo security issues found or no workflows present\n\n", repo))
		}
	}

	if allFindings.Len() == 0 {
		return "No external dependencies scanned", nil
	}

	return allFindings.String(), nil
}

func (ziz *ZizmorImpl) SummarizeExternalFindings(fullReport string) string {
	var summary strings.Builder

	// Count total repos scanned
	repoCount := strings.Count(fullReport, "###")
	summary.WriteString(fmt.Sprintf("## External Dependencies Security Summary (%d repos scanned)\n\n", repoCount))

	lines := strings.Split(fullReport, "\n")
	currentRepo := ""

	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "### "); ok {
			currentRepo = after

			continue
		}

		// Look for issue descriptions in JSON
		if strings.Contains(line, `"desc":`) {
			desc := strings.Split(line, `"desc": "`)[1]
			desc = strings.Split(desc, `"`)[0]
			summary.WriteString(fmt.Sprintf("- **%s**: %s\n", currentRepo, desc))
		}

		// Look for file paths in JSON
		if strings.Contains(line, `"given_path":`) {
			path := strings.Split(line, `"given_path": "`)[1]
			path = strings.Split(path, `"`)[0]
			summary.WriteString(fmt.Sprintf("  - File: %s\n", path))
		}
	}

	// Truncate if too long
	result := summary.String()
	if len(result) > RESULT_SIZE_LIMIT {
		result = result[:RESULT_SIZE_LIMIT] + "\n\n... (truncated)"
	}

	return result
}

func (ziz *ZizmorImpl) getZizmorContainer(source *internalDagger.Directory) dagger.Container {
	return ziz.client.Container().
		From("ghcr.io/zizmorcore/zizmor:1.18.0").
		WithDirectory("/workspace", source).
		WithWorkdir("/workspace")
}
