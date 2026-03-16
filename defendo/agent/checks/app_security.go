//go:build windows

package checks

// Application security checks derived from the Palantir windows-application-security.conf osquery pack.
// Source: https://github.com/palantir/osquery-configuration/blob/master/Classic/Endpoints/packs/windows-application-security.conf

import (
	"fmt"
	"strings"
)

// ── 1. UAC ────────────────────────────────────────────────────────────────────

// UACCheck verifies that User Account Control (UAC) is enabled.
// UAC prompts users before allowing changes that require administrator rights,
// making it significantly harder for malware to silently elevate privileges.
// A value of 0 in EnableLUA means UAC has been fully disabled.
type UACCheck struct{}

func (c UACCheck) Name() string { return "User Account Control (UAC) Enabled" }

const uacSQL = `
	SELECT data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Policies\System\EnableLUA'
	  AND data = 0`

func (c UACCheck) Run() Result {
	rows, err := runOsquerySQL(uacSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "UAC is enabled — privilege escalation requires explicit user approval."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusCritical,
		Message: "UAC is disabled — any program can silently elevate to administrator without prompting the user, making malware installation trivial.",
		Value:   toRegistryFindings(rows),
	}
}

// ── 2. LAPS ───────────────────────────────────────────────────────────────────

// LAPSCheck verifies that Microsoft LAPS (Local Administrator Password Solution)
// is configured. LAPS automatically rotates the local administrator password on
// every managed device and stores it securely in Active Directory. Without it,
// all machines often share the same local admin password — meaning a single
// compromised device gives an attacker admin access to the entire fleet.
type LAPSCheck struct{}

func (c LAPSCheck) Name() string { return "LAPS (Local Admin Password Solution)" }

const lapsClassicSQL = `
	SELECT name, data FROM registry
	WHERE key = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft Services\AdmPwd'`

const lapsNewSQL = `
	SELECT name, data FROM registry
	WHERE key = 'HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\LAPS\Config'`

func (c LAPSCheck) Run() Result {
	classic, err := runOsquerySQL(lapsClassicSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	newLAPS, err := runOsquerySQL(lapsNewSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}

	if len(classic) > 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "Microsoft LAPS (classic) is configured — local administrator passwords are automatically rotated per device."}
	}
	if len(newLAPS) > 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "Microsoft LAPS (Windows built-in) is configured — local administrator passwords are automatically rotated per device."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "LAPS is not configured — all devices may share the same local administrator password. A single compromised device could grant admin access across the entire fleet.",
	}
}

// ── 3. Windows Hello for Business ────────────────────────────────────────────

// WindowsHelloCheck verifies that Windows Hello for Business is configured
// via Group Policy. Windows Hello replaces passwords with strong cryptographic
// credentials tied to the device — PIN, fingerprint, or face — making phishing
// and credential theft attacks largely ineffective.
type WindowsHelloCheck struct{}

func (c WindowsHelloCheck) Name() string { return "Windows Hello for Business" }

const helloSQL = `
	SELECT name, data FROM registry
	WHERE key = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\PassportForWork'`

func (c WindowsHelloCheck) Run() Result {
	rows, err := runOsquerySQL(helloSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	for _, row := range rows {
		if strings.EqualFold(row["name"], "Enabled") && row["data"] == "1" {
			return Result{Name: c.Name(), Status: StatusOK, Message: "Windows Hello for Business is enabled via Group Policy — passwordless strong authentication is enforced."}
		}
		if strings.EqualFold(row["name"], "Enabled") && row["data"] == "0" {
			return Result{
				Name:    c.Name(),
				Status:  StatusWarning,
				Message: "Windows Hello for Business is explicitly disabled via Group Policy — passwordless authentication is not available.",
			}
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "Windows Hello for Business Group Policy is not configured — passwordless strong authentication is not enforced. Configure it to eliminate password-based phishing risk.",
	}
}

// ── 4. Force-Installed Chrome Extensions ─────────────────────────────────────

// ChromeForcedExtensionsCheck detects browser extensions that have been
// forcibly installed via Group Policy. While legitimate IT deployments use
// this mechanism, attackers who compromise Group Policy can abuse it to deploy
// malicious extensions across every managed device — capturing passwords,
// intercepting web traffic, or exfiltrating data silently.
type ChromeForcedExtensionsCheck struct{}

func (c ChromeForcedExtensionsCheck) Name() string { return "Force-Installed Chrome Extensions" }

const chromeExtSQL = `
	SELECT name, data FROM registry
	WHERE key = 'HKEY_LOCAL_MACHINE\Software\Policies\Google\Chrome\ExtensionInstallForcelist'`

func (c ChromeForcedExtensionsCheck) Run() Result {
	rows, err := runOsquerySQL(chromeExtSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "No force-installed Chrome extensions found via Group Policy."}
	}

	type extFinding struct {
		Entry       string `json:"entry"`
		ExtensionID string `json:"extension_id"`
		UpdateURL   string `json:"update_url,omitempty"`
	}
	findings := make([]extFinding, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		// Registry value format: "extension_id;update_url"
		data := row["data"]
		parts := strings.SplitN(data, ";", 2)
		f := extFinding{Entry: data, ExtensionID: parts[0]}
		if len(parts) == 2 {
			f.UpdateURL = parts[1]
		}
		findings = append(findings, f)
		ids = append(ids, parts[0])
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d Chrome extension(s) are force-installed via Group Policy (%s) — verify these are authorised IT deployments and not a sign of Group Policy compromise.", len(findings), strings.Join(ids, ", ")),
		Value:   findings,
	}
}
