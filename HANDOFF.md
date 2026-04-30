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
- Local branch is ahead of `origin/master` by 2 commits at this handoff:
  - `bdc40e4 fix: make tray autostart explicit and remove web UI`
  - `686999d docs: remove web UI references`

Push after the next successful implementation batch, unless the user asks otherwise.

## What changed in the latest iteration

### Tray startup and Web UI removal

- `picord run` now supports tray flags:
  - `picord run --tray`
  - `picord run --no-tray`
- No-argument `picord` still runs the daemon using config defaults.
- `resources/picord.desktop` now uses `Exec=picord run` and includes `X-GNOME-Autostart-Delay=5`.
- Tray menu no longer includes `Open Web GUI`.
- Removed Web UI code and assets:
  - `internal/server/web/index.html`
  - `internal/server/web/css/style.css`
  - `internal/server/web/js/api.js`
  - `internal/server/web/js/app.js`
- `internal/server/server.go` no longer embeds or serves browser UI assets.
- Root HTTP path `/` now returns JSON 404: `Picord API: use /api/status or the picord CLI`.
- Server startup log now says `Picord API: http://...`, not Web GUI.
- README now describes GTK Settings/tray/CLI/YAML workflows instead of a browser Web UI.

### Important caveat

`resources/picord.service` and the GTK Settings dialog startup toggle still need follow-up work. The dialog currently labels startup as `Launch on login (systemd service)` and manages `systemctl --user enable/disable picord.service`. That may still be the cause of the boot-time no-tray behavior because systemd user services can run without a ready StatusNotifier/tray environment.

The next plan prioritizes fixing that.

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

## Next iteration plan for DeepSeek V4 Pro

Plan file:

- `docs/plans/2026-04-30-deepseek-v4-pro-next-iteration.md`

Highest-priority items:

1. Fix startup mechanism so login startup reliably creates a tray icon in a graphical desktop session.
2. Prevent the Settings UI/config from accidentally disabling the only graphical configuration path.
3. Add tests for `picord run --tray` / `--no-tray` behavior.
4. Keep removing stale Web UI wording and add a test that `/` does not serve HTML.

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

1. **Boot-time tray reliability**
   - Desktop autostart was improved, but `resources/picord.service` and the Settings dialog still need alignment.
   - Prefer desktop autostart for graphical tray access unless a systemd user unit is proven reliable in the user's session.

2. **Tray can still be disabled by config**
   - With Web UI removed, disabling tray removes the only graphical settings path.
   - Next iteration should make this safer or advanced-only.

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
