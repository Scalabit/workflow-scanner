#!/bin/bash
set -e

echo "Running all tests for dagger/workflow-scanner..."
echo "================================================"

# Set required Dagger environment variables for tests
export DAGGER_SESSION_PORT=1234
export DAGGER_SESSION_TOKEN=test

echo "Running all package tests..."
go test ./pkg/... -v

echo ""
echo "Running main package tests..."
go test . -v

echo ""
echo "All tests passed!"