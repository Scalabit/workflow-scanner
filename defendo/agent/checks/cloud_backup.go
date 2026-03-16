//go:build windows

package checks

import (
	"fmt"
	"strings"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"
)

// win32Service maps to Win32_Service WMI class.
type win32Service struct {
	Name      string
	DisplayName string
	State     string // "Running", "Stopped", ...
	StartMode string // "Auto", "Manual", "Disabled", ...
}

// cloudProvider describes how to detect a cloud backup solution.
type cloudProvider struct {
	Name         string
	ServiceNames []string // Windows service names to probe
	Detect       func() (active bool, detail string) // optional custom detection
}

var cloudProviders = []cloudProvider{
	{
		Name:         "OneDrive",
		Detect:       detectOneDrive,
	},
	{
		Name:         "Backblaze",
		ServiceNames: []string{"backblaze"},
	},
	{
		Name:         "Acronis",
		ServiceNames: []string{"AcrSch2Svc", "AcronisAgent", "BackupAndRecoveryAgent"},
	},
	{
		Name:         "Veeam",
		ServiceNames: []string{"VeeamBackupSvc", "VeeamEndpointBackupSvc"},
	},
	{
		Name:         "CrashPlan",
		ServiceNames: []string{"CrashPlanService"},
	},
	{
		Name:         "Carbonite",
		ServiceNames: []string{"CarboniteService"},
	},
}

type backupProviderResult struct {
	Provider string `json:"provider"`
	Active   bool   `json:"active"`
	Detail   string `json:"detail"`
}

// CloudBackupCheck verifies that at least one cloud backup solution is active.
type CloudBackupCheck struct{}

func (c CloudBackupCheck) Name() string {
	return "Cloud Backup Active"
}

func (c CloudBackupCheck) Run() Result {
	// Pre-fetch all running services once to avoid repeated WMI calls.
	serviceState, err := queryServices()
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("WMI service query failed: %v", err)}
	}

	var results []backupProviderResult
	activeCount := 0

	for _, p := range cloudProviders {
		var active bool
		var detail string

		if p.Detect != nil {
			active, detail = p.Detect()
		} else {
			active, detail = checkServices(p.ServiceNames, serviceState)
		}

		if active || detail != "" {
			results = append(results, backupProviderResult{
				Provider: p.Name,
				Active:   active,
				Detail:   detail,
			})
			if active {
				activeCount++
			}
		}
	}

	if activeCount == 0 && len(results) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "No cloud backup solution detected on this machine.",
		}
	}

	if activeCount == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Cloud backup software found but none are currently active: %s", providerNames(results)),
			Value:   results,
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%d active cloud backup provider(s): %s", activeCount, providerNames(results)),
		Value:   results,
	}
}

// queryServices fetches all Windows services into a map[lowercase_name]State.
func queryServices() (map[string]win32Service, error) {
	var services []win32Service
	q := wmi.CreateQuery(&services, "")
	if err := wmi.Query(q, &services); err != nil {
		return nil, err
	}
	m := make(map[string]win32Service, len(services))
	for _, s := range services {
		m[strings.ToLower(s.Name)] = s
	}
	return m, nil
}

// checkServices returns (active, detail) for a set of service names.
func checkServices(names []string, services map[string]win32Service) (bool, string) {
	for _, name := range names {
		svc, ok := services[strings.ToLower(name)]
		if !ok {
			continue
		}
		running := svc.State == "Running"
		return running, fmt.Sprintf("service %q is %s (start mode: %s)", svc.DisplayName, strings.ToLower(svc.State), svc.StartMode)
	}
	return false, ""
}

// detectOneDrive checks the registry for a signed-in OneDrive account and
// whether Known Folder Move (Desktop/Documents/Pictures backup) is enabled.
func detectOneDrive() (bool, string) {
	// Check personal and business accounts.
	accountPaths := []string{
		`SOFTWARE\Microsoft\OneDrive\Accounts\Personal`,
		`SOFTWARE\Microsoft\OneDrive\Accounts\Business1`,
		`SOFTWARE\Microsoft\OneDrive\Accounts\Business2`,
	}

	for _, path := range accountPaths {
		k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		email, _, _ := k.GetStringValue("UserEmail")
		// KfmFoldersProtectedNow is a bitmask: Desktop=1, Documents=2, Pictures=4
		kfm, _, _ := k.GetIntegerValue("KfmFoldersProtectedNow")
		k.Close()

		if email == "" {
			continue
		}

		if kfm > 0 {
			folders := decodeKFMFolders(kfm)
			return true, fmt.Sprintf("account %q — Known Folder Move active for: %s", email, strings.Join(folders, ", "))
		}

		return true, fmt.Sprintf("account %q signed in but Known Folder Move (Desktop/Documents/Pictures backup) is not enabled", email)
	}

	return false, ""
}

// decodeKFMFolders returns the folder names protected by OneDrive KFM.
func decodeKFMFolders(mask uint64) []string {
	var folders []string
	if mask&1 != 0 {
		folders = append(folders, "Desktop")
	}
	if mask&2 != 0 {
		folders = append(folders, "Documents")
	}
	if mask&4 != 0 {
		folders = append(folders, "Pictures")
	}
	return folders
}

func providerNames(results []backupProviderResult) string {
	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, r.Provider)
	}
	return strings.Join(names, ", ")
}