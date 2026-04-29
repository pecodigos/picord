# Picord — Agent Continuation Handoff

> **Date:** 2026-04-29  
> **Branch:** \`master\`  
> **Go:** 1.21+  
> **Status:** Builds, \`go test ./...\` passes (38 tests), \`go vet\` clean.

---

## 1. What This Is

Picord is a Linux daemon that auto-sets Discord Rich Presence. By default it scans all numeric \`/proc/<pid>\` entries, matches them against profiles, and sends \`SET_ACTIVITY\` over Discord's local IPC protocol. Users can set \`scan_all_processes: false\` to use the narrower legacy scan that only considers processes already connected to Discord IPC. Features: system tray (D-Bus SNI), web GUI (embedded SPA at \`localhost:17970\`), CLI, 48 built-in profiles, template variables, window title matching, auto-reconnect.

**Current constraint:** Picord still needs a running Discord client and a valid Discord application client ID before it can publish Rich Presence. The scanner no longer depends on games opening Discord IPC sockets.

---

## 2. File Tree

```
cmd/picord/
  main.go          (330L) — Entry point. Wires all subsystems, signal handling, cleanup.
  main_test.go     (10L) — Fallback default config coverage.
  cli.go           (248L) — CLI: run, status, profiles, override, clear, reload, help.
internal/rpc/
  client.go        (314L) — Custom Discord IPC wire protocol. Handshake, frame I/O, reconnect.
internal/monitor/
  monitor.go       (170L) — /proc scanner, poll loop, process name resolution.
  window_linux.go  (248L) — Window title detection per compositor.
  monitor_test.go  (152L)
  window_linux_test.go (106L)
internal/profile/
  types.go         (43L) — Profile, MatchRule, Activity, Button structs.
  matcher.go       (84L) — Profile.Matches(), FindBestMatch().
  manager.go       (161L) — Thread-safe profile store. Merge, CRUD, sort.
  render.go        (29L) — Template var replacement: {process_name}, {window_title}.
  defaults.go      (33L) — //go:embed defaults.yaml loader.
  defaults.yaml    (549L) — 48 built-in profiles.
  matcher_test.go  (131L)
  manager_test.go  (133L)
  render_test.go   (82L)
internal/config/
  config.go        (167L) — YAML load/save, fsnotify watcher, validation.
  config_test.go   (145L)
internal/server/
  server.go        (336L) — HTTP API (7 endpoints), CORS, AppState.
  web/index.html
  web/css/style.css
  web/js/api.js
  web/js/app.js
internal/tray/
  tray.go          (103L) — energye/systray wrapper.
  icon.png         (32px, embedded)
icons/             — SVG source + PNG renders (16/32/128/256px)
resources/         — systemd service, .desktop autostart entry
.github/workflows/build.yml — CI: test, vet, build, release on tags
Makefile — build, run, clean, install, fmt, lint, tidy, deb, appimage
```

---

## 3. Dependencies

**Go modules (go.mod):**
- \`github.com/energye/systray\` v1.0.3 — D-Bus SNI tray. API: \`MenuItem.Click(func)\`, \`AddMenuItemCheckbox\`, \`Run(onReady, onExit)\` blocks.
- \`github.com/fsnotify/fsnotify\` v1.9.0 — Config file watcher.
- \`gopkg.in/yaml.v3\` v3.0.1 — YAML parsing.

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
go test ./...                       # 38 tests
go vet ./...

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
3. \`rpc.NewClient(cfg.AppID)\` → discovers Discord IPC socket, dials Unix, handshake. **May fail** if Discord not running; daemon continues regardless.
4. Create \`server.AppState\` (autoDetect=true)
5. \`profile.NewManager(userProfiles, defaultProfiles)\` → merges defaults first, then user overrides by name
6. \`config.NewManager(path, onChange)\` → fsnotify watcher on config dir
7. Create \`server.Server\`, wire callbacks:
   - OnOverrideSet → setRichPresence(client, p, nil) + tray status
   - OnOverrideClear → client.ClearActivity() + tray "Idle"
   - OnAutoDetectSet → update state, clear activity if disabled
   - OnReloadConfig → reload config from disk
   - OnProfilesSaved → configMgr.UpdateProfiles() (writes YAML)
8. server.StartServer() → http.ListenAndServe in goroutine
9. \`monitor.NewWithOptions(cfg.PollInterval, cfg.ScanAllProcesses, callback)\` + Start() → poll loop in goroutine
10. Background reconnect goroutine (10s ticker, stops via reconnectStopCh)
11. Signal handler (SIGINT/SIGTERM) → cleanup() → os.Exit(0)
12. tray.Run() → **blocks main goroutine**

**Cleanup order:** close reconnectStopCh → monitor.Stop() → client.ClearActivity() + Close() → httpServer.Close() → configMgr.Close()

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

### 5.3 Process Monitor (internal/monitor/monitor.go)

Scans /proc every cfg.PollInterval seconds (default 2). Default mode (`scan_all_processes: true`) includes every numeric `/proc/<pid>` entry. Legacy mode (`scan_all_processes: false`) only includes processes with fd symlinks containing "discord-ipc".

```go
var procRoot = "/proc"  // overridden in tests

type Monitor struct {
    interval time.Duration
    scanAll  bool
    stopCh   chan struct{}
    callback func([]profile.DetectedProcess)
}
```

readProcName(pid) resolution:
1. Read /proc/<pid>/cmdline (null-separated argv[0]). Take filepath.Base() — full name, NOT truncated.
2. Fallback to /proc/<pid>/comm (kernel thread name, max 15 chars).
3. Fallback to "unknown".

Debug logging: when --debug, logs every detected process with PID, Name, WindowTitle.

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
    byName   map[string]*Profile
}
```

Key methods: NewManager(), MergeDefaults(), MergeUser(), ReplaceUser(), Add(), Delete(), All(), Get(), Match(), SerializeProfiles(), DeserializeProfiles().

Important: ReplaceUser() was fixed from a deadlock. It uses mergeUserUnlocked() (private, no lock) after acquiring the mutex.

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

38 tests across 5 packages. Run: go test ./...

| File | Tests | What they cover |
|------|-------|-----------------|
| cmd/picord/main_test.go | 1 | Fallback default config keeps scan-all enabled |
| profile/matcher_test.go | 8 | process_name exact+ci, window_title substring, regex, invalid regex, disabled, FindBestMatch priority + tie-breaker |
| profile/manager_test.go | 8 | Merge defaults+user, user overrides default, add, update, delete, sort, match, ReplaceUser keeps defaults |
| profile/render_test.go | 5 | No templates, {process_name}, {window_title}, mixed, empty window title |
| config/config_test.go | 6 | Load/save round-trip, defaults on missing file, scan_all_processes default/false handling, validation clamping, invalid YAML error |
| monitor/monitor_test.go | 7 | Mock /proc: IPC-only scan, all-process scan, ScanNow options, dedup, Start/Stop no panic |
| monitor/window_linux_test.go | 3 | Hyprland JSON parse, Sway tree walk, DetectCompositor for all 5 types |

**Untested (0% coverage):**
- internal/rpc/client.go — needs mock Unix socket
- internal/server/server.go — needs HTTP test server
- internal/tray/tray.go — GUI, hard to unit test
- cmd/picord/cli.go — needs mock HTTP server
- cmd/picord/main.go — mostly integration-only (defaultConfig helper covered)

---

## 9. Known Issues & Limitations

### CRITICAL: Never tested against real Discord
- rpc/client.go implements the protocol per Discord docs but has never connected to a live Discord client.
- Handshake expects cmd:"DISPATCH" + evt:"READY". Real Discord may differ.
- sendCommand() reads exactly ONE response frame. Unsolicited Discord events will break it.
- **Fix:** Add a background frame reader goroutine that routes responses by nonce. Handle unsolicited events separately.

### RESOLVED: Pre-emptive process matching
- `scan_all_processes` now defaults to true and scans every numeric `/proc/<pid>` entry.
- `scan_all_processes: false` keeps the legacy Discord-IPC-only scan for narrower detection.
- Matching still uses the same profile semantics: process_name exact match, window_title substring, regex on process name.

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

| Feature | Status |
|---------|--------|
| Compilation | ✅ Go 1.21+ |
| Tests | ✅ 38/38 pass |
| go vet | ✅ Clean |
| CLI commands | ✅ All work |
| Debug logging | ✅ --debug flag |
| Config load/save/reload | ✅ |
| Web GUI | ✅ Embedded SPA |
| Profile matching | ✅ process_name, window_title, regex |
| Template variables | ✅ {process_name}, {window_title} |
| Window title detection | ✅ Hyprland, Sway, X11 (best-effort) |
| Auto-reconnect | ✅ Reconnect() + background ticker |
| System tray | ⚠️ Depends on compositor SNI support |
| Actual Discord RPC | ❌ NEVER TESTED with real Discord |
| Pre-emptive matching | ✅ scan_all_processes default covers ordinary apps/games |
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

### Phase 2. RPC client tests (next)
Create `internal/rpc/client_test.go` with a mock Unix socket server implementing the Discord protocol. Test handshake, SET_ACTIVITY frame format, reconnect, and close. This prepares for real-Discord validation.

### Phase 3. Follow-up improvements
1. TEST WITH REAL DISCORD: create Discord app → set app_id → run `./bin/picord --debug run` with Discord running, capture real handshake/SET_ACTIVITY behavior.
2. Cache compiled regexes in `profile.Manager`.
3. Add timestamps support (`start_time`) across config, GUI, CLI, and RPC payload.
4. Keep README and HANDOFF updated as architecture changes.

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
2. Run go test ./... and go vet ./....
3. Pick the highest-impact remaining item from the refined plan.
4. Make minimal changes. Add tests.
5. Run tests and vet again.
6. Update this HANDOFF.md if architecture changes significantly.

*End of handoff.*
