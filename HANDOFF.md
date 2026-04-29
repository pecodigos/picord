# Picord — Agent Continuation Handoff

> **Date:** 2026-04-29  
> **Branch:** \`master\`  
> **Go:** 1.21+  
> **Status:** Builds; `go test -count=1 ./...`, `go vet ./...`, `go test -race ./...`, `make build`, and diff checks pass after Kimi's third stabilization pass. Latest next-step plan: `docs/plans/2026-04-29-post-kimi-third-pass-stabilization.md`.
>
> **Current audit:** Kimi fixed several previous P0/P1 items, but the pass is not fully complete. Desired activity is now stored while initially disconnected; hostile-origin unsafe requests are rejected; profile edits preserve existing `Enabled`; catalog candidates are ranked against profile candidates; and `external_url` image mode is gated behind `images.external_validated`. Remaining priority work: built-in default profiles currently appear disabled by default, live Discord restart/stale-socket replay remains incomplete, unsafe API writes still need real token/content-type/JSON-error protection, config reload semantics are partial, and profile rename/default-copy behavior still needs hardening.

---

## 1. What This Is

Picord is a Linux daemon that auto-sets Discord Rich Presence. By default it scans all numeric `/proc/<pid>` entries, reads process/window metadata, currently extracts Steam AppID hints, matches against configured profiles and a local SQLite catalog, and sends `SET_ACTIVITY` over Discord's local IPC protocol. Users can set `scan_all_processes: false` to use the narrower legacy scan. Features: system tray (D-Bus SNI), web GUI (embedded SPA at `localhost:17970`), CLI, intended 48 built-in profiles (currently blocked by the defaults `Enabled` bug), rich game catalog foundation, template variables, window title matching, auto-reconnect, background catalog refresh.

**Current constraint:** Picord still needs a running Discord client and a valid Discord application client ID before it can publish Rich Presence. The scanner no longer depends on games opening Discord IPC sockets.

**Current status:** Kimi's latest pass improved the runtime path but did not close every stabilization item. The latest plan is `docs/plans/2026-04-29-post-kimi-third-pass-stabilization.md`; start with restoring built-in default profiles, then RPC stale-connection replay, then local write protection.

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
1. `$DISCORD_IPC_PATH` env var if it exists.
2. Indexed `discord-ipc-0` through `discord-ipc-9` under:
   - `$XDG_RUNTIME_DIR`
   - `/run/user/$UID`
   - `os.TempDir()`
3. Indexed Flatpak path `$XDG_RUNTIME_DIR/app/com.discordapp.Discord/discord-ipc-{0..9}`.

**Remaining socket gap:** discovery currently returns the first existing path, even if it is a stale regular file. Next pass should dial/handshake candidates in order and test with real Unix sockets.

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
- Background reconnect goroutine calls `rpcMgr.connect()` when not connected, so Picord can start before Discord and connect later.
- `rpcManager` stores `desiredActivity` and replays it after connect, but `setRichPresence()` still returns before calling `rm.setActivity()` when disconnected. This means desired presence is not recorded if the first match happens while Discord is down; see the follow-up plan.

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
- Known current bug: defaults.yaml omits `enabled: true`, while `MergeDefaults()` only appends enabled profiles. Built-in defaults therefore appear disabled/not merged until `DefaultProfiles()` sets Enabled=true by default or YAML is updated.
- 48 profiles intended: 12 emulators, 5 launchers, 4 source ports, 11 games, 8 editors, 4 media, 4 misc

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
    Catalog          CatalogConfig     `yaml:"catalog" json:"catalog"`
    Images           ImageConfig       `yaml:"images" json:"images"`
}
```

Defaults: AppID="", PollInterval=2, WebPort=17970, ScanAllProcesses=true. Catalog defaults: enabled=true, auto_refresh=true, sources=`[steam_local, desktop]`, refresh_hours=24. Image defaults: mode=`generic`, generic_asset_key=`picord_game`, cache enabled, external_validated=false.

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
- GET /api/catalog/status → {enabled, entry_count, alias_count}
- GET /api/catalog/search?q=... → []catalogEntryResponse with snake_case fields
- GET /api/catalog/entries/:id → catalogEntryResponse
- POST /api/catalog/refresh → refresh one source (`steam_local`, `desktop`, `lutris_public`)
- POST /api/catalog/profiles/from-entry/:id → create a profile from a catalog entry

**Server callbacks** (wired in main.go): OnOverrideSet, OnOverrideClear, OnAutoDetectSet, OnReloadConfig, OnProfilesSaved.

**AppState** (thread-safe via RWMutex): activeName, activeProc, detectedProcs, override, autoDetect.

**Web GUI:** Dark Discord-style theme. Sections: Status, Detected Processes, Catalog, Manual Override, Your Profiles, Built-in Profiles, Settings. Auto-refreshes every 3 seconds. Modal for add/edit profiles. Supports match types process_name, window_title, regex. Dynamic content is now mostly DOM/textContent based, but static inline onclick handlers remain.

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
    ├── selectBestPresence(procs, profileMgr, catalogMatcher, imgResolver)
    │       ├── profile candidate from profileMgr.Match(procs)
    │       ├── best catalog candidate from detected hints
    │       │       └── Steam AppID → Lutris slug → desktop ID → executable → exact title/window
    │       ├── score profile as priority*10
    │       ├── score catalog as match confidence
    │       └── choose higher score; tie currently prefers profile
    ├── if selected → setRichPresence(profile, proc)
    └── else if had previous match:
            rpcMgr.clearActivity()
            tray.UpdateStatus("Idle")
```

Important follow-up: ranking now compares profile and catalog candidates, but it is still a heuristic. It does not yet distinguish user profiles from built-in defaults or broad launcher defaults from exact app/game defaults. Also note current built-in defaults appear disabled until the defaults `Enabled` bug is fixed.

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

110 test functions/benchmarks across 17 `_test.go` files. Run: `go test -count=1 ./...`.

| File | Tests | What they cover |
|------|-------|-----------------|
| cmd/picord/main_test.go | 13 | Fallback defaults, rpcManager retry/replay, default catalog sources, catalog/profile candidate selection, disconnected desired storage |
| internal/rpc/client_test.go | 8 | Mock Discord IPC handshake, SET_ACTIVITY payloads, reconnect, close, socket discovery ordering |
| internal/profile/matcher_test.go | 8 | process_name exact+ci, window_title substring, regex, invalid regex, disabled, FindBestMatch priority + tie-breaker |
| internal/profile/manager_test.go | 10 | Merge defaults+user, overrides, add/update/delete, sort, match, ReplaceUser, preserve Enabled on update, stable after sort/delete |
| internal/profile/render_test.go | 5 | Activity template rendering |
| internal/config/config_test.go | 6 | Load/save, defaults, scan_all_processes, validation, invalid YAML |
| internal/monitor/monitor_test.go | 8 | Mock /proc scans, all-process and IPC-only modes, hints, dedup, Start/Stop |
| internal/monitor/hints_test.go | 8 | Steam/env/desktop hint extraction and allowlist parsing |
| internal/monitor/window_linux_test.go | 3 | Hyprland, Sway, compositor detection |
| internal/catalog/store_test.go | 8 | Migration, upsert/update, entry lookup, alias/title search, source state |
| internal/catalog/matcher_test.go | 6 | Steam/Lutris/executable/title matching and ToProfile |
| internal/catalog/images_test.go | 5 | Download validation, resolver modes, cache dir |
| internal/catalog/source_steam_test.go | 2 | ACF parser and SteamLocalSource refresh |
| internal/catalog/source_lutris_test.go | 5 | Lutris httptest refresh, MaxPages, offline skip, source-state errors |
| internal/catalog/source_desktop_test.go | 2 | Desktop parser/source refresh |
| internal/catalog/refresher_test.go | 5 | Start/Stop, Stop waits, BuildSources valid/unknown/defaults |
| internal/server/server_test.go | 8 | Catalog status/search/entry/refresh/profile-from-entry/missing query and CORS hostile/local origin checks |

**Still missing or shallow:**
- `cmd/picord/cli.go` has no mock HTTP tests.
- Default built-in profile enablement is not covered yet; current defaults appear disabled because embedded YAML omits `enabled: true`.
- Runtime tests cover initial disconnected desired storage, but not stale live Discord socket/restart replay.
- Socket discovery tests use regular files, not real stale-vs-valid Unix socket candidates.
- Refresher tests do not prove Stop cancels an in-flight immediate refresh.
- Browser UI behavior has no automated DOM tests.

---

## 9. Known Issues & Limitations

### CRITICAL: Built-in default profiles appear disabled
- `internal/profile/defaults.yaml` omits `enabled: true`.
- `DefaultProfiles()` sets `isDefault=true` and default priority, but does not set `Enabled=true`.
- `MergeDefaults()` only appends defaults when `p.Enabled` is true.
- Impact: the advertised 48 built-in profiles may not be active in default runtime.
- **Fix:** add tests, then default embedded profiles to enabled while preserving user same-name `enabled:false` overrides.

### CRITICAL/PARTIAL: RPC reconnect and replay still need stale-socket hardening
- Kimi fixed the initial disconnected path: `setRichPresence()` now calls `rm.setActivity(activity)`, which records `desiredActivity` before returning `not connected`.
- Remaining gap: live Discord restart/stale sockets. `IsConnected()` only checks local fields, `sendCommand()` does not mark the client closed on EOF, and reconnect/replay can duplicate sends or ignore replay errors.
- Unit tests should isolate `DISCORD_IPC_PATH` / `XDG_RUNTIME_DIR` so they never touch a real Discord socket.
- **Fix:** mark clients closed on I/O failure, reconnect on stale client, replay exactly once, preserve desired state on replay failure.

### CRITICAL/PARTIAL: Local write API needs real unsafe-method protection
- Hostile `Origin` unsafe requests are now rejected.
- Remaining gaps: missing `Origin` is trusted, there is no local token/session, JSON mutators do not consistently require `Content-Type: application/json`, and forbidden errors are not consistently JSON.
- **Fix:** define one local write-safety model, update CLI/web UI to use it, require JSON content type for JSON mutators, and table-test all unsafe endpoints.

### CRITICAL: Needs live Discord validation
- rpc/client.go has mock Unix socket coverage for handshake, SET_ACTIVITY, reconnect, and close.
- It still has not connected to a live Discord client in this repo.
- Handshake expects cmd:"DISPATCH" + evt:"READY". Real Discord may differ.
- sendCommand() reads exactly ONE response frame. Unsolicited Discord events could break it.
- **Fix if real Discord exposes this:** Add a background frame reader goroutine that routes responses by nonce and handles unsolicited events separately.

### CRITICAL: Discord image mode unvalidated
- `images.external_validated` now gates external URL image mode.
- A `picord debug-rpc-image` CLI command exists to test asset keys and external URLs against a live Discord client.
- External URL mode has NOT been validated against a real Discord client yet.
- Until validated, `images.mode` defaults to `generic`; do not recommend `external_url` for users.
- **Validation needed:** Run `picord debug-rpc-image --app-id <ID> --external-url https://cdn.akamai.steamstatic.com/steam/apps/620/header.jpg` with Discord running and observe if the image appears.

### MEDIUM: Profile edit/default-copy/rename lifecycle is incomplete
- Existing edit no longer clears `Enabled`, but the edit modal allows changing `name` while the server overwrites it from the URL path, so rename is silently ignored.
- Disabled custom profiles can be skipped by merge paths and may not round-trip through config/API saves.
- Once built-in defaults are enabled, copying a default with the same name may preserve `isDefault` and remain hidden from user-profile persistence.
- **Fix:** make rename read-only or implement real rename; preserve disabled user profiles; make default-copy behavior explicit and persistent.

### MEDIUM: Catalog/profile ranking is still heuristic
- Kimi added score-based ranking between profile and catalog candidates.
- Remaining gap: it does not distinguish user profiles from built-in defaults or broad launchers from exact app/game defaults.
- **Fix:** introduce explicit candidate metadata and table tests for user/default/catalog/broad launcher cases.

### MEDIUM: Config reload is partial and race-prone
- Watcher, GUI reload, and tray reload do not apply the same runtime updates.
- Reload uses `MergeUser`, so deleted profiles can remain active.
- Image resolver, catalog matcher/refresher, app_id, poll interval, scan mode, and web port semantics are not consistently updated or documented as restart-only.
- **Fix:** centralize reload through a synchronized runtime-state owner and use `ReplaceUser` for file reloads.

### MEDIUM: Catalog-created profiles may not match real games
- `POST /api/catalog/profiles/from-entry/:id` creates a `process_name` profile from `NormalizeTitle(entry.Title)`.
- `process_name` matching is exact executable equality, so Steam/Lutris/desktop catalog profiles often will not match the actual process.
- **Fix:** add source-aware match types or generate profiles from high-confidence aliases such as Steam AppID, Lutris slug, desktop ID, or executable aliases.

### MEDIUM: CLI HTTP-contract gaps
- CLI commands still lack mock HTTP tests.
- Some commands can print HTTP errors but return exit code 0; path parameters and multi-word search queries need stronger escaping/joining tests.

### MEDIUM: Catalog refresher/manual refresh lifecycle
- Immediate background refresh is not owned/cancelled like the main loop.
- Manual and background refresh can overlap.
- Opt-in `lutris_public` can still be expensive if invoked with a large page count.
- **Fix:** singleflight/queue per source, cancel/wait in Stop, and use safe caps for network refreshes.

### RESOLVED: Pre-emptive process matching
- `scan_all_processes` now defaults to true and scans every numeric `/proc/<pid>` entry.
- `scan_all_processes: false` keeps the legacy Discord-IPC-only scan for narrower detection.

### RESOLVED: Profile manager stale pointers
- `byName` is now `map[string]int` (index into slice), rebuilt after every `sortByPriority()` and `Delete()`.

### RESOLVED: Noisy monitor logging
- Per-process and summary logs now require `monitor.SetDebug(true)`.

### MEDIUM: Regex compiled on every match
- matcher.go compiles regex profiles on every scan.
- **Fix:** cache compiled regexes in profile.Manager.

### MEDIUM: Tray on pure GNOME Wayland
- GNOME needs the AppIndicator/KStatusNotifierItem extension for D-Bus SNI.
- **Workaround:** Web GUI at localhost:17970 has full functionality.

### LOW: KDE window fallback unimplemented
- window_linux.go:240 — getKDEDBusWindows() is a stub.

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
| CLI commands | ⚠️ Usable, but HTTP failures can still return exit code 0; no CLI tests yet |
| Debug logging | ✅ --debug flag |
| Config load/save/reload | ⚠️ Reload only partially applies runtime changes; use unified reload/runtime-state plan next |
| Web GUI | ⚠️ Dynamic XSS mostly fixed; profile edit preserves Enabled but rename/default-copy/disabled-profile lifecycle remains incomplete; inline handlers block strict CSP |
| Local API writes | ⚠️ Hostile origins rejected, but token/content-type/missing-origin/JSON-error protection remains |
| Built-in defaults | ❌ Intended 48 profiles appear disabled/not merged because embedded defaults omit `enabled: true` |
| Profile matching | ✅ process_name, window_title, regex match types work when profiles are loaded |
| Catalog auto-detection | ✅ Catalog-only fallback now iterates detected processes after profile miss |
| Catalog/profile ranking | ⚠️ Score-based ranking exists, but user/default/broad-launcher metadata is still missing |
| Template variables | ✅ {process_name}, {window_title}, {title}, {source}, {steam_app_id} |
| Window title detection | ✅ Hyprland, Sway, X11, KDE best-effort |
| Auto-reconnect | ⚠️ Initial disconnected desired storage fixed; stale live Discord socket/restart replay still needs hardening |
| System tray | ⚠️ Depends on compositor SNI support |
| Actual Discord RPC | ⚠️ Mock socket covered; needs live Discord validation and nonce/background reader hardening |
| Pre-emptive matching | ✅ scan_all_processes default covers ordinary apps/games |
| Catalog refresh defaults | ✅ New defaults use implemented sources (`steam_local`, `desktop`) |
| Catalog/refresher lifecycle | ⚠️ Immediate refresh goroutine/manual refresh overlap still need cancellation/singleflight hardening |
| Repository hygiene | ✅ Root `picord` binary removed; `/picord` ignored |
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
2. ✅ Fix initial Discord startup-order client creation: replaced raw `*rpc.Client` with `rpcManager` wrapper that can `connect()` when nil. Background goroutine now creates a client if Discord starts after Picord. Initial disconnected desired storage was fixed later; stale live-socket/restart behavior remains tracked in Phase 7.
3. ✅ Gate noisy monitor logging: per-process and summary logs now require `monitor.SetDebug(true)`.

### Phase 4. Rich game catalog foundation (implemented by Kimi)
Kimi implemented the first catalog foundation pass after the original plan in `docs/plans/2026-04-29-kimi-rich-game-catalog.md`:

1. SQLite catalog store and schema.
2. Steam local, Lutris public, and desktop source adapters.
3. Catalog matcher and image resolver/cache helpers.
4. Catalog HTTP endpoints, CLI commands, and web UI search/suggestions.
5. Background refresher and tests.

### Phase 5. Post-Kimi stabilization pass (implemented by Kimi)
Kimi's second pass addressed the previous P0 catalog/runtime items from `docs/plans/2026-04-29-post-kimi-stabilization.md`:

1. ✅ Removed tracked root `picord` binary and ignored `/picord`.
2. ✅ Fixed default catalog sources to implemented adapters (`steam_local`, `desktop`).
3. ✅ Activated catalog-only fallback by iterating detected processes after profile miss.
4. ✅ Added catalog API DTOs with snake_case JSON consumed by the web UI.
5. ✅ Added `rpcManager.desiredActivity` replay after reconnect when an activity was already recorded.
6. ✅ Improved status privacy and dynamic UI escaping.

### Phase 6. Follow-up stabilization pass (partially implemented by Kimi)
Kimi's third pass addressed part of `docs/plans/2026-04-29-post-kimi-followup-stabilization.md`:

1. ✅ Fixed initial disconnected desired storage: `setRichPresence()` now calls `rm.setActivity()` so desired activity can be saved while disconnected.
2. ✅ Added basic hostile-Origin rejection for unsafe HTTP methods.
3. ✅ Preserved existing profile `Enabled` state during `Manager.Add()` updates.
4. ✅ Added score-based catalog/profile candidate ranking.
5. ✅ Gated `external_url` image mode behind `images.external_validated`.
6. ✅ Added/expanded tests; current audit saw 110 test functions/benchmarks across 17 test files.
7. ⚠️ Remaining gaps: built-in defaults appear disabled, RPC stale-socket/restart replay is incomplete, write protection lacks token/content-type/missing-origin handling, reload semantics are partial, profile rename/default-copy behavior remains incomplete.

### Phase 7. Third-pass stabilization (next)
Read `docs/plans/2026-04-29-post-kimi-third-pass-stabilization.md`. Do these before provider/image expansion:

1. Restore built-in default profiles by defaulting embedded defaults to `Enabled=true` while preserving explicit user disables.
2. Harden RPC stale connection detection, reconnect, and exactly-once desired activity replay after Discord restart.
3. Finish local write API protection with a token/same-origin policy, JSON content-type checks, and JSON error bodies.
4. Centralize runtime config reload semantics and use `ReplaceUser` for file reloads.
5. Finish profile edit/rename/default-copy/disabled-profile lifecycle.
6. Replace heuristic catalog/profile ranking with explicit candidate metadata.
7. Add source-aware match types or alias-driven profile generation for catalog-created profiles.
8. Refactor catalog refresher/manual refresh around cancellation and per-source singleflight.
9. Add CLI HTTP-contract tests and fix nonzero exit behavior.
10. Keep image URLs conservative until live Discord validation succeeds.

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
2. Read `docs/plans/2026-04-29-post-kimi-third-pass-stabilization.md`.
3. Run `go test -count=1 ./...`, `go vet ./...`, `go test -race ./...`, `make build`, and `git diff --check`.
4. Start with Phase 7 item 1: restore built-in default profiles with tests, then RPC stale-socket/reconnect replay, then local write protection.
5. Make minimal changes. Add tests first for regressions.
6. Commit each logical fix and push.
7. Update this HANDOFF.md if architecture or actual status changes significantly.

*End of handoff.*
