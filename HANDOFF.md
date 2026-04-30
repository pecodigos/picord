# Picord handoff

Updated: 2026-04-30

## Current focus

Post-stabilization feature hardening. Recent work focused on:

1. Hardening browser/app exclusion to prevent false-positive tracking
2. Performance improvements (regex caching)
3. Emulator game title extraction for better catalog matching
4. SteamGridDB integration for non-Steam game artwork
5. Profile rename support in the web UI

Picord is still targeting the user's Discord application:

- App name: Picord
- Application ID: 1499058229571752148

## Current branch state

- Repository: `/mnt/hdd/Code/2026/picord`
- Branch: `master`

## What is now implemented

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
- New `SteamGridDBClient` with search + grid endpoints.
- `Enricher` component queries SGDB for entries missing `image_url`.
- `catalog enrich` CLI command: `picord catalog enrich [--batch-size N]`.
- Server endpoint `POST /api/catalog/enrich` returns `{status, enriched, enabled, message}`.
- Background enrichment runs automatically after each catalog refresh cycle when `steamgriddb_api_key` is configured.
- Config field: `catalog.steamgriddb_api_key` (optional).
- New store methods: `EntriesMissingImages(limit)` and `UpdateEntryImage(id, url, kind)`.
- Enricher tests with mock SGDB server.

### Profiles
- Profile/catalog matching aware of aliases.
- Blank match value guardrails: empty/whitespace match values are rejected.
- Stable tie-breaking: priority → match type specificity → value length → profile index.
- Regex compilation caching via `Profile.regexCache`.
- `isExcludedApp()` blocks browsers, Discord, file managers, and desktop noise.
- Browser exclusion covers: exact names, `firefox-bin`, flatpak IDs, `xdg-*` prefix, `*-settings`/`*-config` suffix.
- **Profile rename support**: `PUT /api/profiles/{old-name}` with body containing `name: "new-name"` deletes the old profile and creates the new one.
- Server test `TestHandleProfileByID_Rename` verifies rename behavior.

### Emulator game titles
- `ExtractEmulatorGameTitle(processName, windowTitle)` parses known emulator window title formats:
  - DuckStation `[game]`
  - PCSX2 `[game]`
  - Dolphin `| game`
  - Cemu `Cemu - game`
  - Yuzu/Ryujinx `game - Emulator`
  - RetroArch `| game`
  - melonDS, mGBA, Snes9x, DeSmuME, PPSSPP `game - Emulator`
  - RPCS3 `[game]`
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
- `apiGet` sends `X-Picord-Token` for CSRF protection.
- Token read from `~/.local/state/picord/api-token`.
- Status checks non-200 responses.
- `catalog search` joins multi-word queries.

### Window titles
- `GetWindowTitles()` supports Hyprland, Sway, X11 (wmctrl/xdotool), and KDE (kdotool/qdbus).
- KDE D-Bus fallback `getKDEDBusWindows()` implemented using KWin `clientList` interface.

### Privacy & security
- Full process environment never exposed; only allowlisted env keys.
- Default `/api/status` does not include aliases, Steam app ID, or desktop ID.
- Verbose status includes identity fields but never exe path, cwd, args, or env.
- `debug-processes --json` emits sanitized DTOs.
- `debug-processes --name` searches aliases, window title, Steam AppID, desktop ID.

## Validation status

Latest validation passed:

```bash
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```

`make build` produced `bin/picord`.

## Main remaining risks / next steps

1. **Emulator title extraction live validation**
   - Needs real-world testing with actual emulators to verify window title formats match.
   - Some emulators (RetroArch) have configurable title bars; default may not include ROM name.

2. **SteamGridDB rate limits**
   - Free tier has rate limits (~20 requests/second). The enricher has a 200ms delay between requests but may still hit limits on large catalogs.
   - Consider adding exponential backoff or caching search results.

3. **KDE window title reliability**
   - `kdotool` is only available on KDE 6+. The D-Bus fallback depends on `qdbus` being installed.
   - May need `dbus-send` fallback for systems without `qdbus`.

4. **Profile copy from default**
   - Copying a default profile silently overwrites an existing user profile with the same name. Could be confusing.
   - Consider prompting for confirmation in the UI.

5. **Wayland-native window title backends**
   - Generic Wayland protocol doesn't support window enumeration; each compositor is different.
   - Could add GNOME D-Bus (`org.gnome.Shell`) or wlroots-based (`wlr-foreign-toplevel-management`) approaches.

## Manual user-testing path

After building:

```bash
make build
```

Refresh local catalog data:

```bash
bin/picord catalog refresh --source steam_local
bin/picord catalog refresh --source steam_shortcuts
bin/picord catalog refresh --source desktop
```

Enrich missing artwork (requires SteamGridDB API key in config):

```bash
bin/picord catalog enrich --batch-size 50
```

Launch a Steam, non-Steam, Wine, Proton, or emulator game, then inspect:

```bash
bin/picord status --verbose
bin/picord status --json
bin/picord debug-processes --wine --with-aliases
bin/picord debug-processes --name "<game>" --json
```

Expected healthy signs:

- Last Scan is recent.
- Steam/Proton game has a Steam App ID.
- Wine/Proton carrier has a game basename alias, e.g. `Lethal Company.exe` and `Lethal Company`.
- Emulator games show extracted title in aliases (when window title includes game name).
- No browser/Discord/file manager processes in detected list.
- Debug JSON does not include `exe_path`, `cwd`, `args`, env values, or private full paths.

## Do not lose

- The public Picord Discord application ID is `1499058229571752148`.
- Do not preserve credentials, tokens, passwords, or secrets in docs/logs.
- SteamGridDB API key should be stored in config under `catalog.steamgriddb_api_key`.
- Keep committing and pushing every completed change.
