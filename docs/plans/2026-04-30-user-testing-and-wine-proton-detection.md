# 2026-04-30 — User testing readiness and Wine/Proton process identity

## Current read

Kimi's latest iteration materially improves catalog coverage:

- `steam_shortcuts` is now a default catalog source.
- `internal/catalog/vdf.go` parses Steam's binary `shortcuts.vdf` format.
- `SteamShortcutsSource` imports non-Steam shortcut title, executable basename, and generated Steam app id aliases.
- The catalog matcher can already prefer `steam_app_id`, then Lutris slug, desktop id, executable alias, exact title, and window title.

I also found one immediate user-testing gap unrelated to Wine: existing and fresh configs still need to resolve to the real Picord Discord application. That is now handled by the app default `1499058229571752148`, including old generated configs with an empty `app_id`.

## Key conclusion

Do not implement Wine/Proton support as "find children of a `wine` process" only.

That will be brittle because Steam, Proton, pressure-vessel, Wine, wineserver, Windows services, and the actual game process may be siblings, descendants, process-group peers, or already reparented. The stable identity signals are not just parent/child edges; they are:

1. Steam/Proton app id hints from environment and launch args.
2. Process-group/session relationships around generic Wine/Proton processes.
3. Windows executable names visible in cmdline, `/proc/<pid>/exe`, or related process names.
4. Steam shortcut metadata already imported from `shortcuts.vdf`.
5. Window title as a last-mile disambiguator, especially for games that collapse to `wine`.

Best design: add a small process-identity resolver that builds one `/proc` table per scan, enriches generic Wine/Proton processes with safe aliases, then lets the existing catalog matcher score those aliases.

## Target behavior

When a non-Steam game launched through Steam/Proton appears as any of these process names:

- `wine`
- `wine64`
- `wineserver`
- `wine-preloader`
- `proton`
- `pressure-vessel-*`
- Windows helper processes such as `explorer.exe`, `services.exe`, `steam.exe`

Picord should still detect the real game if one of these signals is available:

- A related process has a game-like `.exe` basename, e.g. `Lethal Company.exe`.
- A related process/env/cmdline exposes the generated Steam shortcut app id.
- The process group/session contains a game-like executable path matching a Steam shortcut executable alias.
- The window title uniquely matches a catalog entry or shortcut title.

## Proposed implementation plan

### Phase 1 — Process table foundation

Add `internal/monitor/proctable.go` and tests.

The scanner should read each `/proc/<pid>` once and keep:

- `PID`
- `PPID`
- process group id
- session id
- `Name`
- `ExePath`
- `Cwd`
- `Args`
- allowlisted env hints only:
  - `SteamAppId`
  - `SteamGameId`
  - `SteamAppID`
  - `SteamOverlayGameId`
  - `PROTON_COMPAT_DATA_PATH`
  - `STEAM_COMPAT_DATA_PATH`
  - `SteamCompatAppId`
  - `WINEPREFIX`
  - `WINELOADER`
  - `GIO_LAUNCHED_DESKTOP_FILE`

Build child and parent indexes from that table.

Acceptance:

- Existing monitor tests stay green.
- New fixture tests prove PPID/process-group/session parsing without touching the real `/proc`.
- Full env is never stored or logged.

### Phase 2 — Safe alias enrichment

Extend `profile.DetectedProcess` with fields such as:

- `Aliases []string`
- `IdentitySource string`
- optional `RelatedPIDs []int` for debug/status only

Do not overwrite `Name`; preserve the real observed process for diagnostics.

Add a resolver that, for each generic Wine/Proton process or process with Wine/Proton hints:

1. Collects the process itself.
2. Walks ancestors up to a small limit.
3. Walks descendants.
4. Adds process-group/session peers only when a Steam/Wine/Proton hint ties them together.
5. Extracts candidate aliases from:
   - process name
   - executable basename
   - argv tokens ending in `.exe`
   - Steam app id env/args
   - desktop id env
   - window title
6. Removes generic/noisy names:
   - `wine`, `wine64`, `wineserver`, `wine-preloader`
   - `explorer.exe`, `services.exe`, `plugplay.exe`, `winedevice.exe`, `rpcss.exe`
   - `steam.exe`, `steamwebhelper.exe`, `proton`, `pressure-vessel-*`
7. Adds both `name.exe` and stripped `name` aliases for Windows executables.

Acceptance:

- A mocked process tree where only parent process is `wine` and child is `Lethal Company.exe` resolves aliases `Lethal Company.exe` and `Lethal Company`.
- A mocked Proton launch with `SteamGameId=<shortcut app id>` matches the imported Steam shortcut alias without needing a visible `.exe` process.
- Noise helper processes do not win over the game executable.

### Phase 3 — Matcher integration

Update `internal/catalog/matcher.go` to search aliases after high-confidence ids and before low-confidence title/window fallbacks.

Suggested scoring:

1. `SteamAppID`: 100
2. `LutrisSlug`: 95
3. `DesktopID`: 90
4. `Aliases` executable/shortcut alias: 85
5. `Name` executable: 80
6. exact title from alias/name/window: 70
7. unique window title substring: 50

For explicit user profiles, update `profile.Profile.Matches` so `process_name` and `regex` can check aliases too, while keeping current behavior for `Name`.

Acceptance:

- Existing profile matcher tests stay green.
- New tests show manual `process_name: lethal company.exe` matches a generic `wine` detected process enriched with aliases.
- Catalog match reason reports the winning alias for debugging.

### Phase 4 — Self-test/debug command

Add a user-facing diagnostic path before deeper automation:

- `picord status` should include aliases/source when present.
- Add either `picord debug-processes` or extend `picord status --verbose` to show:
  - PID
  - observed name
  - aliases
  - Steam app id
  - identity source
  - window title

Keep paths short/sanitized; do not print full env.

Acceptance:

- User can launch a game and immediately see whether Picord found `wine`, which aliases were derived, and why a catalog/profile match did or did not happen.

### Phase 5 — Real Proton/Wine test pass

Manual test recipe:

1. Build Picord.
2. Start Discord desktop client.
3. Start Picord with debug logging.
4. Refresh catalog so `steam_shortcuts` imports the current non-Steam library.
5. Launch one known non-Steam Proton/Wine game from Steam.
6. Run `picord status` or the new debug command.
7. Confirm one of:
   - rich presence shows the correct title through app `1499058229571752148`, or
   - debug output explains exactly which signal was missing.

Candidate local test titles from the current Steam shortcuts import:

- Lethal Company
- Need For Speed: Most Wanted
- Project Wingman
- Star Wars Squadrons
- Civilization VI
- Final Fantasy VII
- Slay The Spire 2
- Outer Wilds

## Risks and guardrails

- Avoid scanning arbitrary full environment. Use an allowlist only.
- Avoid storing or rendering full user paths unless necessary for matching; use basenames and sanitized tails in debug output.
- Keep process scanning O(n) per interval. Build one table and reuse indexes instead of recursively rereading `/proc`.
- Treat window title matching as fallback because titles can be launcher screens or duplicated.
- Do not make `wine` itself a profile/catalog winner; it is a carrier process, not the game identity.

## Next implementation checkpoint

The next coding pass should implement Phases 1–3 first. Phase 4 can be implemented immediately after, because it is the fastest way to observe whether real Proton games expose the expected signals on this system.
