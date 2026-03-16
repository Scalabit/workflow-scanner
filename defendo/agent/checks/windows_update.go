//go:build windows

package checks

import (
	"golang.org/x/sys/windows/registry"
)

const (
	// Group Policy: disables automatic updates when set to 1.
	auPolicyKey   = `SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU`
	noAutoUpdate  = "NoAutoUpdate"

	// Windows Update service start type values.
	wuServiceKey  = `SYSTEM\CurrentControlSet\Services\wuauserv`
	serviceStart  = "Start"
	startAuto     = 2 // Automatic
	startManual   = 3 // Manual
	startDisabled = 4 // Disabled
)

// WindowsUpdateCheck verifies that Windows Update is enabled on the machine.
type WindowsUpdateCheck struct{}

func (c WindowsUpdateCheck) Name() string {
	return "Windows Update Enabled"
}

func (c WindowsUpdateCheck) Run() Result {
	// 1. Check if a Group Policy has disabled automatic updates.
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, auPolicyKey, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if val, _, err := k.GetIntegerValue(noAutoUpdate); err == nil && val == 1 {
			return Result{
				Name:    c.Name(),
				Status:  StatusCritical,
				Message: "Automatic Windows Update is disabled by Group Policy (NoAutoUpdate=1).",
				Value:   false,
			}
		}
	}

	// 2. Check the Windows Update service (wuauserv) start type.
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, wuServiceKey, registry.QUERY_VALUE)
	if err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Could not read Windows Update service registry key.",
		}
	}
	defer k.Close()

	startType, _, err := k.GetIntegerValue(serviceStart)
	if err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Could not read Windows Update service start type.",
		}
	}

	switch startType {
	case startDisabled:
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "Windows Update service (wuauserv) is disabled.",
			Value:   false,
		}
	case startManual:
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "Windows Update service (wuauserv) is set to Manual — updates may not apply automatically.",
			Value:   false,
		}
	default:
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "Windows Update service is enabled and set to Automatic.",
			Value:   true,
		}
	}
}