# Picord handoff

Updated: 2026-04-30

## Current focus

Tray-first Picord hardening after the browser Web UI removal. The immediate user-reported issue was that the system tray did not initialize with the system, leaving no graphical access to configuration after boot.

Picord is still targeting the user's Discord application:

- App name: Picord
- Application ID: 1499058229571752148

## Current branch state

- Repository: `/mnt/hdd/Code/2026/picord`
- Branch: `master`
- Local branch is ahead of `origin/master` by 6 commits at this handoff:
  - `1b54cf3 fix: use desktop autostart for tray startup`
  - `911ebb1 fix: prevent settings from disabling tray access`
  - `760437f test: cover tray startup flags`
  - `55051fb refactor: rename web port to local api port`
  - `27f89de test: assert removed web ui stays removed`
  - `8aa35e5 docs: document tray-first startup workflow and add auto-detect CLI`

Push after the next successful implementation batch, unless the user asks otherwise.

## What changed in the latest iteration (2026-04-30 DeepSeek V4 Pro)

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

### SteamGridDB enrichment
- `SteamGridDBClient` with search + grid endpoints.
- `Enricher` component queries SGDB for entries missing `image_url`.
- `catalog enrich` CLI command: `picord catalog enrich [--batch-size N]`.
- Server endpoint `POST /api/catalog/enrich` returns `{status, enriched, enabled, message}`.
- Background enrichment runs automatically after each catalog refresh cycle when `steamgriddb_api_key` is configured.
- Config field: `catalog.steamgriddb_api_key` (optional).
- Store methods: `EntriesMissingImages(limit)` and `UpdateEntryImage(id, url, kind)`.

### Profiles
- Profile/catalog matching aware of aliases.
- Blank match value guardrails: empty/whitespace match values are rejected.
- Stable tie-breaking: priority → match type specificity → value length → profile index.
- Regex compilation caching via `Profile.regexCache`.
- `isExcludedApp()` blocks browsers, Discord, file managers, and desktop noise.
- Profile rename support: `PUT /api/profiles/{old-name}` with body containing `name: "new-name"` deletes the old profile and creates the new one.

### Emulator game titles
- `ExtractEmulatorGameTitle(processName, windowTitle)` parses known emulator window title formats.
- Extracted titles are added as aliases, allowing catalog matching of actual ROMs/games.
- Emulator profile priorities lowered from 10 → 5 so catalog matches can win when the game is known.

### Scanning & status
- `scan_all_processes = false` candidate-first scanning with lite table + env-only peer probe.
- Atomic scan snapshots (`ScanSnapshot` with mode and state).
- Match diagnostics in verbose status: source, profile name, process name, reason, confidence, Discord app ID, RPC connected state.
- `picord status --verbose` shows match info.
- `picord status --json` outputs raw formatted JSON.
- `picord debug-processes` filters: `--wine`, `--proton`, `--with-aliases`, `--name`, `--pid`, `--json`.

### CLI/API robustness
- `apiGet` sends `X-Picord-Token` for CSRF/token protection.
- Token read from `~/.local/state/picord/api-token`.
- Status checks non-200 responses.
- `catalog search` joins multi-word queries.
- Localhost API remains for CLI/tray integration, but there is no browser Web UI.

### Window titles
- `GetWindowTitles()` supports Hyprland, Sway, X11 (wmctrl/xdotool), and KDE (kdotool/qdbus).
- KDE D-Bus fallback `getKDEDBusWindows()` implemented using KWin `clientList` interface.

### Privacy & security
- Full process environment never exposed; only allowlisted env keys.
- Default `/api/status` does not include aliases, Steam app ID, or desktop ID.
- Verbose status includes identity fields but never exe path, cwd, args, or env.
- `debug-processes --json` emits sanitized DTOs.

## Validation status

Latest validation passed after Web UI removal and README update:

```bash
go test -count=1 ./...
go vet ./...
make build
```

`make build` produced ignored `bin/picord`.

## Completed plan

Plan file:

- `docs/plans/2026-04-30-deepseek-v4-pro-next-iteration.md` — **IMPLEMENTED**

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
   - Settings dialog now manages desktop autostart (`~/.config/autostart/picord.desktop`).
   - `resources/picord.service` updated to use `picord run --tray` without brittle env assumptions.
   - README positions desktop autostart as recommended for tray access.

2. **Tray can still be disabled by config** ✅ RESOLVED
   - "Show icon in system tray" checkbox removed from Settings dialog.
   - Headless mode available via `picord run --no-tray` only.

3. **Emulator title extraction live validation**
   - Needs real-world testing with actual emulators to verify window title formats match.

4. **SteamGridDB rate limits**
   - Free tier has rate limits. The enricher has a 200ms delay between requests but may still hit limits on large catalogs.

5. **KDE window title reliability**
   - `kdotool` is only available on KDE 6+. D-Bus fallback depends on `qdbus` being installed.

6. **Wayland-native window title backends**
   - Generic Wayland protocol doesn't support window enumeration; each compositor is different.

## Do not lose

- Public Picord Discord application ID: `1499058229571752148`.
- Do not preserve credentials, tokens, passwords, or secrets in docs/logs.
- SteamGridDB API key should be stored in config under `catalog.steamgriddb_api_key`.
- Keep committing and pushing every completed change.
- Do not reintroduce a browser Web UI unless the user explicitly asks for it.
