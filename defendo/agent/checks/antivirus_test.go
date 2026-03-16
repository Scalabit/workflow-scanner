//go:build windows

package checks

import "testing"

// decodeProductState formula (from antivirus.go):
//   scannerByte := (state >> 16) & 0xFF   — bits 16-23
//   defsByte    := (state >>  8) & 0xFF   — bits  8-15
//   enabled      = scannerByte == 0x10
//   defsUpToDate = defsByte    == 0x10

func TestDecodeProductState(t *testing.T) {
	tests := []struct {
		name         string
		state        uint32
		wantEnabled  bool
		wantUpToDate bool
	}{
		{
			name:         "all zeros — disabled, out of date",
			state:        0x00000000,
			wantEnabled:  false,
			wantUpToDate: false,
		},
		{
			name: "scanner enabled (0x10 in byte 2), defs out of date (0x00 in byte 1)",
			// scannerByte = (0x00100000 >> 16) & 0xFF = 0x10 ✓
			// defsByte    = (0x00100000 >>  8) & 0xFF = 0x00 ✗
			state:        0x00100000,
			wantEnabled:  true,
			wantUpToDate: false,
		},
		{
			name: "scanner enabled, defs up to date",
			// scannerByte = (0x00101000 >> 16) & 0xFF = 0x10 ✓
			// defsByte    = (0x00101000 >>  8) & 0xFF = 0x10 ✓
			state:        0x00101000,
			wantEnabled:  true,
			wantUpToDate: true,
		},
		{
			name: "scanner disabled (0x00), defs up to date (0x10)",
			// scannerByte = (0x00001000 >> 16) & 0xFF = 0x00 ✗
			// defsByte    = (0x00001000 >>  8) & 0xFF = 0x10 ✓
			state:        0x00001000,
			wantEnabled:  false,
			wantUpToDate: true,
		},
		{
			name: "scanner snoozed (0x01) — not enabled",
			// scannerByte = (0x00010000 >> 16) & 0xFF = 0x01 ✗
			// defsByte    = (0x00010000 >>  8) & 0xFF = 0x00 ✗
			state:        0x00010000,
			wantEnabled:  false,
			wantUpToDate: false,
		},
		{
			name: "scanner expired (0x11) — not enabled",
			// scannerByte = (0x00110000 >> 16) & 0xFF = 0x11 ✗
			// defsByte    = (0x00110000 >>  8) & 0xFF = 0x00 ✗
			state:        0x00110000,
			wantEnabled:  false,
			wantUpToDate: false,
		},
		{
			name: "Windows Defender value 397568 (0x061100)",
			// 397568 = 0x061100
			// scannerByte = (0x061100 >> 16) & 0xFF = 0x06 — not 0x10, so disabled
			// defsByte    = (0x061100 >>  8) & 0xFF = 0x11 — not 0x10, so out of date
			state:        397568,
			wantEnabled:  false,
			wantUpToDate: false,
		},
		{
			name: "Windows Defender active value 266240 (0x041000)",
			// 266240 = 0x041000
			// scannerByte = (0x041000 >> 16) & 0xFF = 0x04 — not 0x10
			// defsByte    = (0x041000 >>  8) & 0xFF = 0x10 ✓
			state:        266240,
			wantEnabled:  false,
			wantUpToDate: true,
		},
		{
			name: "typical third-party AV enabled+uptodate: 0x001010",
			// This is a small value — check the bytes carefully:
			// scannerByte = (0x001010 >> 16) & 0xFF = 0x00 ✗
			// defsByte    = (0x001010 >>  8) & 0xFF = 0x10 ✓
			// Note: 0x001010 = 4112; scanner disabled, defs up to date
			state:        0x001010,
			wantEnabled:  false,
			wantUpToDate: true,
		},
		{
			name: "both bytes set to 0x10 via explicit bit layout",
			// Set scannerByte=0x10 → bits 16-23: 0x10 << 16 = 0x00100000
			// Set defsByte=0x10    → bits  8-15: 0x10 <<  8 = 0x00001000
			// Combined: 0x00101000
			state:        0x00101000,
			wantEnabled:  true,
			wantUpToDate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, defsUpToDate := decodeProductState(tt.state)
			if enabled != tt.wantEnabled {
				t.Errorf("decodeProductState(0x%08X): enabled = %v, want %v", tt.state, enabled, tt.wantEnabled)
			}
			if defsUpToDate != tt.wantUpToDate {
				t.Errorf("decodeProductState(0x%08X): defsUpToDate = %v, want %v", tt.state, defsUpToDate, tt.wantUpToDate)
			}
		})
	}
}
