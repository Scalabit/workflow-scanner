//go:build windows

package checks

// Compliance checks derived from the Palantir windows-compliance.conf osquery pack.
// Source: https://github.com/palantir/osquery-configuration/blob/master/Classic/Endpoints/packs/windows-compliance.conf

import "fmt"

// ── 1. Secure Boot ────────────────────────────────────────────────────────────

// SecureBootCheck verifies that UEFI Secure Boot is enabled. Secure Boot
// prevents unauthorised bootloaders and OS kernels from loading at startup,
// blocking bootkits and firmware-level persistence mechanisms.
type SecureBootCheck struct{}

func (c SecureBootCheck) Name() string { return "Secure Boot Enabled" }

const secureBootSQL = `
	SELECT data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\SecureBoot\State\UEFISecureBootEnabled'`

func (c SecureBootCheck) Run() Result {
	rows, err := runOsquerySQL(secureBootSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Secure Boot registry key not found — the system may be using legacy BIOS mode or Secure Boot is unsupported on this hardware.",
		}
	}
	if rows[0]["data"] == "1" {
		return Result{Name: c.Name(), Status: StatusOK, Message: "UEFI Secure Boot is enabled — the system is protected against bootkit attacks."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "UEFI Secure Boot is disabled — the system may be vulnerable to bootkit and UEFI-level malware that persists below the operating system.",
		Value:   toRegistryFindings(rows),
	}
}

// ── 2. TPM ────────────────────────────────────────────────────────────────────

// TPMCheck verifies that a Trusted Platform Module (TPM) chip is present,
// enabled, and activated. TPM is required for BitLocker full-disk encryption
// and Windows Hello credential isolation.
type TPMCheck struct{}

func (c TPMCheck) Name() string { return "TPM Security Chip" }

const tpmSQL = `SELECT activated, enabled, owned, spec_version FROM tpm_info`

func (c TPMCheck) Run() Result {
	rows, err := runOsquerySQL(tpmSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("TPM query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No TPM chip detected — hardware-backed encryption (BitLocker) and secure credential storage (Windows Hello) are not available.",
		}
	}
	row := rows[0]
	if row["enabled"] == "1" && row["activated"] == "1" {
		return Result{Name: c.Name(), Status: StatusOK, Message: fmt.Sprintf("TPM %s is present, enabled, and activated.", row["spec_version"])}
	}
	status := "present but not fully operational"
	if row["enabled"] != "1" {
		status = "present but disabled — enable it in BIOS/UEFI settings"
	} else if row["activated"] != "1" {
		status = "present but not yet activated"
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("TPM %s is %s — hardware security features are unavailable.", row["spec_version"], status),
	}
}

// ── 3. RDP Exposure ───────────────────────────────────────────────────────────

// RDPExposureCheck detects whether Remote Desktop Protocol (RDP) is enabled.
// RDP on port 3389 is one of the most exploited attack surfaces, used in
// ransomware campaigns, brute-force attacks, and initial access by threat actors.
type RDPExposureCheck struct{}

func (c RDPExposureCheck) Name() string { return "Remote Desktop (RDP) Exposure" }

const rdpSQL = `
	SELECT data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Terminal Server\fDenyTSConnections'`

func (c RDPExposureCheck) Run() Result {
	rows, err := runOsquerySQL(rdpSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	// data=0 means connections are ALLOWED; an absent key also defaults to enabled.
	if len(rows) == 0 || rows[0]["data"] == "0" {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Remote Desktop (RDP) is enabled — ensure access is restricted by firewall rules and protected by MFA. RDP on port 3389 is the most common ransomware and brute-force entry point.",
		}
	}
	return Result{Name: c.Name(), Status: StatusOK, Message: "Remote Desktop (RDP) is disabled — the most common ransomware entry point is closed."}
}

// ── 4. LSA Credential Guard ───────────────────────────────────────────────────

// LSAProtectionCheck verifies that the Local Security Authority (LSA) process
// is running as a Protected Process Light (PPL). This prevents credential-dumping
// tools (e.g. Mimikatz) from reading passwords and NTLM hashes from LSA memory —
// a technique used in virtually every modern post-exploitation playbook.
type LSAProtectionCheck struct{}

func (c LSAProtectionCheck) Name() string { return "LSA Credential Guard" }

const lsaProtectionSQL = `
	SELECT data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Control\Lsa\RunAsPPL'
	  AND data = 1`

func (c LSAProtectionCheck) Run() Result {
	rows, err := runOsquerySQL(lsaProtectionSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 1 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "LSA is running as a Protected Process (PPL) — credential-dumping tools cannot read passwords or hashes from memory."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "LSA Protected Process (RunAsPPL) is not enabled — tools like Mimikatz can extract passwords and NTLM hashes from system memory. Enable via Group Policy or: reg add HKLM\\SYSTEM\\CurrentControlSet\\Control\\Lsa /v RunAsPPL /t REG_DWORD /d 1",
	}
}

// ── 5. Crash Reporting Integrity ─────────────────────────────────────────────

// CrashReportingIntegrityCheck detects whether Windows crash reporting settings
// have been tampered with. WannaCry and other malware disable crash dumps and
// error reporting to avoid leaving forensic artefacts after exploitation.
type CrashReportingIntegrityCheck struct{}

func (c CrashReportingIntegrityCheck) Name() string { return "Crash Reporting Integrity" }

const crashReportSQL = `
	SELECT path, data FROM registry WHERE (
		(path = 'HKEY_LOCAL_MACHINE\System\CurrentControlSet\Control\CrashControl\CrashDumpEnabled' AND data = 0)
		OR (path = 'HKEY_LOCAL_MACHINE\System\CurrentControlSet\Control\CrashControl\LogEvent' AND data = 0)
		OR (path = 'HKEY_LOCAL_MACHINE\System\CurrentControlSet\Control\Windows\ErrorMode' AND data = 2)
		OR (path = 'HKEY_LOCAL_MACHINE\Software\Microsoft\PCHealth\ErrorReporting\ShowUI' AND data = 0)
		OR (path = 'HKEY_LOCAL_MACHINE\Software\Microsoft\PCHealth\ErrorReporting\DoReport' AND data = 0)
	)`

func (c CrashReportingIntegrityCheck) Run() Result {
	rows, err := runOsquerySQL(crashReportSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "Crash reporting settings are intact — no signs of malware tampering with error reporting."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusCritical,
		Message: fmt.Sprintf("%d crash reporting registry key(s) have been disabled — this is a known WannaCry/malware indicator used to suppress crash dumps and destroy forensic evidence.", len(rows)),
		Value:   toRegistryFindings(rows),
	}
}

// ── 6. Post-Mortem Debugger Integrity ────────────────────────────────────────

// PostMortemDebuggerCheck verifies that the Windows post-mortem debugger
// registry key (AeDebug) is intact. Some malware — including WannaCry —
// deletes this key to prevent crash dump files from being written, eliminating
// a key forensic artefact that would reveal the exploit used.
type PostMortemDebuggerCheck struct{}

func (c PostMortemDebuggerCheck) Name() string { return "Post-Mortem Debugger Integrity" }

const drWatsonSQL = `
	SELECT name, data FROM registry
	WHERE key = 'HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\AeDebug'`

func (c PostMortemDebuggerCheck) Run() Result {
	rows, err := runOsquerySQL(drWatsonSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) >= 2 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "Post-mortem debugger key (AeDebug) is intact — crash dumps can be captured for forensic analysis."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "Windows post-mortem debugger key (AeDebug) is missing or incomplete — some malware (including WannaCry) deletes this key to prevent crash analysis and hide evidence of exploitation.",
	}
}
