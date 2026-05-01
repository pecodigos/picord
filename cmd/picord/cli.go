package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/pecodigos/picord/internal/catalog"
	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/monitor"
	"github.com/pecodigos/picord/internal/profile"
	"github.com/pecodigos/picord/internal/rpc"
	"github.com/pecodigos/picord/internal/server"
)

func runCLI(args []string, debug bool) int {
	if len(args) == 0 {
		return runDaemon(debug)
	}

	switch args[0] {
	case "run":
		return cmdRun(args[1:], debug)
	case "status":
		return cmdStatus(args[1:])
	case "diagnose":
		return cmdDiagnose()
	case "profiles", "profile":
		return cmdProfiles(args[1:])
	case "override":
		return cmdOverride(args[1:])
	case "clear":
		return cmdClear()
	case "reload":
		return cmdReload()
	case "auto-detect":
		return cmdAutoDetect(args[1:])
	case "catalog":
		return cmdCatalog(args[1:])
	case "debug-rpc-image":
		return cmdDebugRPCImage(args[1:])
	case "debug-processes":
		return cmdDebugProcesses(args[1:])
	case "debug-scan":
		return cmdDebugScan(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		return 1
	}
}

func parseRunFlags(args []string) daemonOptions {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	trayEnabled := fs.Bool("tray", true, "Show icon in system tray")
	noTray := fs.Bool("no-tray", false, "Run without the system tray icon")
	fs.Parse(args)

	if *noTray {
		return daemonOptions{TrayOverride: boolPtr(false)}
	}
	if !*trayEnabled {
		return daemonOptions{TrayOverride: boolPtr(false)}
	}
	return daemonOptions{TrayOverride: boolPtr(true)}
}

func cmdRun(args []string, debug bool) int {
	opts := parseRunFlags(args)
	return runDaemonWithOptions(debug, opts)
}

func printUsage() {
	fmt.Print(`Usage: picord <command> [options]

Commands:
  run              Run the daemon (default if no command given)
                   Options: --tray / --no-tray
  status           Show current presence status
  diagnose         Test Discord Rich Presence connection
  profiles         List all profiles or create from catalog
  override         Set a manual override
  clear            Clear manual override
  reload           Reload configuration from disk
  auto-detect      Enable or disable auto-detection (on|off)
  catalog          Catalog management (status, search, refresh, enrich)
  debug-rpc-image  Test a Discord Rich Presence image
  debug-processes  Show process identity hints and Wine/Proton aliases
  debug-scan       Run a single process scan and print detected identities
  help             Show this help message

Status options:
  --verbose        Include aliases and Steam app IDs

Debug-processes options:
  --wine           Show only Wine-related processes
  --proton         Show only Proton-related processes
  --with-aliases   Show only processes with aliases
  --name <filter>  Filter by process name substring
  --pid <n>        Filter by exact PID
  --json           Output as JSON

Override options:
  -n, --name       Profile name
  -d, --details    Activity details
  -s, --state      Activity state
  -i, --image      Large image key

Catalog commands:
  picord catalog status
  picord catalog search <query>
  picord catalog refresh --source <source> [--max-pages N]
  picord catalog enrich [--batch-size N]

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

func getAPIToken() string {
	token, _ := server.LoadToken(server.TokenStateDir())
	return token
}

func apiGet(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, getAPIBase()+path, nil)
	if err != nil {
		return nil, err
	}
	if t := getAPIToken(); t != "" {
		req.Header.Set("X-Picord-Token", t)
	}
	return http.DefaultClient.Do(req)
}

func apiPost(path string, body any) (*http.Response, error) {
	return apiMethod(http.MethodPost, path, body)
}

func apiPut(path string, body any) (*http.Response, error) {
	return apiMethod(http.MethodPut, path, body)
}

func apiMethod(method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, getAPIBase()+path, reader)
	if err != nil {
		return nil, err
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t := getAPIToken(); t != "" {
		req.Header.Set("X-Picord-Token", t)
	}
	return http.DefaultClient.Do(req)
}

func apiDelete(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodDelete, getAPIBase()+path, nil)
	if err != nil {
		return nil, err
	}
	if t := getAPIToken(); t != "" {
		req.Header.Set("X-Picord-Token", t)
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

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Show verbose output including aliases")
	jsonOut := fs.Bool("json", false, "Output raw JSON response")
	fs.Parse(args)

	path := "/api/status"
	if *verbose {
		path += "?verbose=1"
	}
	resp, err := apiGet(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n%s\n", resp.Status, string(body))
		return 1
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		return 1
	}

	if *jsonOut {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			fmt.Fprintf(os.Stderr, "Error formatting JSON: %v\n", err)
			return 1
		}
		fmt.Println(pretty.String())
		return 0
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		return 1
	}

	active := safeString(result, "active_name")
	if active == "" {
		active = "(none)"
	}
	proc := safeString(result, "active_process")
	if proc == "" {
		proc = "(none)"
	}
	auto := safeBool(result, "auto_detect")
	override := safeBool(result, "has_override")

	fmt.Printf("Active Profile: %s\n", active)
	fmt.Printf("Active Process: %s\n", proc)
	fmt.Printf("Auto-Detect:    %v\n", auto)
	fmt.Printf("Has Override:   %v\n", override)

	if rpc, ok := result["rpc_connected"].(bool); ok {
		status := "disconnected"
		if rpc {
			status = "connected"
		}
		fmt.Printf("Discord:        %s\n", status)
	}
	if appID := safeString(result, "app_id"); appID != "" {
		fmt.Printf("App ID:         %s\n", appID)
	}

	if state := safeString(result, "scan_state"); state != "" {
		fmt.Printf("Scan State:     %s\n", state)
	}
	if mode := safeString(result, "scan_mode"); mode != "" {
		fmt.Printf("Scan Mode:      %s\n", mode)
	}
	if t := safeString(result, "last_scan_time"); t != "" {
		fmt.Printf("Last Scan:      %s\n", t)
	}

	if *verbose {
		if mi, ok := result["match_info"].(map[string]any); ok && mi != nil {
			fmt.Printf("\nMatch Info:\n")
			fmt.Printf("  Source:       %s\n", safeString(mi, "source"))
			if name := safeString(mi, "profile_name"); name != "" {
				fmt.Printf("  Profile:      %s\n", name)
			}
			if procName := safeString(mi, "process_name"); procName != "" {
				fmt.Printf("  Process:      %s\n", procName)
			}
			if reason := safeString(mi, "reason"); reason != "" {
				fmt.Printf("  Reason:       %s\n", reason)
			}
			if conf, ok := mi["confidence"].(float64); ok && conf > 0 {
				fmt.Printf("  Confidence:   %.0f\n", conf)
			}
			if appID := safeString(mi, "discord_app_id"); appID != "" {
				fmt.Printf("  Discord App:  %s\n", appID)
			}
			if connected, ok := mi["rpc_connected"].(bool); ok {
				fmt.Printf("  RPC Connected: %v\n", connected)
			}
		}
	}

	if procs, ok := result["detected_processes"].([]any); ok && len(procs) > 0 {
		fmt.Printf("\nDetected Processes:\n")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if *verbose {
			fmt.Fprintln(w, "PID\tNAME\tSTEAM\tALIASES\tWINDOW TITLE")
		} else {
			fmt.Fprintln(w, "PID\tNAME\tWINDOW TITLE")
		}
		for _, p := range procs {
			if m, ok := p.(map[string]any); ok {
				pid := safeFloat64(m, "pid")
				name := safeString(m, "name")
				title := safeString(m, "window_title")
				if title == "" {
					title = "-"
				}
				if *verbose {
					steam := safeString(m, "steam_app_id")
					if steam == "" {
						steam = "-"
					}
					aliases := "-"
					if a, ok := m["aliases"].([]any); ok && len(a) > 0 {
						parts := make([]string, len(a))
						for i, v := range a {
							parts[i] = fmt.Sprint(v)
						}
						aliases = strings.Join(parts, ", ")
					}
					fmt.Fprintf(w, "%.0f\t%s\t%s\t%s\t%s\n", pid, name, steam, aliases, title)
				} else {
					fmt.Fprintf(w, "%.0f\t%s\t%s\n", pid, name, title)
				}
			}
		}
		w.Flush()
	}

	return 0
}

func safeString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func safeBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func safeFloat64(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func cmdOverride(args []string) int {
	fs := flag.NewFlagSet("override", flag.ExitOnError)
	nameShort := fs.String("n", "", "Profile name")
	nameLong := fs.String("name", "", "Profile name")
	detailsShort := fs.String("d", "", "Activity details")
	detailsLong := fs.String("details", "", "Activity details")
	stateShort := fs.String("s", "", "Activity state")
	stateLong := fs.String("state", "", "Activity state")
	imageShort := fs.String("i", "", "Large image key")
	imageLong := fs.String("image", "", "Large image key")
	fs.Parse(args)

	name := *nameShort
	if name == "" {
		name = *nameLong
	}
	details := *detailsShort
	if details == "" {
		details = *detailsLong
	}
	state := *stateShort
	if state == "" {
		state = *stateLong
	}
	image := *imageShort
	if image == "" {
		image = *imageLong
	}

	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: name is required")
		return 1
	}

	p := profile.Profile{
		Name: name,
		Activity: profile.Activity{
			Details:    details,
			State:      state,
			LargeImage: image,
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

func cmdAutoDetect(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: picord auto-detect [on|off]")
		return 1
	}
	var enabled bool
	switch args[0] {
	case "on", "enable", "true", "1":
		enabled = true
	case "off", "disable", "false", "0":
		enabled = false
	default:
		fmt.Fprintf(os.Stderr, "Usage: picord auto-detect [on|off]\n")
		return 1
	}

	resp, err := apiPut("/api/settings", map[string]any{"auto_detect": enabled})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to picord daemon: %v\n", err)
		return 1
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("Auto-detect: %v\n", enabled)
	} else {
		printResponse(resp)
		return 1
	}
	return 0
}

func cmdDebugRPCImage(args []string) int {
	fs := flag.NewFlagSet("debug-rpc-image", flag.ExitOnError)
	assetKey := fs.String("asset-key", "", "Discord asset key to test")
	externalURL := fs.String("external-url", "", "External image URL to test")
	appIDFlag := fs.String("app-id", "", "Discord application client ID")
	fs.Parse(args)

	if *externalURL == "" && *assetKey == "" {
		fmt.Fprintln(os.Stderr, "Error: either --asset-key or --external-url is required")
		return 1
	}

	appID := strings.TrimSpace(*appIDFlag)
	if appID == "" {
		if cfg, err := config.Load(filepath.Join(configDirPath(), "picord", "config.yaml")); err == nil {
			appID = cfg.ResolveDiscordApp("main")
		}
	}
	if appID == "" {
		appID = config.DefaultDiscordAppID
	}

	client, err := rpc.NewClient(appID)
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
	fmt.Printf("Using Discord app ID: %s\n", appID)

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
	if *externalURL != "" {
		fmt.Println("\nIf the image appears correctly in Discord, enable it permanently by setting")
		fmt.Println("  images.external_validated: true")
		fmt.Println("in your picord config and restarting.")
	}

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh

	client.ClearActivity()
	fmt.Println("Activity cleared.")
	return 0
}

type debugProcessView struct {
	PID         int      `json:"pid"`
	Name        string   `json:"name"`
	WindowTitle string   `json:"window_title,omitempty"`
	SteamAppID  string   `json:"steam_app_id,omitempty"`
	DesktopID   string   `json:"desktop_id,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

func newDebugProcessView(p profile.DetectedProcess) debugProcessView {
	return debugProcessView{
		PID:         p.PID,
		Name:        p.Name,
		WindowTitle: p.WindowTitle,
		SteamAppID:  p.SteamAppID,
		DesktopID:   p.DesktopID,
		Aliases:     p.Aliases,
	}
}

func debugProcessViews(procs []profile.DetectedProcess) []debugProcessView {
	views := make([]debugProcessView, len(procs))
	for i, p := range procs {
		views[i] = newDebugProcessView(p)
	}
	return views
}

func debugProcessNameMatches(p profile.DetectedProcess, query string) bool {
	query = strings.ToLower(query)
	if query == "" {
		return true
	}
	fields := []string{p.Name, p.WindowTitle, p.SteamAppID, p.DesktopID}
	fields = append(fields, p.Aliases...)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

func cmdDebugProcesses(args []string) int {
	fs := flag.NewFlagSet("debug-processes", flag.ExitOnError)
	wineOnly := fs.Bool("wine", false, "Show only Wine-related processes")
	protonOnly := fs.Bool("proton", false, "Show only Proton-related processes")
	withAliases := fs.Bool("with-aliases", false, "Show only processes with aliases")
	nameFilter := fs.String("name", "", "Filter by process name (substring match)")
	pidFilter := fs.Int("pid", 0, "Filter by exact PID")
	jsonOut := fs.Bool("json", false, "Output as JSON")
	fs.Parse(args)

	procs := monitor.ResolveProcessIdentities()

	// Apply filters
	var filtered []profile.DetectedProcess
	for _, p := range procs {
		if *wineOnly && !strings.Contains(strings.ToLower(p.Name), "wine") {
			continue
		}
		if *protonOnly && !strings.Contains(strings.ToLower(p.Name), "proton") && !strings.HasPrefix(strings.ToLower(p.Name), "pressure-vessel-") {
			continue
		}
		if *withAliases && len(p.Aliases) == 0 {
			continue
		}
		if *nameFilter != "" && !debugProcessNameMatches(p, *nameFilter) {
			continue
		}
		if *pidFilter != 0 && p.PID != *pidFilter {
			continue
		}
		filtered = append(filtered, p)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(debugProcessViews(filtered))
		return 0
	}

	if len(filtered) == 0 {
		fmt.Println("No processes match the filter criteria.")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tNAME\tSTEAM APP\tALIASES\tWINDOW TITLE")
	for _, p := range filtered {
		steamAppID := p.SteamAppID
		if steamAppID == "" {
			steamAppID = "-"
		}
		aliases := "-"
		if len(p.Aliases) > 0 {
			aliases = strings.Join(p.Aliases, ", ")
		}
		title := p.WindowTitle
		if title == "" {
			title = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", p.PID, p.Name, steamAppID, aliases, title)
	}
	w.Flush()
	return 0
}

func cmdCatalog(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: catalog subcommand required")
		fmt.Fprintln(os.Stderr, "Usage: picord catalog status|search|refresh|enrich")
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
		query := strings.Join(args[1:], " ")
		return cmdCatalogSearch(query)
	case "refresh":
		return cmdCatalogRefresh(args[1:])
	case "enrich":
		return cmdCatalogEnrich(args[1:])
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
	resp, err := apiGet("/api/catalog/search?q=" + url.QueryEscape(query))
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
	source := fs.String("source", "", "Source to refresh (steam_local, steam_shortcuts, lutris_public, desktop)")
	maxPages := fs.Int("max-pages", 0, "Max pages to fetch (for paginated sources)")
	fs.Parse(args)

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

func cmdCatalogEnrich(args []string) int {
	fs := flag.NewFlagSet("enrich", flag.ExitOnError)
	batchSize := fs.Int("batch-size", 50, "Max entries to enrich in this run")
	fs.Parse(args)

	reqBody := map[string]any{"batch_size": *batchSize}
	resp, err := apiPost("/api/catalog/enrich", reqBody)
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

func cmdDiagnose() int {
	configDir := configDirPath()
	configPath := filepath.Join(configDir, "picord", "config.yaml")

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("Config:        failed to load (%v), using defaults\n", err)
		cfg = defaultConfig()
	} else {
		fmt.Println("Config:        loaded successfully")
	}

	appID := cfg.ResolveDiscordApp("main")
	fmt.Printf("App ID:        %s\n", appID)

	// Step 1: Socket discovery
	fmt.Println("\n--- Discord IPC Socket ---")
	socket, err := rpc.DiscoverSocket()
	if err != nil {
		fmt.Printf("Socket:        NOT FOUND (%v)\n", err)
		fmt.Println("\nDiagnosis: Discord is not running or the IPC socket is not available.")
		fmt.Println("Make sure Discord is running and you're not using a Flatpak/Snap")
		fmt.Println("version that isolates the IPC socket.")
		return 1
	}
	fmt.Printf("Socket:        %s\n", socket)

	// Step 2: Connect
	fmt.Println("\n--- Connection ---")
	client, err := rpc.NewClient(appID)
	if err != nil {
		fmt.Printf("Connect:       FAILED (%v)\n", err)
		fmt.Println("\nDiagnosis: Could not establish IPC connection to Discord.")
		fmt.Println("Common causes:")
		fmt.Println("  - Discord is running but the socket path is wrong")
		fmt.Println("  - Permission denied on the socket")
		fmt.Println("  - Another client is already connected and Discord limits connections")
		return 1
	}
	fmt.Println("Connect:       OK")
	defer client.Close()

	// Step 3: Send test activity
	fmt.Println("\n--- Rich Presence Test ---")
	testActivity := &rpc.RichActivity{
		Details:  "Picord Diagnostic",
		State:    "Testing connection...",
		Instance: false,
	}
	if err := client.SetActivity(testActivity); err != nil {
		fmt.Printf("SetActivity:   FAILED (%v)\n", err)
		fmt.Println("\nDiagnosis: Connected to Discord but could not set activity.")
		fmt.Println("Common causes:")
		fmt.Println("  - Invalid Application ID (app not registered or deleted)")
		fmt.Println("  - Discord rate limiting")
		return 1
	}
	fmt.Println("SetActivity:   OK")
	fmt.Println("\nDiagnosis: Discord Rich Presence is working!")
	fmt.Println("You should see 'Picord Diagnostic' in your Discord status now.")
	fmt.Println("(It will clear when this command exits)")

	// Keep it visible for a moment
	fmt.Println("\nClearing test activity in 3 seconds...")
	time.Sleep(3 * time.Second)
	_ = client.ClearActivity()
	return 0
}

func cmdDebugScan(args []string) int {
	fs := flag.NewFlagSet("debug-scan", flag.ExitOnError)
	verbose := fs.Bool("verbose", false, "Show match attempts against catalog")
	fs.Parse(args)

	fmt.Println("Running single process scan...")
	fmt.Println()

	procs := monitor.ResolveProcessIdentities()
	if len(procs) == 0 {
		fmt.Println("No processes detected.")
		fmt.Println("This usually means:")
		fmt.Println("  - scan_all_processes is false and no Discord IPC candidates were found")
		fmt.Println("  - all visible processes are excluded (browsers, Discord, terminals)")
		fmt.Println("  - the game is running but its process name is unexpected")
		return 0
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tNAME\tSTEAM\tALIASES\tWINDOW TITLE")
	for _, p := range procs {
		steam := p.SteamAppID
		if steam == "" {
			steam = "-"
		}
		aliases := "-"
		if len(p.Aliases) > 0 {
			aliases = strings.Join(p.Aliases, ", ")
		}
		title := p.WindowTitle
		if title == "" {
			title = "-"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", p.PID, p.Name, steam, aliases, title)
	}
	w.Flush()
	fmt.Printf("\nTotal detected processes: %d\n", len(procs))

	if *verbose {
		fmt.Println("\n--- Match Attempts ---")
		configDir := configDirPath()
		configPath := filepath.Join(configDir, "picord", "config.yaml")
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Printf("Cannot load config: %v\n", err)
			return 0
		}

		dataDir := os.Getenv("XDG_DATA_HOME")
		if dataDir == "" {
			home, _ := os.UserHomeDir()
			if home != "" {
				dataDir = filepath.Join(home, ".local", "share")
			}
		}
		dbPath := filepath.Join(dataDir, "picord", "catalog.db")

		store, err := catalog.Open(dbPath)
		if err != nil {
			fmt.Printf("Cannot open catalog: %v\n", err)
			return 0
		}
		defer store.Close()

		ctx := context.Background()
		matcher := catalog.NewMatcher(store)
		imgResolver := catalog.ImageResolver{
			Mode:            catalog.ImageMode(cfg.Images.Mode),
			GenericAssetKey: cfg.Images.GenericAssetKey,
			ExternalEnabled: cfg.Images.ExternalValidated,
			LocalAssetBase:  fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort),
		}

		pm := profile.NewManager(cfg.Profiles, profile.DefaultProfiles())

		for _, p := range procs {
			catResult := matcher.Match(ctx, p)
			profileMatch, _ := pm.Match([]profile.DetectedProcess{p})

			if catResult != nil || profileMatch != nil {
				fmt.Printf("\nPID %d %q:\n", p.PID, p.Name)
				if catResult != nil {
					p := catResult.ToProfile(imgResolver)
					fmt.Printf("  catalog: %q (confidence=%d reason=%s source=%s)\n",
						catResult.Entry.Title, catResult.Confidence, catResult.Reason, catResult.Entry.Source)
					fmt.Printf("    -> would show: %q / %q\n", p.Activity.Details, p.Activity.State)
				}
				if profileMatch != nil {
					fmt.Printf("  profile: %q (priority=%d type=%s)\n",
						profileMatch.Name, profileMatch.Priority, profileMatch.Match.Type)
				}
			}
		}
	}

	return 0
}
