package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"text/tabwriter"

	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/profile"
	"github.com/pecodigos/picord/internal/rpc"
)

func runCLI(args []string, debug bool) int {
	if len(args) == 0 {
		return runDaemon(debug)
	}

	switch args[0] {
	case "run":
		return runDaemon(debug)
	case "status":
		return cmdStatus()
	case "profiles":
		return cmdProfiles(args[1:])
	case "override":
		return cmdOverride(args[1:])
	case "clear":
		return cmdClear()
	case "reload":
		return cmdReload()
	case "catalog":
		return cmdCatalog(args[1:])
	case "debug-rpc-image":
		return cmdDebugRPCImage(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Print(`Usage: picord <command> [options]

Commands:
  run              Run the daemon (default if no command given)
  status           Show current presence status
  profiles         List all profiles or create from catalog
  override         Set a manual override
  clear            Clear manual override
  reload           Reload configuration from disk
  catalog          Catalog management (status, search, refresh)
  help             Show this help message

Override options:
  -n, --name       Profile name
  -d, --details    Activity details
  -s, --state      Activity state
  -i, --image      Large image key

Catalog commands:
  picord catalog status
  picord catalog search <query>
  picord catalog refresh --source <source> [--max-pages N]

Profile commands:
  picord profile from-catalog <entry-id>
`)
}

func getAPIBase() string {
	configDir := configDirPath()
	configPath := filepath.Join(configDir, "picord", "config.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return "http://127.0.0.1:17970"
	}
	return fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort)
}

func apiGet(path string) (*http.Response, error) {
	return http.Get(getAPIBase() + path)
}

func apiPost(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return http.Post(getAPIBase()+path, "application/json", bytes.NewReader(data))
}

func apiDelete(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodDelete, getAPIBase()+path, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func printResponse(resp *http.Response) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Println(string(body))
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n%s\n", resp.Status, string(body))
	}
}

func cmdStatus() int {
	resp, err := apiGet("/api/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		return 1
	}

	active := result["active_name"].(string)
	if active == "" {
		active = "(none)"
	}
	proc := result["active_process"].(string)
	if proc == "" {
		proc = "(none)"
	}
	auto := result["auto_detect"].(bool)
	override := result["has_override"].(bool)

	fmt.Printf("Active Profile: %s\n", active)
	fmt.Printf("Active Process: %s\n", proc)
	fmt.Printf("Auto-Detect:    %v\n", auto)
	fmt.Printf("Has Override:   %v\n", override)

	if procs, ok := result["detected_processes"].([]any); ok && len(procs) > 0 {
		fmt.Printf("\nDetected Processes:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "PID\tNAME\tWINDOW TITLE")
		for _, p := range procs {
			if m, ok := p.(map[string]any); ok {
			pid, _ := m["pid"].(float64)
			if pid == 0 {
				pid, _ = m["PID"].(float64)
			}
			name, _ := m["name"].(string)
			if name == "" {
				name, _ = m["Name"].(string)
			}
			title, _ := m["window_title"].(string)
				if title == "" {
					title = "-"
				}
				fmt.Fprintf(w, "%.0f\t%s\t%s\n", pid, name, title)
			}
		}
		w.Flush()
	}

	return 0
}

func cmdOverride(args []string) int {
	fs := flag.NewFlagSet("override", flag.ExitOnError)
	name := fs.String("n", "", "Profile name")
	details := fs.String("d", "", "Activity details")
	state := fs.String("s", "", "Activity state")
	image := fs.String("i", "", "Large image key")
	fs.Parse(args)

	// Also support --long-form flags manually
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 < len(args) { *name = args[i+1]; i++ }
		case "--details":
			if i+1 < len(args) { *details = args[i+1]; i++ }
		case "--state":
			if i+1 < len(args) { *state = args[i+1]; i++ }
		case "--image":
			if i+1 < len(args) { *image = args[i+1]; i++ }
		}
	}

	if *name == "" {
		fmt.Fprintln(os.Stderr, "Error: name is required")
		return 1
	}

	p := profile.Profile{
		Name: *name,
		Activity: profile.Activity{
			Details:    *details,
			State:      *state,
			LargeImage: *image,
		},
	}

	resp, err := apiPost("/api/override", p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	printResponse(resp)
	return 0
}

func cmdClear() int {
	resp, err := apiDelete("/api/override")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	printResponse(resp)
	return 0
}

func cmdReload() int {
	resp, err := apiPost("/api/reload", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	printResponse(resp)
	return 0
}

func cmdDebugRPCImage(args []string) int {
	fs := flag.NewFlagSet("debug-rpc-image", flag.ExitOnError)
	assetKey := fs.String("asset-key", "", "Discord asset key to test")
	externalURL := fs.String("external-url", "", "External image URL to test")
	appID := fs.String("app-id", "", "Discord application client ID")
	fs.Parse(args)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--asset-key":
			if i+1 < len(args) { *assetKey = args[i+1]; i++ }
		case "--external-url":
			if i+1 < len(args) { *externalURL = args[i+1]; i++ }
		case "--app-id":
			if i+1 < len(args) { *appID = args[i+1]; i++ }
		}
	}

	if *appID == "" {
		fmt.Fprintln(os.Stderr, "Error: --app-id is required")
		return 1
	}

	client, err := rpc.NewClient(*appID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to Discord: %v\n", err)
		return 1
	}
	defer client.Close()

	act := &rpc.RichActivity{
		Details:  "Picord Image Test",
		State:    "Testing image display",
		Instance: false,
	}

	if *externalURL != "" {
		act.Assets = &rpc.RichAssets{
			LargeImage: *externalURL,
			LargeText:  "External URL Test",
		}
		fmt.Printf("Testing external URL: %s\n", *externalURL)
	} else if *assetKey != "" {
		act.Assets = &rpc.RichAssets{
			LargeImage: *assetKey,
			LargeText:  "Asset Key Test",
		}
		fmt.Printf("Testing asset key: %s\n", *assetKey)
	} else {
		fmt.Fprintln(os.Stderr, "Error: either --asset-key or --external-url is required")
		return 1
	}

	fmt.Printf("Payload: %+v\n", act)
	if err := client.SetActivity(act); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting activity: %v\n", err)
		return 1
	}
	fmt.Println("Activity set. Check Discord. Press Ctrl+C to clear and exit.")

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	client.ClearActivity()
	fmt.Println("Activity cleared.")
	return 0
}

func cmdCatalog(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: catalog subcommand required")
		fmt.Fprintln(os.Stderr, "Usage: picord catalog status|search|refresh")
		return 1
	}
	switch args[0] {
	case "status":
		return cmdCatalogStatus()
	case "search":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Error: search query required")
			return 1
		}
		return cmdCatalogSearch(args[1])
	case "refresh":
		return cmdCatalogRefresh(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown catalog subcommand: %s\n", args[0])
		return 1
	}
}

func cmdCatalogStatus() int {
	resp, err := apiGet("/api/catalog/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
	return 0
}

func cmdCatalogSearch(query string) int {
	resp, err := apiGet("/api/catalog/search?q=" + query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
	return 0
}

func cmdCatalogRefresh(args []string) int {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	source := fs.String("source", "", "Source to refresh (steam_local, lutris_public, desktop)")
	maxPages := fs.Int("max-pages", 0, "Max pages to fetch (for paginated sources)")
	fs.Parse(args)

	// Manual long flag support
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--source":
			if i+1 < len(args) { *source = args[i+1]; i++ }
		case "--max-pages":
			if i+1 < len(args) {
				if v, err := fmt.Sscanf(args[i+1], "%d", maxPages); v == 1 && err == nil {
					i++
				}
			}
		}
	}

	if *source == "" {
		fmt.Fprintln(os.Stderr, "Error: --source is required")
		return 1
	}

	reqBody := map[string]any{"source": *source}
	if *maxPages > 0 {
		reqBody["max_pages"] = *maxPages
	}
	resp, err := apiPost("/api/catalog/refresh", reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	printResponse(resp)
	return 0
}

func cmdProfiles(args []string) int {
	if len(args) >= 2 && args[0] == "from-catalog" {
		return cmdProfileFromCatalog(args[1])
	}

	resp, err := apiGet("/api/profiles")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	var profiles []profile.Profile
	if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		return 1
	}

	if len(profiles) == 0 {
		fmt.Println("No user profiles configured.")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tMATCH\tPRIORITY\tENABLED")
	for _, p := range profiles {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%v\n", p.Name, p.Match.Type, p.Match.Value, p.Priority, p.Enabled)
	}
	w.Flush()
	return 0
}

func cmdProfileFromCatalog(entryID string) int {
	resp, err := apiPost("/api/catalog/profiles/from-entry/"+entryID, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	printResponse(resp)
	return 0
}
