//go:build windows

package checks

import "testing"

func TestKnownPasswordManagersSliceIsNonEmpty(t *testing.T) {
	if len(knownPasswordManagers) == 0 {
		t.Fatal("knownPasswordManagers must not be empty")
	}
}

func TestKnownPasswordManagerEntriesAreValid(t *testing.T) {
	for i, pm := range knownPasswordManagers {
		t.Run(pm.Name, func(t *testing.T) {
			if pm.Name == "" {
				t.Errorf("entry[%d]: Name must not be empty", i)
			}

			hasKey := len(pm.UninstallKeys) > 0
			hasDisplayName := len(pm.DisplayNames) > 0

			if !hasKey && !hasDisplayName {
				t.Errorf("entry[%d] (%q): must have at least one UninstallKey or DisplayName", i, pm.Name)
			}

			for j, k := range pm.UninstallKeys {
				if k == "" {
					t.Errorf("entry[%d] (%q): UninstallKeys[%d] must not be empty string", i, pm.Name, j)
				}
			}
			for j, d := range pm.DisplayNames {
				if d == "" {
					t.Errorf("entry[%d] (%q): DisplayNames[%d] must not be empty string", i, pm.Name, j)
				}
			}
		})
	}
}

func TestKnownPasswordManagerNamesAreUnique(t *testing.T) {
	seen := make(map[string]int)
	for i, pm := range knownPasswordManagers {
		if prev, ok := seen[pm.Name]; ok {
			t.Errorf("duplicate name %q at indices %d and %d", pm.Name, prev, i)
		}
		seen[pm.Name] = i
	}
}
