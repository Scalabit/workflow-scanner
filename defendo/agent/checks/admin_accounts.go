//go:build windows

package checks

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	netLocalGroupGetMembers = netapi32.NewProc("NetLocalGroupGetMembers")
)

// localGroupMembersInfo3 maps to Windows LOCALGROUP_MEMBERS_INFO_3.
// https://learn.microsoft.com/en-us/windows/win32/api/lmaccess/ns-lmaccess-localgroup_members_info_3
type localGroupMembersInfo3 struct {
	DomainAndName *uint16 // LPWSTR "DOMAIN\username"
}

const maxAdminAccounts = 3

// AdminAccountsCheck lists all members of the local Administrators group.
type AdminAccountsCheck struct{}

func (c AdminAccountsCheck) Name() string {
	return "Admin Accounts"
}

func (c AdminAccountsCheck) Run() Result {
	groupName, err := syscall.UTF16PtrFromString("Administrators")
	if err != nil {
		return Result{Name: c.Name(), Status: StatusError, Message: fmt.Sprintf("failed to encode group name: %v", err)}
	}

	var (
		buf          *localGroupMembersInfo3
		entriesRead  uint32
		totalEntries uint32
		resumeHandle uintptr
	)

	ret, _, _ := netLocalGroupGetMembers.Call(
		0,                                    // servername: NULL = local machine
		uintptr(unsafe.Pointer(groupName)),   // localgroupname
		3,                                    // level 3 → LOCALGROUP_MEMBERS_INFO_3
		uintptr(unsafe.Pointer(&buf)),        // bufptr
		0xFFFFFFFF,                           // prefmaxlen: MAX
		uintptr(unsafe.Pointer(&entriesRead)),
		uintptr(unsafe.Pointer(&totalEntries)),
		uintptr(unsafe.Pointer(&resumeHandle)),
	)
	if ret != 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("NetLocalGroupGetMembers failed with error code %d", ret),
		}
	}
	defer netApiBufferFree.Call(uintptr(unsafe.Pointer(buf)))

	// Walk the returned array of LOCALGROUP_MEMBERS_INFO_3 structs.
	accounts := make([]string, 0, entriesRead)
	for i := uint32(0); i < entriesRead; i++ {
		entry := (*localGroupMembersInfo3)(unsafe.Pointer(
			uintptr(unsafe.Pointer(buf)) + uintptr(i)*unsafe.Sizeof(*buf),
		))
		accounts = append(accounts, syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(entry.DomainAndName))[:]))
	}

	count := len(accounts)
	msg := fmt.Sprintf("%d admin account(s) found: %v", count, accounts)

	status := StatusOK
	if count > maxAdminAccounts {
		status = StatusWarning
		msg = fmt.Sprintf("%d admin accounts found (threshold: %d): %v", count, maxAdminAccounts, accounts)
	}

	return Result{
		Name:    c.Name(),
		Status:  status,
		Message: msg,
		Value:   accounts,
	}
}