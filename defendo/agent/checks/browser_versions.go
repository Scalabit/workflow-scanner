//go:build windows

package checks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const latestVersionTimeout = 10 * time.Second

// uninstallRoots are the registry paths that hold installed software entries.
var uninstallRoots = []struct {
	hive registry.Key
	path string
}{
	{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
}

// browserDef describes how to find and check a browser.
type browserDef struct {
	Name        string
	// Subkey name(s) under the uninstall roots.
	UninstallKeys []string
	FetchLatest func() (string, error)
}

var browsers = []browserDef{
	{
		Name:          "Google Chrome",
		UninstallKeys: []string{"Google Chrome"},
		FetchLatest:   fetchChromeLatest,
	},
	{
		Name:          "Mozilla Firefox",
		UninstallKeys: []string{"Mozilla Firefox"},
		FetchLatest:   fetchFirefoxLatest,
	},
	{
		Name:          "Microsoft Edge",
		UninstallKeys: []string{"Microsoft Edge"},
		FetchLatest:   fetchEdgeLatest,
	},
	{
		Name:          "Brave",
		UninstallKeys: []string{"BraveSoftware Brave-Browser"},
		FetchLatest:   fetchBraveLatest,
	},
}

type browserResult struct {
	Browser   string `json:"browser"`
	Installed string `json:"installed"`
	Latest    string `json:"latest,omitempty"`
	UpToDate  *bool  `json:"up_to_date,omitempty"`
	Note      string `json:"note,omitempty"`
}

// BrowserVersionCheck verifies that installed browsers are up to date.
type BrowserVersionCheck struct{}

func (c BrowserVersionCheck) Name() string {
	return "Browser Versions Up To Date"
}

func (c BrowserVersionCheck) Run() Result {
	var results []browserResult
	outdated := 0
	found := 0

	for _, b := range browsers {
		installed, err := installedVersion(b.UninstallKeys)
		if err != nil {
			// Browser not installed — skip silently.
			continue
		}
		found++

		br := browserResult{Browser: b.Name, Installed: installed}

		latest, err := b.FetchLatest()
		if err != nil {
			br.Note = fmt.Sprintf("could not fetch latest version: %v", err)
			results = append(results, br)
			continue
		}
		br.Latest = latest

		upToDate := versionGTE(installed, latest)
		br.UpToDate = &upToDate
		if !upToDate {
			outdated++
		}

		results = append(results, br)
	}

	if found == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "No supported browsers found on this machine.",
		}
	}

	if outdated > 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d of %d installed browser(s) are out of date.", outdated, found),
			Value:   results,
		}
	}

	return Result{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("All %d installed browser(s) are up to date.", found),
		Value:   results,
	}
}

// installedVersion searches the uninstall registry roots for any of the given
// subkey names and returns the DisplayVersion value.
func installedVersion(subkeys []string) (string, error) {
	for _, root := range uninstallRoots {
		for _, subkey := range subkeys {
			k, err := registry.OpenKey(root.hive, root.path+`\`+subkey, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			v, _, err := k.GetStringValue("DisplayVersion")
			k.Close()
			if err == nil && v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("not found")
}

// --- Latest version fetchers ---

func fetchChromeLatest() (string, error) {
	// Google's version history API.
	url := "https://versionhistory.googleapis.com/v1/chrome/platforms/win64/channels/stable/versions?filter=endtime=none&orderBy=version+desc&pageSize=1"
	body, err := httpGet(url)
	if err != nil {
		return "", err
	}
	var resp struct {
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Versions) == 0 {
		return "", fmt.Errorf("unexpected Chrome API response")
	}
	return resp.Versions[0].Version, nil
}

func fetchFirefoxLatest() (string, error) {
	body, err := httpGet("https://product-details.mozilla.org/1.0/firefox_versions.json")
	if err != nil {
		return "", err
	}
	var resp struct {
		Latest string `json:"LATEST_FIREFOX_VERSION"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Latest == "" {
		return "", fmt.Errorf("unexpected Firefox API response")
	}
	return resp.Latest, nil
}

func fetchEdgeLatest() (string, error) {
	body, err := httpGet("https://edgeupdates.microsoft.com/api/products")
	if err != nil {
		return "", err
	}
	// Response is an array of product objects; find Stable channel, Windows x64.
	var products []struct {
		Product  string `json:"Product"`
		Releases []struct {
			Platform        string `json:"Platform"`
			Architecture    string `json:"Architecture"`
			ProductVersion  string `json:"ProductVersion"`
		} `json:"Releases"`
	}
	if err := json.Unmarshal(body, &products); err != nil {
		return "", fmt.Errorf("unexpected Edge API response")
	}
	for _, p := range products {
		if p.Product != "Stable" {
			continue
		}
		for _, r := range p.Releases {
			if r.Platform == "Windows" && r.Architecture == "x64" {
				return r.ProductVersion, nil
			}
		}
	}
	return "", fmt.Errorf("edge stable Windows x64 release not found in API response")
}

func fetchBraveLatest() (string, error) {
	body, err := httpGet("https://api.github.com/repos/brave/brave-browser/releases/latest")
	if err != nil {
		return "", err
	}
	var resp struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.TagName == "" {
		return "", fmt.Errorf("unexpected Brave API response")
	}
	// Tag name is like "v1.75.182" — strip the leading "v".
	return strings.TrimPrefix(resp.TagName, "v"), nil
}

// --- Helpers ---

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: latestVersionTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// versionGTE returns true if version a is >= b.
// Compares dot-separated numeric components (e.g. "134.0.6998.166").
func versionGTE(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	max := len(aParts)
	if len(bParts) > max {
		max = len(bParts)
	}

	for i := 0; i < max; i++ {
		av, bv := 0, 0
		if i < len(aParts) {
			av, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(bParts[i])
		}
		if av != bv {
			return av > bv
		}
	}
	return true // equal
}