# Picord handoff

Updated: 2026-05-01

## Current focus

Tray-first Picord hardening after the browser Web UI removal. The immediate user-reported issue was that the system tray did not initialize with the system, leaving no graphical access to configuration after boot.

Picord is still targeting the user's Discord application:

- App name: Picord
- Application ID: 1499058229571752148

## Current branch state

- Repository: `/mnt/hdd/Code/2026/picord`
- Branch: `master`
- Local branch is ahead of `origin/master` by 20 commits at this handoff.
- Latest commit: `6261a23 fix: embed local icons as base64 data URIs instead of localhost HTTP`

Push after the next successful implementation batch, unless the user asks otherwise.

## What changed in the latest iteration (2026-05-01 DeepSeek V4 Pro)

### Hyprland IPC rate-limiting
- `GetWindowTitles()` now caches results for 10 seconds to avoid calling `hyprctl clients -j` on every poll cycle (was 30x/min at default 2s interval).
- All compositor command invocations (`hyprctl`, `swaymsg`, `wmctrl`, `kdotool`, `qdbus`, `xdotool`) now have a 5-second context timeout via `execCommand()` helper.
- `GetWindowTitles()` errors are now logged instead of silently discarded in `resolveIdentitiesFromTable`.

### Tray icon at boot (two-part fix)
- **DBus wait**: `waitForTrayHost()` in `internal/tray/tray.go` polls the D-Bus session bus for `org.kde.StatusNotifierWatcher` for up to 60s before calling `systray.Run()`.
- **Systray library patch**: Two upstream bugs in `energye/systray@v1.0.3` prevented the tray icon from appearing on waybar/Hyprland:
  1. `RegisterStatusNotifierItem` passed the object path (`/StatusNotifierItem`) instead of the service name (`org.kde.StatusNotifierItem-{pid}-1`). waybar/Hyprland reject the malformed argument.
  2. `systrayReady()` (which fires `onReady`) was called **before** DBus properties and connection were set up, so `SetIcon()`/`SetTitle()`/`SetTooltip()` silently failed (`instance.props == nil`).
- Patched copy of the library stored at `local/energye/systray/` with a `replace` directive in `go.mod`.
- Desktop autostart entry (`resources/picord.desktop` and settings dialog) now uses `os.Executable()` for the binary path + explicit `--tray` flag.

### Process exclusion: utility app filtering
- Added `isDesktopUtility()` in `internal/catalog/source_desktop.go` that classifies `.desktop` entries by their `Categories` field.
- Apps with primary category `Utility`, `System`, `Settings`, `TerminalEmulator`, `FileTools`, etc. are excluded from the catalog.
- Subcategories like `;screenshot;`, `;settings;`, `;terminalemulator;` are filtered even when secondary.
- Added screenshot/audio tools (Flameshot, ksnip, spectacle, pavucontrol, gnome-screenshot) to all three exclusion lists: `isExcludedApp()`, `isExcludedDesktopApp()`, and `isExcludedCatalogEntry()`.

### Desktop icon images via base64 data URIs
- Added `internal/iconfinder/` package that resolves `.desktop` `Icon=` field to actual files via XDG icon theme directory search (`/usr/share/icons/hicolor/*/apps/`, `/usr/share/pixmaps/`).
- `DesktopSource` stores resolved icon paths in `image_url` as `localicon:{sha256}` with a SHA-256 hash→path registry.
- `ImageResolver` now embeds the actual PNG file as a `data:image/png;base64,...` URI in the RPC payload.
- Added `/assets/picord-icons/{hash}` HTTP endpoint (serves local icon files — fallback for debugging).
- Added `handleLocalIcon` accepts HEAD + GET.
- Removed the `simpleicons.org` CDN (returned SVG, Discord can't render) and broken GitHub raw URLs for emulator icons.
- Apps without desktop icons fall back to the `picord` generic asset key.

### Presence display improvements
- Added `Name` field to `RichActivity` struct. Discord RPC's `name` field overrides the Discord Application name in the display, so it now shows "DuckStation" or "GIMP" instead of always "Picord".
- `ToProfile()` no longer duplicates the title in the `Details` field. Only `State: "Playing now"` is shown.
- DuckStation profile uses regex match `(?i)duckstation` (substring, case-insensitive) to handle AppImage process names like `DuckStation-x64.AppImage`.

### Auto-detect toggle fix
- When auto-detect is toggled OFF, `currentProfile` is now reset to `nil` and `state.ClearActive()` is called. Previously, re-enabling auto-detect would skip `setRichPresence()` because the callback compared `currentProfile.Name == winner.Profile.Name` and found them equal.
- `OnAutoDetectSet` now calls `procMonitor.ForceScan()` (new method on `Monitor`) when auto-detect is re-enabled, triggering an immediate process scan instead of waiting for the next ticker interval.
- Fixed `auto-detect` CLI: was sending `POST /api/settings` but the endpoint only accepts `PUT`. Added `apiPut()` helper via shared `apiMethod()`.

## What changed in the previous iteration (2026-04-30 DeepSeek V4 Pro)

### Startup mechanism (P0.1)
- GTK Settings dialog now manages `~/.config/autostart/picord.desktop` (desktop autostart) instead of `systemctl --user enable picord.service`.
- Checkbox label changed from "Launch on login (systemd service)" → "Launch on login (desktop autostart)".
- Desktop entry content is embedded in the settings package as a constant.
- `resources/picord.service` updated: uses `picord run --tray`, removed brittle `DISPLAY`/`WAYLAND_DISPLAY` env assumptions. Marked as advanced/headless in README.

### Tray access safety (P0.2)
- Removed "Show icon in system tray" checkbox from Settings dialog (was the only graphical config path after Web UI removal).
- Replaced with explanatory label: "System tray icon is always shown. Headless mode: picord run --no-tray".
- Config field `show_tray_icon` preserved for backward compatibility.
- Added config tests for both legacy `show_tray_icon: false` and default `show_tray_icon: true`.

### Tray flag testing (P1.1)
- Extracted `parseRunFlags()` helper from `cmdRun()` for testability.
- Added 5 tests covering: default, `--tray`, `--no-tray`, `--no-tray` overriding `--tray`, and `--tray=false`.

### Port naming (P1.2)
- Renamed config key `web_port` → `api_port` (yaml + json tags).
- Backward compat in `Load()`: old `web_port` key is mapped to the new `api_port` field when no `api_port` is present.
- `api_port` takes precedence when both keys exist.
- Updated log messages and README references.
- Added backward-compat and precedence config tests.

### Web UI removal tests (P1.3)
- Added `TestRootReturnsJSON404NotHTML`: asserts `GET /` returns HTTP 404, `Content-Type: application/json`, and body contains no HTML tags.
- Cleaned up "browser-facing" comment in settings API code.

### Documentation + CLI parity (P2)
- README autostart section: desktop autostart moved to recommended position, systemd marked as advanced/headless, verification steps added.
- Added `picord auto-detect [on|off]` CLI command (fills the only parity gap with removed Web UI).

## Current implementation snapshot

### Core identity & Wine/Proton
- `/proc` process table support with PID, PPID, PGID, SID, exe path, cwd, cmdline args, allowlisted env hints, and window titles.
- Steam AppID extraction from args and allowlisted env (digit-only validation).
- `DetectedProcess.SteamAppID` propagation through the resolver.
- Wine/Proton/Steam carrier classification and alias enrichment from related processes.
- Relationship gating for same-PGID peers behind Wine/Proton/Steam shared clues.
- SID peers disabled by default.
- Windows path alias normalization using basenames only.
- Shell ancestor blocking (`isCommonShell`) and gaming ancestor gating (`isGamingAncestor`).

### Catalog & matching
- Catalog matching over all candidates with confidence and source-priority tie-breaking.
- Effective confidence = `min(methodConfidence, aliasConfidence)`.
- `reasonPriority()` for stable reason ranking.
- Alias confidence from database incorporated into scoring.
- `steam_shortcut` source gets Steam-like tie priority.
- Catalog ambiguity fix: appends all entries from alias/title searches.
- **Category-based utility filtering**: Desktop entries with `Categories` containing `Utility`, `System`, `Settings`, etc. are excluded from the catalog. Only games and work/creative tools (Graphics, AudioVideo, Development, Office, Engineering) are included.

### Image system
- **SteamGridDB enrichment**: `SteamGridDBClient` with search + grid endpoints. Enricher + background enrichment on refresh.
- **Desktop icon resolution**: `iconfinder` package resolves `.desktop` `Icon=` field to actual files via XDG icon theme search.
- **Base64 data URI embedding**: Local PNG icons are base64-encoded and embedded directly in the RPC payload as `data:image/png;base64,...` URIs. No HTTP fetch needed by Discord.
- **ImageResolver pipeline**: `external_url` mode → entry's HTTP URL → localicon data URI → profile `large_image` → Discord asset key → generic asset key.
- Config fields: `images.mode`, `images.generic_asset_key`, `images.external_validated`, `catalog.steamgriddb_api_key`.

### Profiles
- Profile/catalog matching aware of aliases.
- Blank match value guardrails: empty/whitespace match values are rejected.
- Stable tie-breaking: priority → match type specificity → value length → profile index.
- Regex compilation caching via `Profile.regexCache`.
- `isExcludedApp()` blocks browsers, Discord, file managers, launchers, terminals, shells, and screenshot/audio utility tools.
- **Regex profile support**: DuckStation profile uses `(?i)duckstation` regex to match AppImage process names.
- Profile rename support: `PUT /api/profiles/{old-name}` with body containing `name: "new-name"` deletes the old profile and creates the new one.

### Emulator game titles
- `ExtractEmulatorGameTitle(processName, windowTitle)` parses known emulator window title formats.
- Extracted titles are added as aliases, allowing catalog matching of actual ROMs/games.
- Emulator profile priorities lowered from 10 → 5 so catalog matches can win when the game is known.

### Discord RPC
- `RichActivity.Name` field set to the profile/catalog entry name, overriding the app name in Discord's UI.
- Auto-reconnect with activity replay, idle presence, app switching.

### Scanning & status
- `scan_all_processes = false` candidate-first scanning with lite table + env-only peer probe.
- Atomic scan snapshots (`ScanSnapshot` with mode and state).
- Match diagnostics in verbose status: source, profile name, process name, reason, confidence, Discord app ID, RPC connected state.
- `Monitor.ForceScan()` triggers an immediate scan + callback invocation when auto-detect is re-enabled.
- `picord status --verbose` shows match info.
- `picord status --json` outputs raw formatted JSON.
- `picord debug-processes` filters: `--wine`, `--proton`, `--with-aliases`, `--name`, `--pid`, `--json`.

### Tray & auto-detect
- **Systray patched** (see above): service name fix + onReady ordering fix in `local/energye/systray/systray_unix.go`.
- **Tray host wait**: `waitForTrayHost()` polls DBus for `StatusNotifierWatcher` before `systray.Run()`.
- **Auto-detect toggle**: `currentProfile` reset on disable, `ForceScan()` on re-enable.
- **CLI fix**: `auto-detect on/off` uses PUT (not POST) to `/api/settings`. Added `apiPut()` + `apiMethod()` shared helpers.

### CLI/API robustness
- `apiGet`/`apiPost`/`apiPut`/`apiDelete`/`apiMethod` helpers with `X-Picord-Token` for CSRF/token protection.
- Token read from `~/.local/state/picord/api-token`.
- Status checks non-200 responses.
- `catalog search` joins multi-word queries.
- Localhost API remains for CLI/tray integration, but there is no browser Web UI.

### Window titles
- `GetWindowTitles()` supports Hyprland, Sway, X11 (wmctrl/xdotool), and KDE (kdotool/qdbus).
- KDE D-Bus fallback `getKDEDBusWindows()` implemented using KWin `clientList` interface.
- **Rate-limited**: cached for 10s, all compositor commands have 5s timeout.

### Privacy & security
- Full process environment never exposed; only allowlisted env keys.
- Default `/api/status` does not include aliases, Steam app ID, or desktop ID.
- Verbose status includes identity fields but never exe path, cwd, args, or env.
- `debug-processes --json` emits sanitized DTOs.

## Validation status

Latest validation passed after all changes:

```bash
go test -count=1 ./...
go vet ./...
make build
```

All 9 packages pass (2 without test files: `settings`, `tray`).

## Manual user-testing path

After building:

```bash
make build
```

Install/refresh desktop autostart manually for a real-session check:

```bash
mkdir -p ~/.config/autostart ~/.local/share/icons/hicolor/128x128/apps
cp resources/picord.desktop ~/.config/autostart/picord.desktop
cp icons/picord_128.png ~/.local/share/icons/hicolor/128x128/apps/picord.png
```

Then log out/in or reboot. Expected:

- Picord process is running: `pgrep -a picord`.
- A Picord tray icon is visible.
- Right-click menu includes Settings, Auto-Detect, Manual Override, Reload Config, Quit.
- Right-click menu does not include Open Web GUI.
- Settings opens from the tray.
- Auto-Detect toggle off→on works correctly.

For catalog/runtime checks:

```bash
bin/picord catalog refresh --source steam_local
bin/picord catalog refresh --source steam_shortcuts
bin/picord catalog refresh --source desktop
bin/picord status --verbose
bin/picord status --json
bin/picord debug-processes --wine --with-aliases
bin/picord debug-processes --name "<game>" --json
```

## Main remaining risks / next steps

1. **Boot-time tray reliability** ✅ RESOLVED
   - Systray library patched (service name + onReady ordering).
   - `waitForTrayHost()` polls DBus for `StatusNotifierWatcher` before registration.
   - Settings dialog + desktop autostart handle startup correctly.

2. **Tray can still be disabled by config** ✅ RESOLVED
   - "Show icon in system tray" checkbox removed from Settings dialog.
   - Headless mode available via `picord run --no-tray` only.

3. **Auto-detect toggle regression** ✅ RESOLVED
   - `currentProfile` reset on disable, `ForceScan()` on re-enable.
   - CLI fixed (PUT instead of POST).

4. **Non-Steam app images**
   - Desktop icons are resolved to real files and embedded as base64 data URIs. Discord's acceptance of `data:` URIs in large_image is unverified — if Discord rejects them, apps show the generic Picord icon.
   - Apps without desktop `Icon=` entries (like AppImage-based emulators) fall back to the generic asset key.

5. **Emulator title extraction live validation**
   - Needs real-world testing with actual emulators to verify window title formats match.

6. **SteamGridDB rate limits**
   - Free tier has rate limits. The enricher has a 200ms delay between requests but may still hit limits on large catalogs.

7. **KDE window title reliability**
   - `kdotool` is only available on KDE 6+. D-Bus fallback depends on `qdbus` being installed.

8. **Wayland-native window title backends**
   - Generic Wayland protocol doesn't support window enumeration; each compositor is different.

9. **Vendored systray patch**
   - The patched `energye/systray@v1.0.3` is stored at `local/energye/systray/` with a `replace` directive in `go.mod`. If the upstream library is updated, the patch must be re-applied.

## Do not lose

- Public Picord Discord application ID: `1499058229571752148`.
- Do not preserve credentials, tokens, passwords, or secrets in docs/logs.
- SteamGridDB API key should be stored in config under `catalog.steamgriddb_api_key`.
- Keep committing and pushing every completed change.
- Do not reintroduce a browser Web UI unless the user explicitly asks for it.
- The patched systray library is at `local/energye/systray/` — the `replace` directive in `go.mod` uses it. When updating `energye/systray`, re-apply the two fixes in `systray_unix.go`:
  1. `RegisterStatusNotifierItem(0, name)` (not `path`)
  2. `systrayReady()` at end of `nativeStart()` (after DBus setup)
