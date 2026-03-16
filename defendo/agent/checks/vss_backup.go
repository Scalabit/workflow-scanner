//go:build windows

package checks

import (
	"fmt"
	"sort"
	"time"

	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows/registry"
)

const (
	// Warn if the most recent snapshot is older than this.
	snapshotWarnAgeDays = 7

	// VSS service registry key.
	vssServiceKey = `SYSTEM\CurrentControlSet\Services\VSS`
)

// win32ShadowCopy maps to the Win32_ShadowCopy WMI class.
// https://learn.microsoft.com/en-us/previous-versions/windows/desktop/vsswmi/win32-shadowcopy
type win32ShadowCopy struct {
	ID          string
	VolumeName  string
	InstallDate time.Time
}

type snapshotSummary struct {
	ID        string    `json:"id"`
	Volume    string    `json:"volume"`
	CreatedAt time.Time `json:"created_at"`
	AgeDays   int       `json:"age_days"`
}

// VSSBackupCheck verifies that VSS is running and recent snapshots exist.
type VSSBackupCheck struct{}

func (c VSSBackupCheck) Name() string {
	return "VSS / Backup Snapshots"
}

func (c VSSBackupCheck) Run() Result {
	// 1. Check VSS service start type.
	vssStatus := checkVSSService()
	if vssStatus == StatusError {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Could not read Volume Shadow Copy Service (VSS) registry configuration.",
		}
	}
	if vssStatus == StatusCritical {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "Volume Shadow Copy Service (VSS) is disabled.",
		}
	}

	// 2. Query existing shadow copies.
	var copies []win32ShadowCopy
	q := wmi.CreateQuery(&copies, "")
	if err := wmi.Query(q, &copies); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("WMI Win32_ShadowCopy query failed: %v", err),
		}
	}

	if len(copies) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "No VSS shadow copies found. The system has no backup snapshots.",
		}
	}

	// Sort by date descending so copies[0] is the most recent.
	sort.Slice(copies, func(i, j int) bool {
		return copies[i].InstallDate.After(copies[j].InstallDate)
	})

	now := time.Now()
	summaries := make([]snapshotSummary, 0, len(copies))
	for _, sc := range copies {
		age := int(now.Sub(sc.InstallDate).Hours() / 24)
		summaries = append(summaries, snapshotSummary{
			ID:        sc.ID,
			Volume:    sc.VolumeName,
			CreatedAt: sc.InstallDate,
			AgeDays:   age,
		})
	}

	mostRecentAge := summaries[0].AgeDays

	if mostRecentAge > snapshotWarnAgeDays {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("Most recent snapshot is %d day(s) old (threshold: %d days). %d total snapshot(s) found.", mostRecentAge, snapshotWarnAgeDays, len(copies)),
			Value:   summaries,
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%d snapshot(s) found. Most recent is %d day(s) old.", len(copies), mostRecentAge),
		Value:   summaries,
	}
}

// checkVSSService returns the Status based on the VSS service start type.
func checkVSSService() Status {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, vssServiceKey, registry.QUERY_VALUE)
	if err != nil {
		return StatusError
	}
	defer k.Close()

	startType, _, err := k.GetIntegerValue(serviceStart)
	if err != nil {
		return StatusError
	}

	switch startType {
	case startDisabled:
		return StatusCritical
	case startManual:
		return StatusWarning
	default:
		return StatusOK
	}
}