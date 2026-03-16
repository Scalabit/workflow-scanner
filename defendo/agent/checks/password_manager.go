//go:build windows

package checks

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// knownPasswordManagers lists recognised password managers with the subkey
// names they register under the Windows Uninstall registry paths, plus
// DisplayName fragments used as a fallback when the key name varies.
var knownPasswordManagers = []struct {
	Name         string
	UninstallKeys []string // exact subkey names to probe
	DisplayNames  []string // DisplayName substrings for fuzzy fallback
}{
	{
		Name:         "1Password",
		UninstallKeys: []string{"1Password", "1Password 7 - Password Manager"},
		DisplayNames:  []string{"1Password"},
	},
	{
		Name:         "Bitwarden",
		UninstallKeys: []string{"Bitwarden"},
		DisplayNames:  []string{"Bitwarden"},
	},
	{
		Name:         "LastPass",
		UninstallKeys: []string{"LastPass"},
		DisplayNames:  []string{"LastPass"},
	},
	{
		Name:         "Dashlane",
		UninstallKeys: []string{"Dashlane"},
		DisplayNames:  []string{"Dashlane"},
	},
	{
		Name:         "KeePass",
		UninstallKeys: []string{"KeePassPasswordSafe2_is1", "KeePass Password Safe 2"},
		DisplayNames:  []string{"KeePass"},
	},
	{
		Name:         "Keeper",
		UninstallKeys: []string{"Keeper Password Manager"},
		DisplayNames:  []string{"Keeper"},
	},
	{
		Name:         "NordPass",
		UninstallKeys: []string{"NordPass"},
		DisplayNames:  []string{"NordPass"},
	},
	{
		Name:         "RoboForm",
		UninstallKeys: []string{"RoboForm"},
		DisplayNames:  []string{"RoboForm"},
	},
	{
		Name:         "Enpass",
		UninstallKeys: []string{"Enpass"},
		DisplayNames:  []string{"Enpass"},
	},
	{
		Name:         "Sticky Password",
		UninstallKeys: []string{"Sticky Password"},
		DisplayNames:  []string{"Sticky Password"},
	},
}

type passwordManagerResult struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// PasswordManagerCheck verifies that at least one password manager is installed.
type PasswordManagerCheck struct{}

func (c PasswordManagerCheck) Name() string {
	return "Password Manager Installed"
}

func (c PasswordManagerCheck) Run() Result {
	found := detectPasswordManagers()

	if len(found) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: "No password manager detected. Using a password manager is strongly recommended to avoid password reuse and weak credentials.",
		}
	}

	names := make([]string, 0, len(found))
	for _, f := range found {
		names = append(names, f.Name)
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%d password manager(s) detected: %s", len(found), strings.Join(names, ", ")),
		Value:   found,
	}
}

// detectPasswordManagers searches the uninstall registry for known managers.
func detectPasswordManagers() []passwordManagerResult {
	// Build a map of DisplayName → DisplayVersion for all installed software
	// by enumerating the uninstall roots once.
	installed := enumerateInstalledSoftware()

	var results []passwordManagerResult
	seen := map[string]bool{}

	for _, pm := range knownPasswordManagers {
		if seen[pm.Name] {
			continue
		}

		// 1. Try exact subkey names first.
		if version, err := installedVersion(pm.UninstallKeys); err == nil {
			results = append(results, passwordManagerResult{Name: pm.Name, Version: version})
			seen[pm.Name] = true
			continue
		}

		// 2. Fuzzy match against enumerated DisplayNames.
		for displayName, version := range installed {
			for _, fragment := range pm.DisplayNames {
				if strings.Contains(strings.ToLower(displayName), strings.ToLower(fragment)) {
					results = append(results, passwordManagerResult{Name: pm.Name, Version: version})
					seen[pm.Name] = true
					break
				}
			}
			if seen[pm.Name] {
				break
			}
		}
	}

	return results
}

// enumerateInstalledSoftware returns a map of DisplayName → DisplayVersion
// by walking all uninstall registry roots.
func enumerateInstalledSoftware() map[string]string {
	result := make(map[string]string)

	for _, root := range uninstallRoots {
		k, err := registry.OpenKey(root.hive, root.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}

		subkeys, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}

		for _, sub := range subkeys {
			sk, err := registry.OpenKey(root.hive, root.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			displayName, _, _ := sk.GetStringValue("DisplayName")
			displayVersion, _, _ := sk.GetStringValue("DisplayVersion")
			sk.Close()

			if displayName != "" {
				result[displayName] = displayVersion
			}
		}
	}

	return result
}