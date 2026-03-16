//go:build windows

package checks

import (
	"fmt"
	"strings"

	"github.com/yusufpapurcu/wmi"
)

// encryptableVolume maps to Win32_EncryptableVolume WMI class.
// Namespace: root\cimv2\Security\MicrosoftVolumeEncryption
// https://learn.microsoft.com/en-us/windows/win32/secprov/win32-encryptablevolume
type encryptableVolume struct {
	DeviceID         string
	ProtectionStatus uint32
}

// protectionStatus maps the WMI ProtectionStatus values.
// 0 = Unprotected, 1 = Protected, 2 = Unknown
var protectionStatus = map[uint32]string{
	0: "Unprotected",
	1: "Protected",
	2: "Unknown",
}

// BitLockerCheck verifies that BitLocker is enabled on all fixed drives.
type BitLockerCheck struct{}

func (c BitLockerCheck) Name() string {
	return "BitLocker Status"
}

func (c BitLockerCheck) Run() Result {
	var volumes []encryptableVolume

	q := wmi.CreateQuery(&volumes, "")
	if err := wmi.QueryNamespace(q, &volumes, `root\cimv2\Security\MicrosoftVolumeEncryption`); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("WMI query failed: %v", err),
		}
	}

	if len(volumes) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No encryptable volumes found.",
		}
	}

	type volumeSummary struct {
		DeviceID string `json:"device_id"`
		Status   string `json:"status"`
	}

	var unprotected []string
	summary := make([]volumeSummary, 0, len(volumes))

	for _, v := range volumes {
		label := protectionStatus[v.ProtectionStatus]
		summary = append(summary, volumeSummary{DeviceID: v.DeviceID, Status: label})
		if v.ProtectionStatus != 1 {
			unprotected = append(unprotected, v.DeviceID)
		}
	}

	if len(unprotected) > 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: fmt.Sprintf("%d volume(s) are not protected by BitLocker: %s", len(unprotected), strings.Join(unprotected, ", ")),
			Value:   summary,
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d volume(s) are protected by BitLocker.", len(volumes)),
		Value:   summary,
	}
}