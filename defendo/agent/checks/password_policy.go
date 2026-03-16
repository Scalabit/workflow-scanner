//go:build windows

package checks

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	netapi32         = syscall.NewLazyDLL("netapi32.dll")
	netUserModalsGet = netapi32.NewProc("NetUserModalsGet")
	netApiBufferFree = netapi32.NewProc("NetApiBufferFree")
)

// userModalsInfo0 maps to Windows USER_MODALS_INFO_0.
// https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-user_modals_info_0
type userModalsInfo0 struct {
	MinPasswdLen    uint32
	MaxPasswdAge    uint32
	MinPasswdAge    uint32
	ForceLogoff     uint32
	PasswordHistLen uint32
}

const minRecommendedLen = 8

// PasswordMinLengthCheck verifies that the OS enforces a minimum password length.
type PasswordMinLengthCheck struct{}

func (c PasswordMinLengthCheck) Name() string {
	return "Password Minimum Length"
}

func (c PasswordMinLengthCheck) Run() Result {
	var buf *userModalsInfo0

	ret, _, _ := netUserModalsGet.Call(
		0, // servername: NULL = local machine
		0, // level 0
		uintptr(unsafe.Pointer(&buf)),
	)
	if ret != 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("NetUserModalsGet failed with error code %d", ret),
		}
	}
	defer netApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))

	length := buf.MinPasswdLen

	switch {
	case length == 0:
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "No minimum password length is enforced.",
			Value:   length,
		}
	case length < minRecommendedLen:
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Minimum password length (%d) is below the recommended minimum of %d.", length, minRecommendedLen),
			Value:   length,
		}
	default:
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("Minimum password length is %d.", length),
			Value:   length,
		}
	}
}