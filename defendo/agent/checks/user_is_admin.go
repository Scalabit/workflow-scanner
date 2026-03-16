//go:build windows

package checks

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	advapi32     = syscall.NewLazyDLL("advapi32.dll")
	getUserNameW = advapi32.NewProc("GetUserNameW")

	netUserGetLocalGroups = netapi32.NewProc("NetUserGetLocalGroups")
)

// localGroupUsersInfo0 maps to Windows LOCALGROUP_USERS_INFO_0.
// https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-localgroup_users_info_0
type localGroupUsersInfo0 struct {
	Name *uint16 // LPWSTR group name
}

// UserIsAdminCheck reports whether the currently logged-in user is a member
// of the local Administrators group.
type UserIsAdminCheck struct{}

func (c UserIsAdminCheck) Name() string {
	return "Logged-In User in Administrators"
}

func (c UserIsAdminCheck) Run() Result {
	username, err := currentUsername()
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("could not get current username: %v", err)}
	}

	isAdmin, groups, err := userLocalGroups(username)
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("could not query groups for %q: %v", username, err)}
	}

	if isAdmin {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("User %q is a member of the Administrators group. Consider using a standard account for day-to-day work.", username),
			Value:   map[string]interface{}{"username": username, "groups": groups},
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("User %q is not in the Administrators group.", username),
		Value:   map[string]interface{}{"username": username, "groups": groups},
	}
}

// currentUsername returns the username of the process owner via GetUserNameW.
func currentUsername() (string, error) {
	buf := make([]uint16, 256)
	size := uint32(len(buf))

	ret, _, err := getUserNameW.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:size]), nil
}

// userLocalGroups returns whether the user is an admin plus all their local group names.
func userLocalGroups(username string) (isAdmin bool, groups []string, err error) {
	usernamePtr, err := syscall.UTF16PtrFromString(username)
	if err != nil {
		return false, nil, err
	}

	var (
		buf          *localGroupUsersInfo0
		entriesRead  uint32
		totalEntries uint32
	)

	ret, _, _ := netUserGetLocalGroups.Call(
		0,                                      // servername: NULL = local machine
		uintptr(unsafe.Pointer(usernamePtr)),   // username
		0,                                      // level 0
		0,                                      // flags
		uintptr(unsafe.Pointer(&buf)),          // bufptr
		0xFFFFFFFF,                             // prefmaxlen: MAX
		uintptr(unsafe.Pointer(&entriesRead)),
		uintptr(unsafe.Pointer(&totalEntries)),
	)
	if ret != 0 {
		return false, nil, fmt.Errorf("NetUserGetLocalGroups error code %d", ret)
	}
	defer netApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))

	groups = make([]string, 0, entriesRead)
	for i := uint32(0); i < entriesRead; i++ {
		entry := (*localGroupUsersInfo0)(unsafe.Pointer(
			uintptr(unsafe.Pointer(buf)) + uintptr(i)*unsafe.Sizeof(*buf),
		))
		name := syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(entry.Name))[:])
		groups = append(groups, name)
		if name == "Administrators" {
			isAdmin = true
		}
	}

	return isAdmin, groups, nil
}