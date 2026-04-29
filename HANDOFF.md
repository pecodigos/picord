# Picord — Agent Continuation Handoff

> **Date:** 2026-04-29  
> **Branch:** \`master\`  
> **Go:** 1.21+  
> **Status:** Builds; `go test -count=1 ./...`, `go vet ./...`, `go test -race ./...`, and `make build` pass. Post-Kimi audit found P0 integration blockers; next plan: `docs/plans/2026-04-29-post-kimi-stabilization.md`.

---

## 1. What This Is

Picord is a Linux daemon that auto-sets Discord Rich Presence. By default it scans all numeric `/proc/<pid>` entries, reads process/window metadata, currently extracts Steam AppID hints, matches against built-in profiles and a local SQLite catalog, and sends `SET_ACTIVITY` over Discord's local IPC protocol. Users can set `scan_all_processes: false` to use the narrower legacy scan. Features: system tray (D-Bus SNI), web GUI (embedded SPA at `localhost:17970`), CLI, 48 built-in profiles, rich game catalog foundation, template variables, window title matching, auto-reconnect, background catalog refresh.

**Current constraint:** Picord still needs a running Discord client and a valid Discord application client ID before it can publish Rich Presence. The scanner no longer depends on games opening Discord IPC sockets.

**Post-Kimi blocker:** catalog auto-detection is not reliably active yet. The daemon tries catalog fallback only when `profileMgr.Match` returns a process, but that process is nil when no profile matched. Default catalog refresh also references unsupported `lutris_local`. See `docs/plans/2026-04-29-post-kimi-stabilization.md` before implementing more providers.

---

## 2. File Tree

```
cmd/picord/
  main.go          — Entry point. Wires config, RPC, server, monitor, catalog, tray, cleanup.
  main_test.go     — Daemon/default-config and runtime helper coverage.
  cli.go           — CLI: run, status, profiles/from-catalog, override, clear, reload, catalog, debug-rpc-image.
internal/rpc/
  client.go        — Custom Discord IPC wire protocol. Handshake, frame I/O, reconnect.
  client_test.go   — Mock Unix socket tests for handshake, SET_ACTIVITY, reconnect, close.
internal/monitor/
  monitor.go       — /proc scanner, poll loop, process name resolution.
  hints.go         — Allowlisted process hints, currently Steam AppID-focused.
  window_linux.go  — Window title detection per compositor.
  *_test.go
internal/profile/
  types.go         — Profile, MatchRule, Activity, Button structs.
  matcher.go       — Profile.Matches(), FindBestMatch().
  manager.go       — Thread-safe profile store. Merge, CRUD, sort.
  render.go        — Template var replacement: {process_name}, {window_title}, {title}, {source}, {steam_app_id}.
  defaults.go      — //go:embed defaults.yaml loader.
  defaults.yaml    — built-in profiles.
  *_test.go
internal/catalog/
  types.go         — Entry, Alias, Image, DetectionHints, MatchResult structs.
  normalize.go     — Title normalization for search/matching.
  store.go         — SQLite store: Open, migrate, upsert, search, source state.
  migrations.go    — Schema DDL for entries, aliases, images, source_state.
  matcher.go       — Catalog matcher: Steam AppID → Lutris slug → desktop ID → executable → title.
  images.go        — Image download/cache helper and ImageResolver modes.
  source.go        — Source interface.
  source_steam.go  — Parse appmanifest_*.acf and import Steam titles.
  source_lutris.go — Paginated Lutris public API importer.
  source_desktop.go — Parse .desktop files for native apps.
  refresher.go     — Background refresh goroutine.
  testdata/ and *_test.go
internal/config/
  config.go        — YAML load/save, fsnotify watcher, CatalogConfig, ImageConfig.
  config_test.go
internal/server/
  server.go        — HTTP API, CORS, AppState, catalog endpoints.
  server_test.go
  web/index.html
  web/css/style.css
  web/js/api.js
  web/js/app.js
internal/tray/
  tray.go          — energye/systray wrapper.
  icon.png         — embedded tray icon.
icons/             — SVG source + PNG renders.
resources/         — systemd service, .desktop autostart entry.
docs/plans/        — implementation/audit plans.
.github/workflows/build.yml — CI: test, vet, build, release on tags.
Makefile — build, run, clean, install, fmt, lint, tidy, deb, appimage.
```

---

## 3. Dependencies

**Go modules (go.mod):**
- `github.com/energye/systray` v1.0.3 — D-Bus SNI tray. API: `MenuItem.Click(func)`, `AddMenuItemCheckbox`, `Run(onReady, onExit)` blocks.
- `github.com/fsnotify/fsnotify` v1.9.0 — Config file watcher.
- `gopkg.in/yaml.v3` v3.0.1 — YAML parsing.
- `modernc.org/sqlite` v1.28.0 — pure-Go SQLite catalog store.

**External Linux tools (optional):**
- \`hyprctl\` — Hyprland window titles
- \`swaymsg\` — Sway window titles
- \`wmctrl\` / \`xdotool\` — X11 window titles
- \`kdotool\` — KDE window titles (KDE 6+)
- \`xdg-open\` — Open browser from tray

**No Discord RPC library.** Protocol implemented from scratch in \`internal/rpc/client.go\`.

---

## 4. Build & Run

```bash
cd /mnt/hdd/Code/2026/picord
make build                          # go build -ldflags="-s -w" -o bin/picord ./cmd/picord
go test -count=1 ./...              # unit/integration tests
go vet ./...
go test -race ./...                  # race pass during post-Kimi audit

# Run daemon
./bin/picord                        # default: daemon + tray + web GUI
./bin/picord --debug run            # debug logging to stdout + ~/.local/state/picord/picord.log

# CLI (talks to running daemon via HTTP)
./bin/picord status
./bin/picord profiles
./bin/picord override -n "Name" -d "Details" -s "State" -i "ImageKey"
./bin/picord clear
./bin/picord reload
```

---

## 5. Architecture Deep Dive

### 5.1 Startup Sequence (cmd/picord/main.go)

1. Parse \`--debug\` flag → \`setupDebugLogging()\` (stdout + file)
2. \`config.Load(path)\` → reads \`~/.config/picord/config.yaml\` (creates defaults if missing)
3. \`newRPCManager(cfg.AppID)\` → creates an \`rpcManager\` wrapper. **Initial connect may fail** if Discord not running; daemon continues regardless.
4. Create \`server.AppState\` (autoDetect=true)
5. \`profile.NewManager(userProfiles, defaultProfiles)\` → merges defaults first, then user overrides by name
6. \`config.NewManager(path, onChange)\` → fsnotify watcher on config dir
7. Create \`server.Server\`, wire callbacks:
   - OnOverrideSet → setRichPresence(rpcMgr, p, nil) + tray status
   - OnOverrideClear → rpcMgr.clearActivity() + tray "Idle"
   - OnAutoDetectSet → update state, clear activity if disabled
   - OnReloadConfig → reload config from disk
   - OnProfilesSaved → configMgr.UpdateProfiles() (writes YAML)
8. server.StartServer() → http.ListenAndServe in goroutine
9. \`monitor.NewWithOptions(cfg.PollInterval, cfg.ScanAllProcesses, callback)\` + Start() → poll loop in goroutine
10. Background reconnect goroutine (10s ticker, stops via reconnectStopCh)
11. Signal handler (SIGINT/SIGTERM) → cleanup() → os.Exit(0)
12. tray.Run() → **blocks main goroutine**

**Cleanup order:** close reconnectStopCh → monitor.Stop() → rpcMgr.close() → httpServer.Close() → configMgr.Close()

### 5.2 Discord IPC Protocol (internal/rpc/client.go)

**Frame format:** uint32le opcode + uint32le length + JSON payload

**Opcodes:** 0=Handshake, 1=Frame, 2=Close

**Public API:**
```go
func DiscoverSocket() (string, error)
func NewClient(appID string) (*Client, error)
func (c *Client) SetActivity(activity *RichActivity) error
func (c *Client) ClearActivity() error
func (c *Client) Reconnect() error
func (c *Client) IsConnected() bool
func (c *Client) Close() error
```

**Handshake:** Send {v:"1", client_id:"<appID>"} (opcode 0). Expect response with cmd:"DISPATCH" and evt:"READY".

**Socket discovery order:**
1. $DISCORD_IPC_PATH env var
2. Flatpak: $XDG_RUNTIME_DIR/app/com.discordapp.Discord/discord-ipc-{0,1,2}
3. $XDG_RUNTIME_DIR/discord-ipc-0
4. /run/user/$UID/discord-ipc-0
5. /tmp/discord-ipc-0
6. Indices 0-9 for all above

**Critical limitation:** sendCommand() does writeFrame then readFrame synchronously. It expects exactly one response. If Discord sends unsolicited events (e.g., between command and response), the client will misread. This has **never been tested against real Discord.**

**Thread safety:** sendCommand, Reconnect, IsConnected, Close all hold c.mu. Safe for concurrent use.

**rpcManager wrapper (cmd/picord/main.go):**
```go
type rpcManager struct {
    mu     sync.Mutex
    client *rpc.Client
    appID  string
}
```
- `connect()` creates a new `rpc.Client` via injectable `rpcNewClient` when nil or disconnected.
- `isConnected()`, `setActivity()`, `clearActivity()`, `close()` are thread-safe.
- Background reconnect goroutine now calls `rpcMgr.connect()` when not connected, so Picord can start before Discord and connect later.

### 5.3 Process Monitor (internal/monitor/monitor.go)

Scans /proc every cfg.PollInterval seconds (default 2). Default mode (`scan_all_processes: true`) includes every numeric `/proc/<pid>` entry. Legacy mode (`scan_all_processes: false`) only includes processes with fd symlinks containing "discord-ipc".

```go
var procRoot = "/proc"  // overridden in tests

type Monitor struct {
    interval time.Duration
    scanAll  bool
    debug    bool
    stopCh   chan struct{}
    callback func([]profile.DetectedProcess)
}
```

readProcName(pid) resolution:
1. Read /proc/<pid>/cmdline (null-separated argv[0]). Take filepath.Base() — full name, NOT truncated.
2. Fallback to /proc/<pid>/comm (kernel thread name, max 15 chars).
3. Fallback to "unknown".

Debug logging: per-process and summary logs are gated behind `m.debug` (set via `monitor.SetDebug(bool)`). Non-debug mode is silent to avoid flooding systemd logs.

### 5.4 Window Title Detection (internal/monitor/window_linux.go)

DetectCompositor() returns: hyprland, sway, kde, x11, gnome-wayland, unknown. Uses env vars in priority order:
1. HYPRLAND_INSTANCE_SIGNATURE → hyprland
2. SWAYSOCK or XDG_CURRENT_DESKTOP contains "sway" → sway
3. XDG_CURRENT_DESKTOP contains "kde" or KDE_FULL_SESSION → kde
4. DISPLAY set AND XDG_SESSION_TYPE != "wayland" → x11
5. XDG_CURRENT_DESKTOP contains "gnome" AND wayland → gnome-wayland (no backend)

GetWindowTitles() → map[int]string (PID → title). Dispatches by compositor, then falls back through all backends.

| Backend | Tool | Notes |
|---------|------|-------|
| Hyprland | hyprctl clients -j | JSON array. Filters: mapped=true, hidden=false, pid>0. Falls back to class if title empty. |
| Sway | swaymsg -t get_tree | Recursive tree walk on nodes + floating_nodes. Matches type=="con" or "floating_con". |
| X11 | wmctrl -l -p | Text: <id> <desktop> <pid> <title>. Falls back to xdotool if unavailable. |
| X11 fb | xdotool search --onlyvisible + getwindowname + getwindowpid | 3 subshells per window (slow). |
| KDE | kdotool search + getwindowtitle + getwindowpid | KDE 6+. Falls back to unimplemented qdbus stub. |

All backends return empty/error if tool not in PATH. Fallback chain continues silently.

### 5.5 Profile System (internal/profile/*.go)

**Types (types.go):**
```go
type MatchType string
const (
    MatchProcessName MatchType = "process_name"  // exact, case-insensitive
    MatchWindowTitle MatchType = "window_title"  // substring, case-insensitive
    MatchRegex       MatchType = "regex"         // regex on proc.Name
)

type Profile struct {
    Name     string    `yaml:"name" json:"name"`
    Match    MatchRule `yaml:"match" json:"match"`
    Activity Activity  `yaml:"activity" json:"activity"`
    Priority int       `yaml:"priority" json:"priority"`
    Enabled  bool      `yaml:"enabled" json:"enabled"`
    isDefault bool     `yaml:"-" json:"-"` // unexported, set by defaults.go
}
```

**Matching (matcher.go):**
```go
type DetectedProcess struct {
    PID         int
    Name        string
    WindowTitle string `json:"window_title,omitempty"`
}

func (p Profile) Matches(proc DetectedProcess) int
// Returns priority >=0 on match, -1 on no match or disabled.

func FindBestMatch(profiles []Profile, processes []DetectedProcess) (*Profile, *DetectedProcess)
// Tie-breaker: higher priority wins. Same priority → longer Match.Value wins.
```

**Match semantics:**
- process_name: strings.ToLower(proc.Name) == strings.ToLower(value) — EXACT
- window_title: strings.Contains(strings.ToLower(proc.WindowTitle), strings.ToLower(value)) — SUBSTRING
- regex: regexp.Compile(value).MatchString(proc.Name) — on proc.Name, NOT window title. Compiled on EVERY call (not cached).

**Manager (manager.go):**
```go
type Manager struct {
    mu       sync.RWMutex
    profiles []Profile
    byName   map[string]int
}
```

Key methods: NewManager(), MergeDefaults(), MergeUser(), ReplaceUser(), Add(), Delete(), All(), Get(), Match(), SerializeProfiles(), DeserializeProfiles().

Important: `byName` stores **indices**, not pointers. It is rebuilt after every `sortByPriority()` and `Delete()` to avoid stale pointers after slice reallocations or element swaps.

Profiles are always kept sorted by priority (desc) then Match.Value length (desc).

**Defaults (defaults.go + defaults.yaml):**
- defaults.yaml embedded via //go:embed
- Loaded once via sync.Once
- All get isDefault=true and Priority=5 if unset
- 48 profiles: 12 emulators, 5 launchers, 4 source ports, 11 games, 8 editors, 4 media, 4 misc

**Template rendering (render.go):**
```go
func RenderActivity(act Activity, proc DetectedProcess) Activity
```
Replaces {process_name} → proc.Name, {window_title} → proc.WindowTitle. Applied to Details, State, LargeText, SmallText.

Called in setRichPresence() in main.go when proc != nil. Overrides (proc == nil) skip rendering.

### 5.6 Config (internal/config/config.go)

```go
type AppConfig struct {
    AppID            string            `yaml:"app_id" json:"app_id"`
    PollInterval     int               `yaml:"poll_interval" json:"poll_interval"`
    WebPort          int               `yaml:"web_port" json:"web_port"`
    ScanAllProcesses bool              `yaml:"scan_all_processes" json:"scan_all_processes"`
    Profiles         []profile.Profile `yaml:"profiles" json:"profiles"`
}
```

Defaults: AppID="", PollInterval=2, WebPort=17970, ScanAllProcesses=true.

Validation: PollInterval < 1 → 2. WebPort < 1 || > 65535 → 17970.

Load(path): If file missing, creates dir and writes defaults.

NewManager(path, onChange): Creates fsnotify watcher on the DIRECTORY (not file). Watches Write/Create on exact path. On change: re-Load, update state, call onChange(cfg).

**Race:** OnProfilesSaved → UpdateProfiles() writes file → watcher fires → reloads. Idempotent but could race with rapid writes.

### 5.7 HTTP API & Web GUI (internal/server/server.go)

Web assets embedded via //go:embed all:web. Served from /.

**Endpoints:**
- GET /api/status → {active_name, active_process, detected_processes[], auto_detect, has_override}
- GET /api/profiles → user profiles (non-default)
- POST /api/profiles → add profile (defaults: type=process_name, priority=5, enabled=true)
- GET /api/profiles/:name → get single profile
- PUT /api/profiles/:name → update profile (name overwritten from URL param)
- DELETE /api/profiles/:name → delete profile
- GET /api/defaults → built-in profiles
- POST /api/override → set manual override (body = Profile JSON)
- DELETE /api/override → clear manual override
- GET /api/settings → {auto_detect: bool}
- PUT /api/settings → set auto_detect
- POST /api/reload → trigger OnReloadConfig

**Server callbacks** (wired in main.go): OnOverrideSet, OnOverrideClear, OnAutoDetectSet, OnReloadConfig, OnProfilesSaved.

**AppState** (thread-safe via RWMutex): activeName, activeProc, detectedProcs, override, autoDetect.

**Web GUI:** Dark Discord-style theme. Sections: Status, Detected Processes, Manual Override, Your Profiles, Built-in Profiles, Settings. Auto-refreshes every 3 seconds. Modal for add/edit profiles. Supports match types process_name, window_title, regex.

### 5.8 System Tray (internal/tray/tray.go)

Uses github.com/energye/systray. Menu: Status label, Auto-Detect checkbox, Manual Override submenu, Open Settings, Reload Config, Quit.

**Important:** tray.Run(actions) blocks. This is the MAIN goroutine. Everything else runs in background goroutines.

UpdateStatus(text) and SetAutoDetectState(enabled) update global package-level vars (statusItem, autoDetectItem). Safe because systray callbacks and setters run on the same (main) goroutine.

---

## 6. Data Flow

```
/proc/<pid> entries (default scan_all_processes: true)
    │
    └── legacy mode: /proc/*/fd/ discord-ipc symlinks only
    ↓
monitor.Monitor (every 2s)
    ↓
callback(procs) in main.go
    ├── state.SetDetected(procs)
    ├── if override || !autoDetect → return
    ├── match, proc := profileMgr.Match(procs)
    │       └── FindBestMatch(profiles, procs)
    │           └── Profile.Matches(proc)
    └── if match != nil:
            setRichPresence(client, match, proc)
                ├── RenderActivity(match.Activity, proc) // template vars
                └── client.SetActivity(rpc.RichActivity)
                        └── sendCommand("SET_ACTIVITY", args)
                                ├── writeFrame(opFrame, json)
                                └── readFrame() ← expects response
            tray.UpdateStatus(match.Name)
        else if had previous match:
            client.ClearActivity()
            tray.UpdateStatus("Idle")
```

**Web GUI → Save profile flow:**
Browser POST /api/profiles → handleProfiles → profileMgr.Add() → notifyProfilesChanged() → OnProfilesSaved() → configMgr.UpdateProfiles() → writes config.yaml → fsnotify watcher fires → reloads config → profileMgr.MergeUser() (idempotent re-merge).

---

## 7. Config File

Path: ~/.config/picord/config.yaml (or $XDG_CONFIG_HOME/picord/config.yaml)

```yaml
app_id: "123456789012345678"    # REQUIRED — Discord application client ID
poll_interval: 2                # Seconds between scans
web_port: 17970                 # Web GUI port
scan_all_processes: true        # Scan ordinary apps/games; false = legacy IPC-only scan
profiles:                       # Optional custom profiles
  - name: "My Game"
    match:
      type: process_name        # process_name | window_title | regex
      value: "mygame"
    activity:
      details: "Playing {window_title}"   # Template vars supported
      state: "via {process_name}"
      large_image: "mygame_art"
      large_text: "My Game"
    priority: 10
    enabled: true
```

---

## 8. Test Coverage

57 tests across 7 packages. Run: go test ./...

| File | Tests | What they cover |
|------|-------|-----------------|
| cmd/picord/main_test.go | 4 | Fallback default config; rpcManager connect/reconnect/close with mock Unix socket |
| internal/rpc/client_test.go | 4 | Mock Discord IPC socket: handshake, SET_ACTIVITY frame format, reconnect, close |
| profile/matcher_test.go | 8 | process_name exact+ci, window_title substring, regex, invalid regex, disabled, FindBestMatch priority + tie-breaker |
| profile/manager_test.go | 9 | Merge defaults+user, user overrides default, add, update, delete, sort, match, ReplaceUser, stable after sort+delete |
| profile/render_test.go | 5 | No templates, {process_name}, {window_title}, mixed, empty window title |
| config/config_test.go | 6 | Load/save round-trip, defaults on missing file, scan_all_processes default/false handling, validation clamping, invalid YAML error |
| monitor/monitor_test.go | 7 | Mock /proc: IPC-only scan, all-process scan, ScanNow options, dedup, Start/Stop no panic |
| monitor/window_linux_test.go | 3 | Hyprland JSON parse, Sway tree walk, DetectCompositor for all 5 types |
| internal/catalog/store_test.go | 7 | NormalizeTitle, migration, UpsertEntry, GetEntry, SearchByAlias, SearchTitlePrefix, ExactTitleMatch, SourceState |
| internal/catalog/matcher_test.go | 6 | Steam AppID match, Lutris slug match, executable match, exact title match, no match, ToProfile conversion |
| internal/catalog/images_test.go | 4 | DownloadImage accepts PNG, rejects HTML/text, ImageResolver modes, ImageCacheDir |
| internal/catalog/source_steam_test.go | 2 | ACF parser, SteamLocalSource refresh with mock steamapps dir |
| internal/catalog/source_lutris_test.go | 3 | Lutris public refresh with httptest, MaxPages respect, offline skip |
| internal/catalog/source_desktop_test.go | 2 | Desktop file parser, DesktopSource refresh with mock applications dir |
| internal/catalog/refresher_test.go | 3 | Start/Stop, Stop waits, BuildSources valid/unknown |
| internal/server/server_test.go | 6 | Catalog status, search, entry, refresh, profile-from-entry, missing query handling |

**Untested (0% coverage):**
- internal/tray/tray.go — GUI, hard to unit test
- cmd/picord/cli.go — needs mock HTTP server
- cmd/picord/main.go — mostly integration-only (defaultConfig helper covered; rpcManager tested via main_test.go)

---

## 9. Known Issues & Limitations

### CRITICAL: Needs live Discord validation
- rpc/client.go now has mock Unix socket coverage for handshake, SET_ACTIVITY, reconnect, and close.
- It still has not connected to a live Discord client in this repo.
- Handshake expects cmd:"DISPATCH" + evt:"READY". Real Discord may differ.
- sendCommand() reads exactly ONE response frame. Unsolicited Discord events could break it.
- **Fix if real Discord exposes this:** Add a background frame reader goroutine that routes responses by nonce and handles unsolicited events separately.

### CRITICAL: Discord image mode unvalidated
- A `picord debug-rpc-image` CLI command exists to test asset keys and external URLs against a live Discord client.
- External URL mode has NOT been validated against a real Discord client yet.
- Until validated, `images.mode` defaults to `generic` and `external_url` should not be enabled.
- **Validation needed:** Run `picord debug-rpc-image --app-id <ID> --external-url https://cdn.akamai.steamstatic.com/steam/apps/620/header.jpg` with Discord running and observe if the image appears.

### RESOLVED: Pre-emptive process matching
- `scan_all_processes` now defaults to true and scans every numeric `/proc/<pid>` entry.
- `scan_all_processes: false` keeps the legacy Discord-IPC-only scan for narrower detection.
- Matching still uses the same profile semantics: process_name exact match, window_title substring, regex on process name.

### RESOLVED: Profile manager stale pointers
- `byName` was `map[string]*Profile` pointing into a mutable slice. After sort/append/delete, pointers could become stale.
- Fixed: `byName` is now `map[string]int` (index into slice). Rebuilt after every `sortByPriority()` and `Delete()`.

### RESOLVED: Discord startup-order reconnect
- If Discord was not running at daemon startup, `rpcClient` stayed nil and the reconnect goroutine never created a client later.
- Fixed: Replaced raw `*rpc.Client` with `rpcManager` wrapper that can `connect()` when nil. Background goroutine now calls `rpcMgr.connect()` when not connected.

### RESOLVED: Noisy monitor logging
- With `scan_all_processes: true`, the monitor logged every detected process every 2 seconds.
- Fixed: Per-process and summary logs are now gated behind `monitor.SetDebug(true)`.

### MEDIUM: Regex compiled on every match
- matcher.go:37 calls regexp.Compile() every 2 seconds per regex profile.
- **Fix:** Cache compiled regexes in profile.Manager.

### MEDIUM: Config watcher race
- OnProfilesSaved writes config → fsnotify fires → reloads. Idempotent but theoretically racy.
- **Fix:** Debounce fsnotify events (e.g., 100ms) or suppress reloads from own PID.

### MEDIUM: Tray on pure GNOME Wayland
- GNOME needs the AppIndicator/KStatusNotifierItem extension for D-Bus SNI.
- **Workaround:** Web GUI at localhost:17970 has full functionality.

### LOW: KDE window fallback unimplemented
- window_linux.go:240 — getKDEDBusWindows() is a stub.

### LOW: README outdated
- README line 134 says "window_title coming in V2" — it's implemented. Update match types table.

### LOW: CI Go version mismatch
- .github/workflows/build.yml uses go-version: '1.26' but go.mod says go 1.21. Not harmful but inconsistent.

---

## 10. What Works / What Doesn't

| Area | Status |
|------|--------|
| Compilation | ✅ `make build` passes |
| Tests | ✅ `go test -count=1 ./...` passes |
| Race tests | ✅ `go test -race ./...` passes |
| go vet | ✅ Clean |
| CLI commands | ⚠️ Catalog/profile command naming and long flags need cleanup |
| Debug logging | ✅ --debug flag |
| Config load/save/reload | ⚠️ Reload only partially applies runtime changes |
| Web GUI | ⚠️ Embedded SPA works, but catalog JSON/UI escaping need fixes |
| Profile matching | ✅ process_name, window_title, regex |
| Catalog auto-detection | ❌ Fallback bug prevents catalog-only matches in daemon |
| Template variables | ✅ {process_name}, {window_title}, {title}, {source}, {steam_app_id} |
| Window title detection | ✅ Hyprland, Sway, X11, KDE best-effort |
| Auto-reconnect | ⚠️ Connects later, but does not replay desired presence yet |
| System tray | ⚠️ Depends on compositor SNI support |
| Actual Discord RPC | ⚠️ Mock socket covered; needs live Discord validation |
| Pre-emptive matching | ✅ scan_all_processes default covers ordinary apps/games |
| Catalog refresh defaults | ❌ Default includes unsupported `lutris_local` |
| Repository hygiene | ❌ Root `picord` binary is tracked |
| KDE dbus fallback | ❌ Stub |
| AppImage packaging | ❌ Makefile stub |

---

## 11. Refined Implementation Plan

### Phase 0. Baseline (complete)
- Commit the current handoff state before making new changes.
- Verify `go test ./...` and `go vet ./...` are green.

### Phase 1. Pre-emptive process matching (complete)
Goal: make Picord useful for ordinary games and apps that never connect to Discord IPC.

Completed TDD tasks:
1. Config: add `scan_all_processes` with default `true`; verify missing config files and partial YAML keep the default enabled, while explicit `false` is preserved.
2. Monitor: add an all-process scan path that reads every numeric `/proc/<pid>` entry, resolves names with the existing cmdline/comm fallback, keeps window-title enrichment, and deduplicates by PID.
3. Daemon wiring: pass `cfg.ScanAllProcesses` into the monitor and include it in fallback/default config.
4. Docs/UI: document the new config option and make the status/labels refer to detected processes rather than only Discord IPC connections.
5. Verification: run `go test ./...`, `go vet ./...`, and build.

Design choices:
- Default `scan_all_processes: true` because the handoff identifies IPC-only scanning as the main functional blocker.
- Keep the old IPC-only mode behind `scan_all_processes: false` for users who want the narrower legacy behavior.
- Avoid profile-manager changes in this phase; matching semantics stay the same.

### Phase 2. RPC client tests (complete)
Created `internal/rpc/client_test.go` with a mock Unix socket server implementing the Discord protocol. Covered handshake, SET_ACTIVITY frame format, reconnect, and close. Live Discord validation remains the next RPC step.

### Phase 3. Stabilization (complete)
Fixed high-priority prerequisites before scaling to a catalog:
1. ✅ Fix `profile.Manager` indexing: changed `byName map[string]*Profile` to `map[string]int` and rebuild after sort/delete. Added `TestManager_StableAfterSortAndDelete`.
2. ✅ Fix Discord startup-order reconnect: replaced raw `*rpc.Client` with `rpcManager` wrapper that can `connect()` when nil. Background goroutine now creates a client if Discord starts after Picord. Added mock-socket tests for `rpcManager`.
3. ✅ Gate noisy monitor logging: per-process and summary logs now require `monitor.SetDebug(true)`.

### Phase 4. Rich game catalog foundation (implemented by Kimi)
Kimi implemented the first catalog foundation pass after the original plan in `docs/plans/2026-04-29-kimi-rich-game-catalog.md`:

1. SQLite catalog store and schema.
2. Steam local, Lutris public, and desktop source adapters.
3. Catalog matcher and image resolver/cache helpers.
4. Catalog HTTP endpoints, CLI commands, and web UI search/suggestions.
5. Background refresher and tests.

Post-implementation audit found that the feature is not yet production-ready in the daemon. The next plan is saved at `docs/plans/2026-04-29-post-kimi-stabilization.md`.

### Phase 5. Post-Kimi stabilization (next)
Do these before adding new catalog providers or bigger image datasets:

1. Fix catalog fallback so catalog-only detected games can actually set Rich Presence.
2. Fix default sources (`lutris_local` is unsupported) and remove the tracked root `picord` binary.
3. Fix catalog JSON DTOs so the web UI receives lowercase/snake-case fields.
4. Replay desired presence after Discord reconnect and remove the startup IPC probe leak.
5. Fix Discord IPC socket discovery for nonzero sockets and env override ordering.
6. Sanitize `/api/status`, restrict local API write access, and remove UI injection hazards.
7. Harden SQLite migrations, alias replacement, Lutris cursor/rate-limit behavior, and refresher shutdown.

Keep image URLs conservative: do not send external Rich Presence image URLs by default until live Discord validation succeeds.

---

## 12. Code Patterns

- Errors wrapped: fmt.Errorf("...: %w", err)
- Log errors: log.Printf("...: %v", err) — never log.Fatal in library code
- Mutex pattern: m.mu.Lock(); defer m.mu.Unlock()
- Embedding: //go:embed all:web, //go:embed defaults.yaml, //go:embed icon.png
- Import order: stdlib, blank, third-party, internal

---

## 13. How to Continue

1. Read this file fully.
2. Read `docs/plans/2026-04-29-post-kimi-stabilization.md`.
3. Run `go test -count=1 ./...`, `go vet ./...`, `go test -race ./...`, and `make build`.
4. Start with Phase A from the post-Kimi plan.
5. Make minimal changes. Add tests first for regressions.
6. Commit each logical fix and push.
7. Update this HANDOFF.md if architecture or actual status changes significantly.

*End of handoff.*
