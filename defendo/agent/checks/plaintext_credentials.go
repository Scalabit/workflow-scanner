package checks

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

//go:embed tools/trufflehog.exe
var trufflehogBinary []byte

// truffleHogFinding is a single result from TruffleHog's JSON output.
// TruffleHog emits one JSON object per line.
type truffleHogFinding struct {
	DetectorName string `json:"DetectorName"`
	Verified     bool   `json:"Verified"`
	Redacted     string `json:"Redacted"`
	SourceMetadata struct {
		Data struct {
			Filesystem struct {
				File string `json:"file"`
				Line int64  `json:"line"`
			} `json:"Filesystem"`
		} `json:"Data"`
	} `json:"SourceMetadata"`
}

type credentialFinding struct {
	File     string `json:"file"`
	Line     int64  `json:"line"`
	Detector string `json:"detector"`
	Verified bool   `json:"verified"`
	Redacted string `json:"redacted,omitempty"`
}

// PlaintextCredentialsCheck scans the user's home directory for secrets
// using an embedded TruffleHog binary.
type PlaintextCredentialsCheck struct{}

func (c PlaintextCredentialsCheck) Name() string {
	return "Plaintext Credentials"
}

func (c PlaintextCredentialsCheck) Run() Result {
	// Extract the embedded TruffleHog binary to a temp file.
	tmpExe, err := extractEmbeddedBinary(trufflehogBinary, "trufflehog-*.exe")
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("failed to extract TruffleHog: %v", err)}
	}
	defer os.Remove(tmpExe)

	// Determine scan directory: user home folder.
	scanDir := os.Getenv("USERPROFILE")
	if scanDir == "" {
		scanDir = os.Getenv("HOME")
	}
	if scanDir == "" {
		return Result{Name: c.Name(), Status: StatusError, Message: "could not determine user home directory"}
	}

	// Run: trufflehog filesystem --directory=<dir> --json --no-update
	cmd := exec.Command(tmpExe,
		"filesystem",
		"--directory="+scanDir,
		"--json",
		"--no-update",
		"--concurrency=4",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Exit code 183 means findings were found — not a failure.
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 183 {
			return Result{
				Name:    c.Name(),
				Status:  StatusError,
				Message: fmt.Sprintf("TruffleHog scan failed: %v — %s", err, stderr.String()),
			}
		}
	}

	// Parse findings: one JSON object per line.
	var findings []credentialFinding
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var f truffleHogFinding
		if err := json.Unmarshal(line, &f); err != nil {
			continue
		}
		findings = append(findings, credentialFinding{
			File:     f.SourceMetadata.Data.Filesystem.File,
			Line:     f.SourceMetadata.Data.Filesystem.Line,
			Detector: f.DetectorName,
			Verified: f.Verified,
			Redacted: f.Redacted,
		})
	}

	if len(findings) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("No plaintext credentials found in %s.", scanDir),
		}
	}

	verified := 0
	for _, f := range findings {
		if f.Verified {
			verified++
		}
	}

	status := StatusWarning
	msg := fmt.Sprintf("%d credential(s) found in plaintext files under %s.", len(findings), scanDir)
	if verified > 0 {
		status = StatusCritical
		msg = fmt.Sprintf("%d credential(s) found (%d verified/active) in plaintext files under %s.", len(findings), verified, scanDir)
	}

	return Result{
		Name:    c.Name(),
		Status:  status,
		Message: msg,
		Value:   findings,
	}
}

// extractEmbeddedBinary writes binary data to a temp file and returns its path.
func extractEmbeddedBinary(data []byte, pattern string) (string, error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}
	return tmp.Name(), nil
}
