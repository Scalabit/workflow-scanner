//go:build windows

package checks

import (
	"strings"
	"testing"
)

// --- sharesSubnet ---

func TestSharesSubnet(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: false,
		},
		{
			name: "a empty",
			a:    []string{},
			b:    []string{"192.168.1.0/24"},
			want: false,
		},
		{
			name: "b empty",
			a:    []string{"192.168.1.0/24"},
			b:    []string{},
			want: false,
		},
		{
			name: "no overlap",
			a:    []string{"192.168.1.0/24", "10.0.0.0/8"},
			b:    []string{"172.16.0.0/12", "192.168.2.0/24"},
			want: false,
		},
		{
			name: "one overlap",
			a:    []string{"192.168.1.0/24", "10.0.0.0/8"},
			b:    []string{"172.16.0.0/12", "192.168.1.0/24"},
			want: true,
		},
		{
			name: "multiple overlaps",
			a:    []string{"192.168.1.0/24", "10.0.0.0/8"},
			b:    []string{"10.0.0.0/8", "192.168.1.0/24"},
			want: true,
		},
		{
			name: "overlap on first element",
			a:    []string{"10.0.0.0/8"},
			b:    []string{"10.0.0.0/8", "192.168.1.0/24"},
			want: true,
		},
		{
			name: "single element each no overlap",
			a:    []string{"192.168.1.0/24"},
			b:    []string{"192.168.2.0/24"},
			want: false,
		},
		{
			name: "single element each with overlap",
			a:    []string{"192.168.1.0/24"},
			b:    []string{"192.168.1.0/24"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sharesSubnet(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("sharesSubnet(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// --- toCIDRList ---

func TestToCIDRList(t *testing.T) {
	tests := []struct {
		name    string
		ips     []string
		masks   []string
		want    []string
	}{
		{
			name:  "both empty",
			ips:   []string{},
			masks: []string{},
			want:  []string{},
		},
		{
			name:  "single valid pair — class C",
			ips:   []string{"192.168.1.42"},
			masks: []string{"255.255.255.0"},
			want:  []string{"192.168.1.0/24"},
		},
		{
			name:  "single valid pair — class A",
			ips:   []string{"10.5.6.7"},
			masks: []string{"255.0.0.0"},
			want:  []string{"10.0.0.0/8"},
		},
		{
			name:  "multiple valid pairs",
			ips:   []string{"192.168.1.42", "10.5.6.7"},
			masks: []string{"255.255.255.0", "255.0.0.0"},
			want:  []string{"192.168.1.0/24", "10.0.0.0/8"},
		},
		{
			name:  "more ips than masks — extra IPs ignored",
			ips:   []string{"192.168.1.1", "10.0.0.1"},
			masks: []string{"255.255.255.0"},
			want:  []string{"192.168.1.0/24"},
		},
		{
			name:  "more masks than ips — extra masks ignored",
			ips:   []string{"192.168.1.1"},
			masks: []string{"255.255.255.0", "255.0.0.0"},
			want:  []string{"192.168.1.0/24"},
		},
		{
			name:  "invalid IP skipped",
			ips:   []string{"not-an-ip", "192.168.1.1"},
			masks: []string{"255.255.255.0", "255.255.255.0"},
			want:  []string{"192.168.1.0/24"},
		},
		{
			name:  "invalid mask skipped",
			ips:   []string{"192.168.1.1", "10.0.0.1"},
			masks: []string{"not-a-mask", "255.0.0.0"},
			want:  []string{"10.0.0.0/8"},
		},
		{
			name:  "both invalid skipped",
			ips:   []string{"not-an-ip"},
			masks: []string{"not-a-mask"},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toCIDRList(tt.ips, tt.masks)

			// Normalise nil vs empty slice for comparison.
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("toCIDRList(%v, %v) = %v (len %d), want %v (len %d)",
					tt.ips, tt.masks, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("toCIDRList[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- guidToString ---

func TestGuidToString(t *testing.T) {
	tests := []struct {
		name string
		g    [16]byte
		want string
	}{
		{
			name: "all zeros",
			g:    [16]byte{},
			want: "{00000000-0000-0000-0000-000000000000}",
		},
		{
			// A known GUID in little-endian wire format.
			// GUID: {6BA7B810-9DAD-11D1-80B4-00C04FD430C8}  (RFC 4122 namespace DNS)
			// Wire bytes (little-endian for first three components):
			//   Data1 (LE): 10 B8 A7 6B  → 0x6BA7B810
			//   Data2 (LE): AD 9D        → 0x9DAD
			//   Data3 (LE): D1 11        → 0x11D1
			//   Data4 (BE): 80 B4 00 C0 4F D4 30 C8
			name: "RFC 4122 namespace DNS GUID",
			g: [16]byte{
				0x10, 0xB8, 0xA7, 0x6B, // Data1 LE → 0x6BA7B810
				0xAD, 0x9D,             // Data2 LE → 0x9DAD
				0xD1, 0x11,             // Data3 LE → 0x11D1
				0x80, 0xB4,             // Data4[0:2]
				0x00, 0xC0, 0x4F, 0xD4, 0x30, 0xC8, // Data4[2:8]
			},
			want: "{6BA7B810-9DAD-11D1-80B4-00C04FD430C8}",
		},
		{
			// Simple hand-crafted GUID to verify all nibble positions.
			// bytes: 01 02 03 04 | 05 06 | 07 08 | 09 0A | 0B 0C 0D 0E 0F 10
			// Data1 LE: 04030201
			// Data2 LE: 0605
			// Data3 LE: 0807
			// Data4[0:2]: 09 0A  → "090A"
			// Data4[2:8]: 0B 0C 0D 0E 0F 10
			name: "sequential bytes",
			g: [16]byte{
				0x01, 0x02, 0x03, 0x04,
				0x05, 0x06,
				0x07, 0x08,
				0x09, 0x0A,
				0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
			},
			want: "{04030201-0605-0807-090A-0B0C0D0E0F10}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := guidToString(tt.g)
			if got != tt.want {
				t.Errorf("guidToString(...) = %q, want %q", got, tt.want)
			}
			// Structural sanity: must match {8-4-4-4-12} format.
			if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
				t.Errorf("missing braces: %q", got)
			}
			inner := got[1 : len(got)-1]
			parts := strings.Split(inner, "-")
			if len(parts) != 5 {
				t.Errorf("expected 5 dash-separated parts, got %d: %q", len(parts), got)
			}
			expectedLengths := []int{8, 4, 4, 4, 12}
			for i, part := range parts {
				if len(part) != expectedLengths[i] {
					t.Errorf("part[%d] len = %d, want %d: %q", i, len(part), expectedLengths[i], part)
				}
			}
		})
	}
}
