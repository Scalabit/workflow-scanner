//go:build windows

package checks

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	// TIMEQ_FOREVER: Windows sentinel meaning "password never expires".
	timeqForever = 0xFFFFFFFF

	// Policy thresholds.
	strongMinLength    = 8
	strongMaxAgeDays   = 90
	strongMinAgeDays   = 1
	strongMinHistory   = 5

	lsaKey             = `SYSTEM\CurrentControlSet\Control\Lsa`
	complexityValue    = "PasswordComplexity"
)

type policyRequirement struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// StrongPasswordPolicyCheck validates that the OS enforces a comprehensive
// strong password policy across all five dimensions:
//   1. Minimum length ≥ 8
//   2. Complexity enabled (uppercase + lowercase + digit + special char)
//   3. History ≥ 5 (prevents reuse of recent passwords)
//   4. Maximum age ≤ 90 days (passwords must be rotated)
//   5. Minimum age ≥ 1 day (prevents bypassing history by rapid cycling)
type StrongPasswordPolicyCheck struct{}

func (c StrongPasswordPolicyCheck) Name() string {
	return "Strong Password Policy"
}

func (c StrongPasswordPolicyCheck) Run() Result {
	// Fetch USER_MODALS_INFO_0 for length, age, and history settings.
	var buf *userModalsInfo0
	ret, _, _ := netUserModalsGet.Call(
		0,
		0,
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

	// Fetch complexity flag from LSA registry key.
	complexity := uint64(0)
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, lsaKey, registry.QUERY_VALUE); err == nil {
		complexity, _, _ = k.GetIntegerValue(complexityValue)
		k.Close()
	}

	maxAgeDays := 0
	maxAgeNote := ""
	if buf.MaxPasswdAge == timeqForever || buf.MaxPasswdAge == 0 {
		maxAgeNote = "never expires"
	} else {
		maxAgeDays = int(buf.MaxPasswdAge) / 86400
	}

	minAgeDays := int(buf.MinPasswdAge) / 86400

	reqs := []policyRequirement{
		{
			Name:   "Minimum length ≥ 8",
			Passed: buf.MinPasswdLen >= strongMinLength,
			Detail: fmt.Sprintf("current: %d", buf.MinPasswdLen),
		},
		{
			Name:   "Complexity enforced",
			Passed: complexity == 1,
			Detail: func() string {
				if complexity == 1 {
					return "enabled (uppercase, lowercase, digit, special char required)"
				}
				return "disabled"
			}(),
		},
		{
			Name:   fmt.Sprintf("Password history ≥ %d", strongMinHistory),
			Passed: buf.PasswordHistLen >= strongMinHistory,
			Detail: fmt.Sprintf("current: %d password(s) remembered", buf.PasswordHistLen),
		},
		{
			Name:   fmt.Sprintf("Maximum age ≤ %d days", strongMaxAgeDays),
			Passed: buf.MaxPasswdAge != timeqForever && buf.MaxPasswdAge != 0 && maxAgeDays <= strongMaxAgeDays,
			Detail: func() string {
				if maxAgeNote != "" {
					return maxAgeNote
				}
				return fmt.Sprintf("current: %d day(s)", maxAgeDays)
			}(),
		},
		{
			Name:   fmt.Sprintf("Minimum age ≥ %d day", strongMinAgeDays),
			Passed: minAgeDays >= strongMinAgeDays,
			Detail: fmt.Sprintf("current: %d day(s)", minAgeDays),
		},
	}

	failed := []string{}
	for _, r := range reqs {
		if !r.Passed {
			failed = append(failed, r.Name)
		}
	}

	if len(failed) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "All password policy requirements are met.",
			Value:   reqs,
		}
	}

	status := StatusWarning
	if len(failed) >= 3 {
		status = StatusCritical
	}

	return Result{
		Name:    c.Name(),
		Status:  status,
		Message: fmt.Sprintf("%d of %d password policy requirement(s) not met: %s.", len(failed), len(reqs), strings.Join(failed, "; ")),
		Value:   reqs,
	}
}