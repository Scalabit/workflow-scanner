//go:build windows

package checks

// Registry security checks derived from the Palantir osquery configuration pack.
// Each check runs one or more SQL queries against the Windows registry via the
// embedded osqueryi binary and maps the results to a meaningful security finding.
//
// Source: https://github.com/palantir/osquery-configuration/blob/master/Classic/Endpoints/packs/windows-registry-monitoring.conf

import (
	"fmt"
	"strings"
)

// registryFinding is the Value payload for registry-based checks.
type registryFinding struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
	Data string `json:"data,omitempty"`
}

func toRegistryFindings(rows []map[string]string) []registryFinding {
	out := make([]registryFinding, 0, len(rows))
	for _, r := range rows {
		out = append(out, registryFinding{Path: r["path"], Name: r["name"], Data: r["data"]})
	}
	return out
}

// ── 1. AMSI ───────────────────────────────────────────────────────────────────

// AMSICheck verifies that AMSI (Antimalware Scan Interface) has not been
// disabled for any user. AMSI is the Windows hook that lets antivirus engines
// inspect scripts (PowerShell, VBScript, JScript, macros) before they run.
// Disabling it is a well-known attacker technique to bypass on-execution scanning.
type AMSICheck struct{}

func (c AMSICheck) Name() string { return "Windows Script Scanning (AMSI)" }

const amsiSQL = `
	SELECT r.path, r.name, r.data, users.username
	FROM registry r, users
	WHERE r.path = 'HKEY_USERS\' || users.uuid || '\Software\Microsoft\Windows Script\Settings\AmsiEnable'
	  AND r.data = 0`

func (c AMSICheck) Run() Result {
	rows, err := runOsquerySQL(amsiSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "AMSI is enabled on all user accounts — Windows script scanning is active."}
	}

	seen := map[string]bool{}
	users := make([]string, 0, len(rows))
	for _, row := range rows {
		u := row["username"]
		if u != "" && !seen[u] {
			users = append(users, u)
			seen[u] = true
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusCritical,
		Message: fmt.Sprintf("AMSI is disabled for %d user account(s) (%s) — malicious scripts will not be scanned before execution.", len(rows), strings.Join(users, ", ")),
		Value:   toRegistryFindings(rows),
	}
}

// ── 2. SMBv1 ──────────────────────────────────────────────────────────────────

// SMBv1Check verifies that the legacy SMBv1 file-sharing protocol is disabled.
// SMBv1 is the vector exploited by EternalBlue, which powered the WannaCry and
// NotPetya ransomware outbreaks. It has been deprecated since Windows Server 2008.
type SMBv1Check struct{}

func (c SMBv1Check) Name() string { return "SMBv1 Protocol Disabled" }

const smbv1SQL = `
	SELECT data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\Services\LanmanServer\Parameters\SMB1'`

func (c SMBv1Check) Run() Result {
	rows, err := runOsquerySQL(smbv1SQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		// Key absent: on Windows 10/Server 2019+ SMBv1 ships disabled; on older
		// systems (2016 and below) the absence of this key means SMBv1 is ON.
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "SMBv1 registry key not found — SMBv1 may be enabled by default on older Windows builds. Verify with: Get-WindowsOptionalFeature -Online -FeatureName SMB1Protocol",
		}
	}
	if rows[0]["data"] == "0" {
		return Result{Name: c.Name(), Status: StatusOK, Message: "SMBv1 is explicitly disabled — EternalBlue/WannaCry attack surface is closed."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusCritical,
		Message: "SMBv1 is enabled — this host is vulnerable to EternalBlue (WannaCry, NotPetya). Disable it immediately via 'Set-SmbServerConfiguration -EnableSMB1Protocol $false'.",
		Value:   toRegistryFindings(rows),
	}
}

// ── 3. WinRM Secure Configuration ────────────────────────────────────────────

// WinRMSecurityCheck verifies that Windows Remote Management is not configured
// to allow insecure authentication (Basic, Digest, CredSSP) or unencrypted
// traffic — settings that expose credentials in plaintext over the network.
type WinRMSecurityCheck struct{}

func (c WinRMSecurityCheck) Name() string { return "WinRM Secure Configuration" }

const winrmSQL = `
	SELECT path, data FROM registry WHERE (
		path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Client\AllowBasic'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Client\AllowCredSSP'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Client\AllowUnencryptedTraffic'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Client\AllowDigest'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Service\AllowBasic'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Service\AllowCredSSP'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Service\AllowUnencryptedTraffic'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\WinRM\Service\WinRS\AllowRemoteShellAccess'
	) AND data != 0`

func (c WinRMSecurityCheck) Run() Result {
	rows, err := runOsquerySQL(winrmSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "No insecure WinRM settings detected."}
	}

	settings := make([]string, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row["path"], `\`)
		settings = append(settings, parts[len(parts)-1])
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d insecure WinRM setting(s) are enabled (%s) — remote management traffic may be unencrypted or use weak authentication.", len(rows), strings.Join(settings, ", ")),
		Value:   toRegistryFindings(rows),
	}
}

// ── 4. PowerShell Logging ─────────────────────────────────────────────────────

// PowerShellLoggingCheck verifies that all three PowerShell audit logging
// features are enabled: module logging, script block logging, and transcription.
// Together they provide a complete audit trail of PowerShell activity, which is
// essential for detecting and investigating attacks that abuse PowerShell.
type PowerShellLoggingCheck struct{}

func (c PowerShellLoggingCheck) Name() string { return "PowerShell Logging Enabled" }

const psLoggingSQL = `
	SELECT path, data FROM registry WHERE (
		path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\Powershell\ModuleLogging\EnableModuleLogging'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\Powershell\ScriptBlockLogging\EnableScriptBlockLogging'
		OR path = 'HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\Powershell\Transcription\EnableTranscripting'
	) AND data = 1`

func (c PowerShellLoggingCheck) Run() Result {
	rows, err := runOsquerySQL(psLoggingSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) >= 3 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "PowerShell module logging, script block logging, and transcription are all enabled."}
	}

	enabled := map[string]bool{}
	for _, row := range rows {
		enabled[row["path"]] = true
	}
	labels := map[string]string{
		`HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\Powershell\ModuleLogging\EnableModuleLogging`:        "module logging",
		`HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\Powershell\ScriptBlockLogging\EnableScriptBlockLogging`: "script block logging",
		`HKEY_LOCAL_MACHINE\Software\Policies\Microsoft\Windows\Powershell\Transcription\EnableTranscripting`:          "transcription",
	}
	missing := make([]string, 0, 3)
	for path, label := range labels {
		if !enabled[path] {
			missing = append(missing, label)
		}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d/3 PowerShell logging features are not configured (%s) — attacker PowerShell activity may go undetected.", len(missing), strings.Join(missing, ", ")),
	}
}

// ── 5. Command Line Auditing ──────────────────────────────────────────────────

// CommandLineAuditingCheck verifies that Windows logs the full command line
// for every new process. Without it, event log entries for process creation
// show the executable path but not its arguments — making it impossible to
// reconstruct what a suspicious process was asked to do.
type CommandLineAuditingCheck struct{}

func (c CommandLineAuditingCheck) Name() string { return "Command Line Auditing Enabled" }

const cmdAuditSQL = `
	SELECT data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Policies\System\Audit\ProcessCreationIncludeCmdLine_Enabled'
	  AND data = 1`

func (c CommandLineAuditingCheck) Run() Result {
	rows, err := runOsquerySQL(cmdAuditSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 1 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "Command-line auditing is enabled — process creation events include full command line arguments."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: "Command-line auditing is not enabled — Windows event logs will not capture command line arguments for new processes, making incident investigations much harder.",
	}
}

// ── 6. Registry Persistence Artifacts ────────────────────────────────────────

// RegistryPersistenceCheck detects known registry-based attacker persistence
// techniques: RunOnceEx DLL loading (hidden from Autoruns), DNS server plugin
// DLLs, and cryptographic store provider DLL hijacking via PhysicalStores.
type RegistryPersistenceCheck struct{}

func (c RegistryPersistenceCheck) Name() string { return "Registry Persistence Artifacts" }

const (
	runOnceExSQL = `
		SELECT path, name, data FROM registry
		WHERE key = 'HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\RunOnceEx'`

	dnsPluginSQL = `
		SELECT path, name, data FROM registry
		WHERE key  = 'HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\services\DNS\Parameters'
		  AND name = 'ServerLevelPluginDll'`

	certDllSQL = `
		SELECT path, name FROM registry
		WHERE path LIKE 'HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Cryptography\OID\EncodingType 0\CertDllOpenStoreProv\%'
		  AND name != '#16' AND name != 'Ldap'`
)

type persistenceFinding struct {
	Technique string `json:"technique"`
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Data      string `json:"data,omitempty"`
}

func (c RegistryPersistenceCheck) Run() Result {
	type queryDef struct {
		sql       string
		technique string
	}
	queries := []queryDef{
		{runOnceExSQL, "RunOnceEx DLL persistence"},
		{dnsPluginSQL, "DNS server plugin DLL"},
		{certDllSQL, "Certificate store provider DLL hijack"},
	}

	var findings []persistenceFinding
	for _, q := range queries {
		rows, err := runOsquerySQL(q.sql)
		if err != nil {
			return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed (%s): %v", q.technique, err)}
		}
		for _, row := range rows {
			findings = append(findings, persistenceFinding{
				Technique: q.technique,
				Path:      row["path"],
				Name:      row["name"],
				Data:      row["data"],
			})
		}
	}

	if len(findings) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "No known registry-based persistence artifacts detected."}
	}

	techniques := map[string]bool{}
	for _, f := range findings {
		techniques[f.Technique] = true
	}
	names := make([]string, 0, len(techniques))
	for t := range techniques {
		names = append(names, t)
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusCritical,
		Message: fmt.Sprintf("%d registry persistence artifact(s) detected (%s) — these are strong indicators of attacker-established persistence.", len(findings), strings.Join(names, "; ")),
		Value:   findings,
	}
}

// ── 7. Computer Account Password Rotation ────────────────────────────────────

// ComputerPasswordRotationCheck verifies that automatic computer account
// password rotation is not disabled. When rotation is disabled, an attacker
// who has captured a computer account hash can forge Kerberos service tickets
// (silver tickets) indefinitely — long after the initial compromise.
type ComputerPasswordRotationCheck struct{}

func (c ComputerPasswordRotationCheck) Name() string { return "Computer Account Password Rotation" }

const computerPwdSQL = `
	SELECT path, data FROM registry
	WHERE path = 'HKEY_LOCAL_MACHINE\System\CurrentControlSet\Services\Netlogon\Parameters\DisablePasswordChange'
	  AND data != 0`

func (c ComputerPasswordRotationCheck) Run() Result {
	rows, err := runOsquerySQL(computerPwdSQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("registry query failed: %v", err)}
	}
	if len(rows) == 0 {
		return Result{Name: c.Name(), Status: StatusOK, Message: "Computer account password rotation is enabled — Kerberos silver ticket attacks are limited in duration."}
	}
	return Result{
		Name:    c.Name(),
		Status:  StatusCritical,
		Message: "Computer account password rotation is disabled — an attacker with the machine hash can forge Kerberos silver tickets indefinitely without re-authentication.",
		Value:   toRegistryFindings(rows),
	}
}
