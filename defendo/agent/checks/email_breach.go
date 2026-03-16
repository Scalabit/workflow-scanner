//go:build windows

package checks

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	hibpBaseURL = "https://haveibeenpwned.com/api/v3/breachedaccount"
	hibpTimeout = 15 * time.Second
)

type hibpBreach struct {
	Name        string   `json:"Name"`
	Domain      string   `json:"Domain"`
	BreachDate  string   `json:"BreachDate"`
	PwnCount    int      `json:"PwnCount"`
	DataClasses []string `json:"DataClasses"`
	IsVerified  bool     `json:"IsVerified"`
}

type breachSummary struct {
	Name        string   `json:"name"`
	Domain      string   `json:"domain"`
	BreachDate  string   `json:"breach_date"`
	PwnCount    int      `json:"pwn_count"`
	DataClasses []string `json:"data_classes"`
	Verified    bool     `json:"verified"`
}

// EmailBreachCheck looks up the user's email against the Have I Been Pwned
// database to detect known data breaches.
type EmailBreachCheck struct{}

func (c EmailBreachCheck) Name() string {
	return "Email Compromised (HIBP)"
}

func (c EmailBreachCheck) Run() Result {
	apiKey := os.Getenv("HIBP_API_KEY")
	if apiKey == "" {
		return Result{
			Name:   c.Name(),
			Status: StatusError,
			Message: "HIBP_API_KEY environment variable is not set. " +
				"Get a key at https://haveibeenpwned.com/API/Key",
		}
	}

	email, source := resolveEmail()
	if email == "" {
		return Result{
			Name:   c.Name(),
			Status: StatusError,
			Message: "Could not determine user email. Set GUARDIFY_USER_EMAIL " +
				"environment variable to override.",
		}
	}

	breaches, err := queryHIBP(email, apiKey)
	if err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("HIBP API error for %q: %v", email, err),
		}
	}

	if len(breaches) == 0 {
		return Result{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("Email %q (%s) has not appeared in any known data breaches.", email, source),
			Value:   map[string]interface{}{"email": email, "source": source},
		}
	}

	// Summarise breaches; count how many exposed passwords.
	summaries := make([]breachSummary, 0, len(breaches))
	passwordsExposed := 0
	for _, b := range breaches {
		summaries = append(summaries, breachSummary{
			Name:        b.Name,
			Domain:      b.Domain,
			BreachDate:  b.BreachDate,
			PwnCount:    b.PwnCount,
			DataClasses: b.DataClasses,
			Verified:    b.IsVerified,
		})
		for _, dc := range b.DataClasses {
			if strings.EqualFold(dc, "Passwords") {
				passwordsExposed++
				break
			}
		}
	}

	status := StatusWarning
	msg := fmt.Sprintf("Email %q found in %d breach(es).", email, len(breaches))
	if passwordsExposed > 0 {
		status = StatusCritical
		msg = fmt.Sprintf("Email %q found in %d breach(es), %d of which exposed passwords. Immediate password change recommended.", email, len(breaches), passwordsExposed)
	}

	return Result{
		Name:    c.Name(),
		Status:  status,
		Message: msg,
		Value: map[string]interface{}{
			"email":             email,
			"source":            source,
			"breach_count":      len(breaches),
			"passwords_exposed": passwordsExposed,
			"breaches":          summaries,
		},
	}
}

// resolveEmail returns the user's email and the source it was found in.
// Priority: GUARDIFY_USER_EMAIL env var → OneDrive accounts → Azure AD.
func resolveEmail() (email, source string) {
	// 1. Environment variable override.
	if e := os.Getenv("GUARDIFY_USER_EMAIL"); e != "" {
		return e, "GUARDIFY_USER_EMAIL env var"
	}

	// 2. OneDrive personal and business accounts.
	oneDrivePaths := []struct {
		path   string
		source string
	}{
		{`SOFTWARE\Microsoft\OneDrive\Accounts\Personal`, "OneDrive Personal"},
		{`SOFTWARE\Microsoft\OneDrive\Accounts\Business1`, "OneDrive Business"},
		{`SOFTWARE\Microsoft\OneDrive\Accounts\Business2`, "OneDrive Business"},
	}
	for _, p := range oneDrivePaths {
		k, err := registry.OpenKey(registry.CURRENT_USER, p.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		e, _, err := k.GetStringValue("UserEmail")
		k.Close()
		if err == nil && e != "" {
			return e, p.source
		}
	}

	// 3. Azure AD joined machine — JoinInfo stores the enrolling user's UPN.
	joinInfoRoot := `SYSTEM\CurrentControlSet\Control\CloudDomainJoin\JoinInfo`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, joinInfoRoot, registry.ENUMERATE_SUB_KEYS)
	if err == nil {
		subkeys, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, sub := range subkeys {
			path := joinInfoRoot + `\` + sub
			sk, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			e, _, err := sk.GetStringValue("UserEmail")
			sk.Close()
			if err == nil && e != "" {
				return e, "Azure AD Join"
			}
		}
	}

	return "", ""
}

// queryHIBP calls the HIBP v3 breachedaccount endpoint.
// Returns an empty slice if the email is clean (HTTP 404).
func queryHIBP(email, apiKey string) ([]hibpBreach, error) {
	endpoint := fmt.Sprintf("%s/%s?truncateResponse=false", hibpBaseURL, url.PathEscape(email))

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("hibp-api-key", apiKey)
	req.Header.Set("User-Agent", "guardify-agent")

	client := &http.Client{Timeout: hibpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}
		var breaches []hibpBreach
		if err := json.Unmarshal(body, &breaches); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}
		return breaches, nil
	case http.StatusNotFound:
		return nil, nil // clean — no breaches
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("invalid HIBP API key")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("HIBP rate limit exceeded — try again later")
	default:
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
}