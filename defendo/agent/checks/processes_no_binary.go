//go:build windows

package checks

import (
	"fmt"
	"strconv"
)

// SQL: return any process whose binary no longer exists on disk.
// on_disk = 0  → file is confirmed missing (deleted after process launched).
// on_disk = -1 → indeterminate (e.g. access denied) — excluded deliberately.
const processesNoBinarySQL = `
	SELECT name, pid, path, cmdline
	FROM processes
	WHERE on_disk = 0
`

type suspiciousProcess struct {
	Name    string `json:"name"`
	PID     int    `json:"pid"`
	Path    string `json:"path"`
	Cmdline string `json:"cmdline"`
}

// ProcessesNoBinaryCheck detects running processes whose original executable
// has been deleted from disk — a common indicator of fileless malware or an
// attacker cleaning up traces after launching a payload.
type ProcessesNoBinaryCheck struct{}

func (c ProcessesNoBinaryCheck) Name() string {
	return "Processes Running Without Binary on Disk"
}

func (c ProcessesNoBinaryCheck) Run() Result {
	rows, err := runOsquerySQL(processesNoBinarySQL)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("osquery query failed: %v", err)}
	}

	if len(rows) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No processes found running without their binary on disk.",
		}
	}

	procs := make([]suspiciousProcess, 0, len(rows))
	for _, row := range rows {
		pid, _ := strconv.Atoi(row["pid"])
		procs = append(procs, suspiciousProcess{
			Name:    row["name"],
			PID:     pid,
			Path:    row["path"],
			Cmdline: row["cmdline"],
		})
	}

	return Result{
		Name:   c.Name(),
		Status: StatusCritical,
		Message: fmt.Sprintf(
			"%d process(es) are running whose binary has been deleted from disk — a strong indicator of malicious activity: %s",
			len(procs), uniqueProcessNames(procs),
		),
		Value: procs,
	}
}

func uniqueProcessNames(procs []suspiciousProcess) string {
	seen := make(map[string]bool)
	out := ""
	for _, p := range procs {
		if seen[p.Name] {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += p.Name
		seen[p.Name] = true
	}
	return out
}
