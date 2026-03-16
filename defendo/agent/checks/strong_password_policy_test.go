//go:build windows

package checks

import "testing"

func TestStrongPasswordPolicyConstants(t *testing.T) {
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"strongMinLength", strongMinLength, 8},
		{"strongMaxAgeDays", strongMaxAgeDays, 90},
		{"strongMinAgeDays", strongMinAgeDays, 1},
		{"strongMinHistory", strongMinHistory, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestTimeqForeverConstant(t *testing.T) {
	// timeqForever is the Windows sentinel TIMEQ_FOREVER value (0xFFFFFFFF).
	const expected uint32 = 0xFFFFFFFF
	if timeqForever != expected {
		t.Errorf("timeqForever = 0x%X, want 0x%X", timeqForever, expected)
	}
}
