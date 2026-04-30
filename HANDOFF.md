# Picord HANDOFF

## Current focus

Kimi's latest iteration added non-Steam Steam shortcut catalog import. The next engineering focus is Wine/Proton process identity so non-Steam games launched through Steam still resolve when Linux only exposes generic carrier processes like `wine`, `wineserver`, `wine-preloader`, `proton`, or `pressure-vessel-*`.

User context:

- Project path: `/mnt/hdd/Code/2026/picord`
- Discord application name: `Picord`
- Discord application ID: `1499058229571752148`
- User wants to begin real self-testing by launching Picord and games locally.

## Latest completed work in this session

1. Reviewed Kimi's iteration.
2. Committed Kimi's non-Steam shortcut import as:
   - `7fdad76 feat: import Steam non-Steam shortcuts`
3. Removed an untracked throwaway `assets/test.png` file from the working tree.
4. Added Picord's real Discord application ID as the ready-to-run default:
   - exported `config.DefaultDiscordAppID`
   - default `app_id` is now `1499058229571752148`
   - generated configs include `discord_apps.main` for Picord
   - old generated configs with empty `app_id` now fall back to Picord's default app
   - configs that only define `discord_apps.main` backfill the legacy `app_id` used by the daemon
   - daemon now uses `cfg.ResolveDiscordApp("main")` when connecting to Discord
   - `picord debug-rpc-image` now uses the config/default app id when `--app-id` is omitted
5. Added tests for the app ID/default-config behavior.
6. Added plan:
   - `docs/plans/2026-04-30-user-testing-and-wine-proton-detection.md`

## Kimi iteration assessment

What is good:

- Binary VDF parsing for Steam `shortcuts.vdf` is in place.
- `SteamShortcutsSource` imports non-Steam shortcuts from Steam userdata dirs.
- Default catalog sources now include `steam_shortcuts` alongside `steam_local` and `desktop`.
- Shortcut entries get useful aliases:
  - generated Steam shortcut app id
  - normalized title
  - executable basename with `.exe`
  - executable basename without `.exe`
- This is the correct foundation for non-Steam detection.

Important limitation:

- Current `/proc` scanning still treats each observed Linux process mostly independently.
- If the visible process is `wine`/`wineserver`/`wine-preloader` instead of the game `.exe`, the matcher may never see the aliases Kimi imported.
- Parent/child walking alone is not enough because Proton/Wine process relationships can be siblings, descendants, process-group peers, session peers, or reparented processes.

## Best Wine/Proton idea

Implement a process-identity resolver, not a narrow "Wine child process detector".

The resolver should build one `/proc` table per scan, then enrich generic Wine/Proton carrier processes with safe aliases discovered from related processes and launch hints.

Priority signals:

1. Steam app id hints from args/env:
   - `SteamAppId`
   - `SteamGameId`
   - `SteamAppID`
   - `SteamOverlayGameId`
   - `SteamCompatAppId`
2. Steam/Proton/Wine env hints:
   - `PROTON_COMPAT_DATA_PATH`
   - `STEAM_COMPAT_DATA_PATH`
   - `WINEPREFIX`
   - `WINELOADER`
3. Process-group/session relationships around generic Wine/Proton processes.
4. Related Windows `.exe` names from process name, `/proc/<pid>/exe`, and argv tokens.
5. Steam shortcut aliases imported from `shortcuts.vdf`.
6. Window title only as fallback/disambiguation.

Do not let `wine` itself become the game identity. Keep the observed name for debugging and add aliases such as `Lethal Company.exe` / `Lethal Company` for matching.

## Next implementation plan

Follow the detailed plan in:

- `docs/plans/2026-04-30-user-testing-and-wine-proton-detection.md`

High-level phases:

1. Add monitor process table foundation:
   - parse PID, PPID, process group, session, name, exe, cwd, cmdline, allowlisted env hints
   - build parent/child indexes once per scan
2. Add Wine/Proton alias enrichment:
   - identify generic carrier processes
   - collect ancestors, descendants, and safe process-group/session peers
   - extract non-generic `.exe` aliases and Steam app id hints
3. Integrate aliases into profile and catalog matching:
   - Steam app id remains highest confidence
   - alias executable/shortcut matches beat low-confidence title/window matches
4. Add a debug path:
   - `picord status --verbose` or `picord debug-processes`
   - show observed process, aliases, Steam app id, identity source, and window title
   - keep paths/env sanitized
5. Run real Proton/Wine validation against local non-Steam shortcuts.

## Self-testing notes

Build and run:

```bash
cd /mnt/hdd/Code/2026/picord
make build
bin/picord run
```

With Discord desktop open, the default app id should be ready:

```yaml
app_id: "1499058229571752148"
```

Quick image/RPC smoke test:

```bash
bin/picord debug-rpc-image --external-url https://cdn.akamai.steamstatic.com/steam/apps/620/header.jpg
```

Expected: command prints the app id it used and Discord should show a Picord test activity while the command is running.

Catalog commands:

```bash
bin/picord catalog status
bin/picord catalog refresh --source steam_shortcuts
bin/picord catalog search "Lethal Company"
```

## Validation commands

Before marking future implementation done, run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```

## Safety / privacy notes

- Do not log or store full process environments.
- Keep env parsing allowlisted.
- Avoid exposing full user paths in web UI or CLI debug output unless explicitly needed.
- Discord application ID is not a secret; tokens/secrets are.
