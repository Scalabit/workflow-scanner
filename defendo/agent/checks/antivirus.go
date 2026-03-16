//go:build windows

package checks

import (
	"fmt"

	"github.com/yusufpapurcu/wmi"
)

// antiVirusProduct maps to the AntiVirusProduct WMI class.
// Namespace: root\SecurityCenter2
// https://learn.microsoft.com/en-us/windows/win32/wmisdk/wmi-classes
type antiVirusProduct struct {
	DisplayName              string
	ProductState             uint32
	PathToSignedProductExe   string
}

// productState encodes three pieces of information in a single DWORD.
// The two most significant used bytes break down as:
//
//	Byte 2 (bits 16-23): scanner/real-time protection state
//	  0x10 = enabled   0x00 = disabled   0x01 = snoozed   0x11 = expired
//	Byte 1 (bits 8-15): definition update state
//	  0x10 = up to date   0x00 = out of date
func decodeProductState(state uint32) (enabled bool, defsUpToDate bool) {
	scannerByte := (state >> 16) & 0xFF
	defsByte := (state >> 8) & 0xFF
	enabled = scannerByte == 0x10
	defsUpToDate = defsByte == 0x10
	return
}

type avResult struct {
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	DefsUpToDate bool   `json:"definitions_up_to_date"`
	ProductState uint32 `json:"product_state"`
}

// AntivirusCheck verifies that at least one active antivirus is registered
// with the Windows Security Center.
type AntivirusCheck struct{}

func (c AntivirusCheck) Name() string {
	return "Antivirus Installed"
}

func (c AntivirusCheck) Run() Result {
	var products []antiVirusProduct

	q := wmi.CreateQuery(&products, "")
	if err := wmi.QueryNamespace(q, &products, `root\SecurityCenter2`); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("WMI SecurityCenter2 query failed: %v", err),
		}
	}

	if len(products) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: "No antivirus product is registered with Windows Security Center.",
		}
	}

	results := make([]avResult, 0, len(products))
	activeCount := 0
	defsOldCount := 0

	for _, p := range products {
		enabled, defsUpToDate := decodeProductState(p.ProductState)
		results = append(results, avResult{
			Name:         p.DisplayName,
			Enabled:      enabled,
			DefsUpToDate: defsUpToDate,
			ProductState: p.ProductState,
		})
		if enabled {
			activeCount++
		}
		if !defsUpToDate {
			defsOldCount++
		}
	}

	// Build a human-readable summary of installed products.
	names := make([]string, 0, len(products))
	for _, r := range results {
		names = append(names, r.Name)
	}

	if activeCount == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusCritical,
			Message: fmt.Sprintf("%d antivirus product(s) found but none are active: %v", len(products), names),
			Value:   results,
		}
	}

	if defsOldCount > 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d antivirus product(s) active, but %d have outdated definitions: %v", activeCount, defsOldCount, names),
			Value:   results,
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%d antivirus product(s) active with up-to-date definitions: %v", activeCount, names),
		Value:   results,
	}
}