//go:build windows

package checks

import (
	"fmt"
	"strconv"

	"golang.org/x/sys/windows/registry"
)

const (
	desktopKey = `Control Panel\Desktop`

	// Screen lock thresholds.
	screenLockWarnSeconds     = 300 // 5 minutes
	screenLockCriticalSeconds = 900 // 15 minutes
)

// ScreenLockCheck verifies that the screen lock timeout is configured and
// that the screen saver requires a password on resume.
type ScreenLockCheck struct{}

func (c ScreenLockCheck) Name() string {
	return "Screen Lock Timeout"
}

func (c ScreenLockCheck) Run() Result {
	// HKCU values — apply to the currently logged-in user.
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopKey, registry.QUERY_VALUE)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("could not open desktop registry key: %v", err)}
	}
	defer k.Close()

	// ScreenSaveActive: "1" = screen saver enabled, "0" = disabled.
	active, _, _ := k.GetStringValue("ScreenSaveActive")
	if active != "1" {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "Screen saver is disabled. The screen will never lock automatically.",
		}
	}

	// ScreenSaverIsSecure: "1" = requires password on resume.
	secure, _, _ := k.GetStringValue("ScreenSaverIsSecure")
	if secure != "1" {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "Screen saver does not require a password on resume. The screen does not lock.",
		}
	}

	// ScreenSaveTimeOut: timeout in seconds (stored as a string).
	timeoutStr, _, err := k.GetStringValue("ScreenSaveTimeOut")
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("could not read ScreenSaveTimeOut: %v", err)}
	}

	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("invalid ScreenSaveTimeOut value %q: %v", timeoutStr, err)}
	}

	minutes := timeout / 60
	msg := fmt.Sprintf("Screen locks after %d minute(s) (%d seconds) and requires a password.", minutes, timeout)

	switch {
	case timeout >= screenLockCriticalSeconds:
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: fmt.Sprintf("Screen lock timeout is %d minute(s) — too long (threshold: %d min).", minutes, screenLockCriticalSeconds/60),
			Value:   map[string]interface{}{"timeout_seconds": timeout, "password_required": true},
		}
	case timeout >= screenLockWarnSeconds:
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Screen lock timeout is %d minute(s) — consider reducing to %d min or less.", minutes, screenLockWarnSeconds/60),
			Value:   map[string]interface{}{"timeout_seconds": timeout, "password_required": true},
		}
	default:
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: msg,
			Value:   map[string]interface{}{"timeout_seconds": timeout, "password_required": true},
		}
	}
}