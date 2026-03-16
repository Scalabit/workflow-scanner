//go:build windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"guardify-agent/checks"
)

// registry holds all checks to run.
// To add a new check: implement checks.Check and append it here.
var registry = []checks.Check{
	checks.PasswordMinLengthCheck{},
	checks.PasswordAgeCheck{},
	checks.StrongPasswordPolicyCheck{},
	checks.PasswordManagerCheck{},
	checks.AdminAccountsCheck{},
	checks.WindowsUpdateCheck{},
	checks.BitLockerCheck{},
	checks.GuestWiFiCheck{},
	checks.UserIsAdminCheck{},
	checks.VSSBackupCheck{},
	checks.ScreenLockCheck{},
	checks.PlaintextCredentialsCheck{},
	checks.BrowserVersionCheck{},
	checks.AntivirusCheck{},
	checks.CloudBackupCheck{},
	checks.EmailBreachCheck{},
	checks.ProcessesNoBinaryCheck{},
	checks.AMSICheck{},
	checks.SMBv1Check{},
	checks.WinRMSecurityCheck{},
	checks.PowerShellLoggingCheck{},
	checks.CommandLineAuditingCheck{},
	checks.RegistryPersistenceCheck{},
	checks.ComputerPasswordRotationCheck{},
	checks.SecureBootCheck{},
	checks.TPMCheck{},
	checks.RDPExposureCheck{},
	checks.LSAProtectionCheck{},
	checks.CrashReportingIntegrityCheck{},
	checks.PostMortemDebuggerCheck{},
	checks.UACCheck{},
	checks.LAPSCheck{},
	checks.WindowsHelloCheck{},
	checks.ChromeForcedExtensionsCheck{},
}

func main() {
	jsonOutput    := flag.Bool("json", false, "Output results as JSON")
	listChecks    := flag.Bool("list", false, "List all available checks and exit")
	onlyFlag      := flag.String("only", "", "Run only specific checks by index (comma-separated, e.g. --only 1,3,5). Use --list to see indices.")
	interval      := flag.Duration("interval", 0, "Run checks repeatedly at this frequency (e.g. 1h, 30m, 24h). Omit to run once.")
	pubsubProject := flag.String("pubsub-project", "", "Google Cloud project ID for Pub/Sub publishing")
	pubsubTopic   := flag.String("pubsub-topic", "", "Google Cloud Pub/Sub topic ID to publish results to")
	flag.Parse()

	if *listChecks {
		if *jsonOutput {
			type checkInfo struct {
				Index int    `json:"index"`
				Name  string `json:"name"`
			}
			items := make([]checkInfo, len(registry))
			for i, c := range registry {
				items[i] = checkInfo{Index: i + 1, Name: c.Name()}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(items)
		} else {
			fmt.Printf("Guardify Agent — Available Checks (%d total)\n", len(registry))
			fmt.Println("=============================================")
			for i, c := range registry {
				fmt.Printf("  %2d. %s\n", i+1, c.Name())
			}
		}
		return
	}

	active, err := selectChecks(*onlyFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Initialise Pub/Sub publisher if both flags are provided.
	var publisher *Publisher
	if *pubsubProject != "" && *pubsubTopic != "" {
		ctx := context.Background()
		publisher, err = NewPublisher(ctx, *pubsubProject, *pubsubTopic)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init Pub/Sub publisher: %v\n", err)
			os.Exit(1)
		}
		defer publisher.Close()
	} else if *pubsubProject != "" || *pubsubTopic != "" {
		fmt.Fprintln(os.Stderr, "both --pubsub-project and --pubsub-topic must be set together")
		os.Exit(1)
	}

	if *interval > 0 {
		runDaemon(active, *interval, *jsonOutput, publisher)
		return
	}

	runOnce(active, *jsonOutput, publisher)
}

// selectChecks returns the checks to run based on --only.
// An empty flag returns the full registry.
// Accepts comma-separated 1-based indices matching --list output.
func selectChecks(only string) ([]checks.Check, error) {
	if only == "" {
		return registry, nil
	}
	parts := strings.Split(only, ",")
	selected := make([]checks.Check, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx, err := strconv.Atoi(p)
		if err != nil || idx < 1 || idx > len(registry) {
			return nil, fmt.Errorf("invalid check index %q — use --list to see valid indices (1–%d)", p, len(registry))
		}
		selected = append(selected, registry[idx-1])
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("--only produced an empty check list")
	}
	return selected, nil
}

// runOnce executes the given checks a single time, prints results, and
// optionally publishes to Pub/Sub.
func runOnce(active []checks.Check, jsonOutput bool, pub *Publisher) {
	sysInfo := checks.GetSystemInfo()
	results := make([]checks.Result, 0, len(active))
	for _, c := range active {
		results = append(results, c.Run())
	}
	printResults(sysInfo, results, jsonOutput)

	if pub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := pub.Publish(ctx, sysInfo, results); err != nil {
			fmt.Fprintf(os.Stderr, "Pub/Sub publish error: %v\n", err)
		}
	}
}

// runDaemon runs the given checks on a fixed interval until SIGINT/SIGTERM.
func runDaemon(active []checks.Check, interval time.Duration, jsonOutput bool, pub *Publisher) {
	if !jsonOutput {
		fmt.Printf("Guardify Agent — running every %s (press Ctrl+C to stop)\n", interval)
		fmt.Println("==========================================================")
	}

	// Run immediately on start, then repeat.
	runOnce(active, jsonOutput, pub)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case t := <-ticker.C:
			if !jsonOutput {
				fmt.Printf("\n[%s] Running checks...\n", t.Format("2006-01-02 15:04:05"))
			}
			runOnce(active, jsonOutput, pub)
		case <-quit:
			if !jsonOutput {
				fmt.Println("\nShutting down.")
			}
			return
		}
	}
}

// printResults outputs results in human-readable or JSON format.
func printResults(sysInfo checks.SystemInfo, results []checks.Result, jsonOutput bool) {
	if jsonOutput {
		type envelope struct {
			Timestamp string           `json:"timestamp"`
			System    checks.SystemInfo `json:"system"`
			Results   []checks.Result  `json:"results"`
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(envelope{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			System:    sysInfo,
			Results:   results,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode JSON: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("\nGuardify Agent — Security Check Results (%s)\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("================================================")
	fmt.Printf("  Host : %s  |  User: %s\n", sysInfo.Hostname, sysInfo.Username)
	fmt.Printf("  OS   : %s (build %s)\n", sysInfo.OS, sysInfo.OSBuild)
	fmt.Printf("  CPU  : %s (%d cores / %d threads)\n", sysInfo.CPU, sysInfo.CPUCores, sysInfo.CPUThreads)
	fmt.Printf("  RAM  : %s\n", sysInfo.FormatRAM())
	fmt.Println("------------------------------------------------")
	for _, r := range results {
		fmt.Printf("[%-8s] %s: %s\n", r.Status, r.Name, r.Message)
	}
}