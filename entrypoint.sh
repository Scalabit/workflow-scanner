#!/bin/bash
set -e

export GITHUB_TOKEN="$1"
REPOSITORY="$2"
DAGGER_MODULE="${3:-dagger}"

cd "$DAGGER_MODULE"

PR_URL=$(dagger call scan-and-fix-workflows \
  --github-token="env:GITHUB_TOKEN" \
  --repository="$REPOSITORY" \
  --source="..") || true

if [ -n "$PR_URL" ] && [ "$PR_URL" != "null" ]; then
  echo "pr-url=$PR_URL" >> "$GITHUB_OUTPUT"
  echo "Pull request created: $PR_URL"
else
  echo "No security issues found. No PR created."
  echo "pr-url=" >> "$GITHUB_OUTPUT"
fi