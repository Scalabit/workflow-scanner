# Workflow Security Scanner Action

An AI-powered GitHub Action that automatically scans your GitHub Actions workflows for security vulnerabilities and creates pull requests with fixes.

## Description

This action uses Dagger and AI to analyze your GitHub Actions workflows for common security issues.

When security issues are found, the action automatically creates a pull request with the necessary fixes and a detailed description of the changes.

## Inputs

### Required

#### `github-token`
**Required** GitHub token with permissions to write issues and repository contents.

**Example:** `${{ secrets.GITHUB_TOKEN }}`

#### `repository`
**Required** The GitHub repository to scan in the format `owner/repo`.

**Example:** `octocat/hello-world`

### Optional

#### `dagger-module`
**Optional** Path to the Dagger module directory.

**Default:** `dagger`

## Outputs

### `pr-url`
The URL of the created pull request containing the security fixes.

## Secrets Used

This action requires a GitHub token (`github-token` input) with the following permissions:
- `contents: write` - To create pull requests with workflow fixes
- `pull-requests: write` - To create pull requests
- `issues: write` - To create pull request descriptions

## Environment Variables

The action uses the following environment variables internally:
- `GITHUB_TOKEN` - Set from the `github-token` input parameter

## Example Usage

### Basic Usage

```yaml
name: Scan Workflows for Security Issues

on:
  schedule:
  workflow_dispatch:
jobs:
  security-scan:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Scan and fix workflows
        uses: Scalabit/workflow-scanner@main
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          repository: ${{ github.repository }}
```

## How It Works

1. The action scans all workflow files in `.github/workflows/`
2. An AI agent analyzes each workflow for security vulnerabilities
3. If issues are found, they are automatically fixed
4. A pull request is created with the fixes and a detailed description
5. If no issues are found, the workspace remains unchanged and no PR is created

## License

This project is licensed under the terms included in the LICENSE file.

