//go:build windows

package checks

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	netUserGetInfo = netapi32.NewProc("NetUserGetInfo")
)

// userInfo3Partial maps the first three fields of Windows USER_INFO_3.
// We only need up to usri3_password_age so we define a partial struct.
// Layout on 64-bit Windows:
//   offset  0: usri3_name         (LPWSTR, 8 bytes)
//   offset  8: usri3_password     (LPWSTR, 8 bytes)
//   offset 16: usri3_password_age (DWORD,  4 bytes) — seconds since last change
// https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-user_info_3
type userInfo3Partial struct {
	Name        *uint16
	Password    *uint16
	PasswordAge uint32
}

const (
	// Warn if password age exceeds 90 days (≈ 3 months).
	passwordMaxAgeDays    = 90
	passwordMaxAgeSeconds = passwordMaxAgeDays * 24 * 60 * 60
)

// PasswordAgeCheck verifies that the current user's password was changed
// within the last 3 months.
type PasswordAgeCheck struct{}

func (c PasswordAgeCheck) Name() string {
	return "Password Age"
}

func (c PasswordAgeCheck) Run() Result {
	username, err := currentUsername()
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("could not get current username: %v", err)}
	}

	usernamePtr, err := syscall.UTF16PtrFromString(username)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("could not encode username: %v", err)}
	}

	var buf *userInfo3Partial

	ret, _, _ := netUserGetInfo.Call(
		0,                                     // servername: NULL = local machine
		uintptr(unsafe.Pointer(usernamePtr)), // username
		3,                                     // level 3
		uintptr(unsafe.Pointer(&buf)),
	)
	if ret != 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("NetUserGetInfo failed with error code %d", ret),
		}
	}
	defer netApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))

	ageSecs := buf.PasswordAge
	ageDays := ageSecs / 86400

	value := map[string]interface{}{
		"username":  username,
		"age_days":  ageDays,
		"age_secs":  ageSecs,
	}

	switch {
	case ageSecs >= passwordMaxAgeSeconds:
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: fmt.Sprintf("Password for %q is %d day(s) old — exceeds the %d-day limit. Please change it immediately.", username, ageDays, passwordMaxAgeDays),
			Value:   value,
		}
	case ageSecs >= uint32(float64(passwordMaxAgeSeconds)*0.8):
		// Warn when approaching the limit (within the last 18 days of the 90-day window).
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Password for %q is %d day(s) old — expires in %d day(s).", username, ageDays, passwordMaxAgeDays-int(ageDays)),
			Value:   value,
		}
	default:
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("Password for %q is %d day(s) old (limit: %d days).", username, ageDays, passwordMaxAgeDays),
			Value:   value,
		}
	}
}