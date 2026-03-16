//go:build windows

package checks

import (
	"strings"
	"testing"
)

// --- decodeKFMFolders ---

func TestDecodeKFMFolders(t *testing.T) {
	tests := []struct {
		name string
		mask uint64
		want []string
	}{
		{
			name: "mask 0 — no folders",
			mask: 0,
			want: nil,
		},
		{
			name: "mask 1 — Desktop only",
			mask: 1,
			want: []string{"Desktop"},
		},
		{
			name: "mask 2 — Documents only",
			mask: 2,
			want: []string{"Documents"},
		},
		{
			name: "mask 4 — Pictures only",
			mask: 4,
			want: []string{"Pictures"},
		},
		{
			name: "mask 3 — Desktop + Documents",
			mask: 3,
			want: []string{"Desktop", "Documents"},
		},
		{
			name: "mask 5 — Desktop + Pictures",
			mask: 5,
			want: []string{"Desktop", "Pictures"},
		},
		{
			name: "mask 6 — Documents + Pictures",
			mask: 6,
			want: []string{"Documents", "Pictures"},
		},
		{
			name: "mask 7 — all three folders",
			mask: 7,
			want: []string{"Desktop", "Documents", "Pictures"},
		},
		{
			name: "mask with extra high bits — only low three bits matter",
			mask: 0xFF, // bits 0-2 all set, rest ignored
			want: []string{"Desktop", "Documents", "Pictures"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeKFMFolders(tt.mask)

			if len(got) != len(tt.want) {
				t.Errorf("decodeKFMFolders(%d) = %v (len %d), want %v (len %d)",
					tt.mask, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("decodeKFMFolders(%d)[%d] = %q, want %q", tt.mask, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- providerNames ---

func TestProviderNames(t *testing.T) {
	tests := []struct {
		name    string
		results []backupProviderResult
		want    string
	}{
		{
			name:    "empty slice",
			results: []backupProviderResult{},
			want:    "",
		},
		{
			name: "single provider",
			results: []backupProviderResult{
				{Provider: "OneDrive", Active: true, Detail: "signed in"},
			},
			want: "OneDrive",
		},
		{
			name: "multiple providers",
			results: []backupProviderResult{
				{Provider: "OneDrive", Active: true, Detail: "signed in"},
				{Provider: "Backblaze", Active: true, Detail: "service running"},
				{Provider: "Acronis", Active: false, Detail: "service stopped"},
			},
			want: "OneDrive, Backblaze, Acronis",
		},
		{
			name: "two providers",
			results: []backupProviderResult{
				{Provider: "Veeam", Active: true},
				{Provider: "CrashPlan", Active: false},
			},
			want: "Veeam, CrashPlan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerNames(tt.results)
			if got != tt.want {
				t.Errorf("providerNames(...) = %q, want %q", got, tt.want)
			}

			// Cross-check: comma-separated count matches result count (for non-empty).
			if len(tt.results) > 0 {
				parts := strings.Split(got, ", ")
				if len(parts) != len(tt.results) {
					t.Errorf("expected %d comma-separated names, got %d: %q",
						len(tt.results), len(parts), got)
				}
			}
		})
	}
}
