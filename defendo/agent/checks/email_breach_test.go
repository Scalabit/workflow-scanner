//go:build windows

package checks

import (
	"os"
	"testing"
)

// TestResolveEmailNoEnvNoRegistry verifies that resolveEmail returns ("", "")
// when no override env var is set and no registry keys are present.
// In a test environment (macOS cross-compile / CI Windows sandbox) there
// should be no OneDrive or Azure AD join records.
func TestResolveEmailNoEnvNoRegistry(t *testing.T) {
	// Ensure the override env var is not set for this test.
	// t.Setenv restores the original value automatically after the test.
	t.Setenv("GUARDIFY_USER_EMAIL", "")

	// Unset the variable entirely (t.Setenv will restore after).
	os.Unsetenv("GUARDIFY_USER_EMAIL") //nolint:errcheck

	email, source := resolveEmail()

	// On a non-joined, non-OneDrive machine the result should be empty.
	// We treat non-empty as a skip condition (real machine with OneDrive/AAD).
	if email != "" {
		t.Skipf("resolveEmail returned %q (source: %q) — running on a joined/OneDrive machine, skipping", email, source)
	}
	if source != "" {
		t.Errorf("resolveEmail: email is empty but source is %q — expected both empty", source)
	}
}

// TestResolveEmailEnvVarOverride confirms the env-var priority path.
func TestResolveEmailEnvVarOverride(t *testing.T) {
	const testEmail = "test.user@example.com"
	const expectedSource = "GUARDIFY_USER_EMAIL env var"

	// t.Setenv sets the env var and restores original value after the test.
	t.Setenv("GUARDIFY_USER_EMAIL", testEmail)

	email, source := resolveEmail()

	if email != testEmail {
		t.Errorf("resolveEmail: email = %q, want %q", email, testEmail)
	}
	if source != expectedSource {
		t.Errorf("resolveEmail: source = %q, want %q", source, expectedSource)
	}
}
