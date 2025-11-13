# Workflow Security Scanner Action

An AI-powered GitHub Action that automatically scans your GitHub Actions workflows for security vulnerabilities and creates pull requests with fixes.

## Description

This action uses Dagger (as a composite action) and AI to analyze your GitHub Actions workflows for common security issues. 

#TODO: See if Docker image + entrypoint script, instead of composite, can be better.

`action.yml`
```
runs:
  using: 'composite'
```

When security issues are found, the action automatically creates a pull request with the necessary fixes and a detailed description of the changes.

## Inputs

### Required

#### `github-token`
**Required** GitHub Personal Access Token (PAT) with permissions to write issues, repository contents, and workflows.

**Note:** Cannot use `GITHUB_TOKEN` because it lacks permission to modify workflow files. You must create a PAT with `repo` scope.

**Example:** `${{ secrets.PAT_TOKEN }}`

#### `repository`
**Optional** The GitHub repository to scan in the format `owner/repo`. 

**Default:** `${{ github.repository }}` (the repository where the action is running)

**Example:** `octocat/hello-world`

#### `openai-api-key`
**Required** API key for LLM analysis. The action supports multiple LLM providers based on the secret name:
- `OPENAI_API_KEY` - Uses OpenAI (GPT models)
- `ANTHROPIC_API_KEY` - Uses Anthropic (Claude models)
- `GEMINI_API_KEY` - Uses Google Gemini

**Example:** `${{ secrets.OPENAI_API_KEY }}` or `${{ secrets.ANTHROPIC_API_KEY }}` or `${{ secrets.GEMINI_API_KEY }}`

## Outputs

### `pr-url`
The URL of the created pull request containing the security fixes.

## Secrets Used

This action requires the following secrets:

### `PAT_TOKEN` (GitHub Personal Access Token)
A Personal Access Token with the following permissions:
- `repo` - Full control of private repositories (includes all permissions below)
  - `contents: write` - To create pull requests with workflow fixes
  - `pull-requests: write` - To create pull requests
  - `issues: write` - To create pull request descriptions

**Why PAT is required:** The built-in `GITHUB_TOKEN` cannot modify workflow files (`.github/workflows/`) for security reasons. You must create a PAT at https://github.com/settings/tokens

### `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY`
Your LLM API key for AI-powered security analysis. The action automatically detects which provider to use based on the environment variable name:
- **OpenAI:** Set `OPENAI_API_KEY` to use GPT models
- **Anthropic:** Set `ANTHROPIC_API_KEY` to use Claude models  
- **Google Gemini:** Set `GEMINI_API_KEY` to use Gemini models

## Environment Variables

The action uses the following environment variables internally:
- `GITHUB_TOKEN` - Set from the `github-token` input parameter (your PAT)
- `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` - Set from the `openai-api-key` input parameter (provider auto-detected)

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
          github-token: ${{ secrets.PAT_TOKEN }}
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
```

## How It Works

1. The action scans all workflow files in `.github/workflows/`
2. An AI agent analyzes each workflow for security vulnerabilities
3. If issues are found, they are automatically fixed
4. A pull request is created with the fixes and a detailed description
5. If no issues are found, the workspace remains unchanged and no PR is created

## License

This project is licensed under the terms included in the LICENSE file.

## Next steps
- See if Docker image + entrypoint script, instead of composite, can be better.
- Don't make this repo public until we remove the LLM KEY and PAT from secrets.
- See what are the possibilities of using GITHUB_TOKEN instead PAT_TOKEN.