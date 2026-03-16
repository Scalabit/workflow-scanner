//go:build windows

package checks

import (
	"fmt"
	"os"
	"runtime"

	"github.com/yusufpapurcu/wmi"
)

// SystemInfo holds machine-level metadata included in every JSON report.
type SystemInfo struct {
	Hostname     string `json:"hostname"`
	Username     string `json:"username"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version"`
	OSBuild      string `json:"os_build"`
	Architecture string `json:"architecture"`
	CPU          string `json:"cpu"`
	CPUCores     uint32 `json:"cpu_cores"`
	CPUThreads   uint32 `json:"cpu_threads"`
	TotalRAMMB   uint64 `json:"total_ram_mb"`
	Domain       string `json:"domain,omitempty"`
}

type win32OS struct {
	Caption     string
	Version     string
	BuildNumber string
}

type win32Processor struct {
	Name                      string
	NumberOfCores             uint32
	NumberOfLogicalProcessors uint32
}

type win32ComputerSystem struct {
	TotalPhysicalMemory uint64
	Domain              string
}

// GetSystemInfo collects hardware and OS metadata for the current machine.
func GetSystemInfo() SystemInfo {
	info := SystemInfo{
		Architecture: runtime.GOARCH,
	}

	// Hostname.
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// Current username (reuse helper already in user_is_admin.go).
	if u, err := currentUsername(); err == nil {
		info.Username = u
	}

	// OS info via Win32_OperatingSystem.
	var osItems []win32OS
	if err := wmi.Query(wmi.CreateQuery(&osItems, ""), &osItems); err == nil && len(osItems) > 0 {
		info.OS = osItems[0].Caption
		info.OSVersion = osItems[0].Version
		info.OSBuild = osItems[0].BuildNumber
	}

	// CPU info via Win32_Processor.
	var procs []win32Processor
	if err := wmi.Query(wmi.CreateQuery(&procs, ""), &procs); err == nil && len(procs) > 0 {
		info.CPU = procs[0].Name
		info.CPUCores = procs[0].NumberOfCores
		info.CPUThreads = procs[0].NumberOfLogicalProcessors
	}

	// RAM + domain via Win32_ComputerSystem.
	var cs []win32ComputerSystem
	if err := wmi.Query(wmi.CreateQuery(&cs, ""), &cs); err == nil && len(cs) > 0 {
		info.TotalRAMMB = cs[0].TotalPhysicalMemory / 1024 / 1024
		if cs[0].Domain != "" && cs[0].Domain != "WORKGROUP" {
			info.Domain = cs[0].Domain
		}
	}

	return info
}

// FormatRAM returns a human-readable RAM string (e.g. "15.9 GB").
func (s SystemInfo) FormatRAM() string {
	gb := float64(s.TotalRAMMB) / 1024
	return fmt.Sprintf("%.1f GB", gb)
}
