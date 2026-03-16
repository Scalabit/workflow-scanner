//go:build windows

package checks

import "testing"

func TestVersionGTE(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "equal versions",
			a:    "134.0.6998.166",
			b:    "134.0.6998.166",
			want: true,
		},
		{
			name: "a greater than b by patch",
			a:    "134.0.6998.200",
			b:    "134.0.6998.166",
			want: true,
		},
		{
			name: "a less than b by patch",
			a:    "134.0.6998.100",
			b:    "134.0.6998.166",
			want: false,
		},
		{
			name: "a greater than b by major",
			a:    "135.0.0.0",
			b:    "134.0.6998.166",
			want: true,
		},
		{
			name: "a less than b by major",
			a:    "133.0.0.0",
			b:    "134.0.6998.166",
			want: false,
		},
		{
			name: "different component counts — shorter a equals longer b prefix",
			a:    "134.0",
			b:    "134.0.0.0",
			want: true,
		},
		{
			name: "different component counts — shorter b equals longer a prefix",
			a:    "134.0.0.0",
			b:    "134.0",
			want: true,
		},
		{
			name: "shorter a is greater via first component",
			a:    "135.0",
			b:    "134.0.6998.166",
			want: true,
		},
		{
			name: "shorter a is less via first component",
			a:    "133.0",
			b:    "134.0.6998.166",
			want: false,
		},
		{
			name: "minor version difference — a greater",
			a:    "134.1.0.0",
			b:    "134.0.9999.0",
			want: true,
		},
		{
			name: "minor version difference — a less",
			a:    "134.0.0.0",
			b:    "134.1.0.0",
			want: false,
		},
		{
			name: "empty strings — equal",
			a:    "",
			b:    "",
			want: true,
		},
		{
			name: "a empty b non-empty — a treated as 0",
			a:    "",
			b:    "1.0",
			want: false,
		},
		{
			name: "a non-empty b empty — a treated as greater",
			a:    "1.0",
			b:    "",
			want: true,
		},
		{
			name: "single component equal",
			a:    "134",
			b:    "134",
			want: true,
		},
		{
			name: "single component a greater",
			a:    "135",
			b:    "134",
			want: true,
		},
		{
			name: "single component a less",
			a:    "133",
			b:    "134",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionGTE(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("versionGTE(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
