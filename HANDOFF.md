# Picord handoff

Updated: 2026-04-30

## Current focus

Kimi finished a first implementation pass for Wine/Proton process identity detection. This pass analyzed it, stabilized build/runtime blockers found during review, and created the next implementation plan.

Picord is still targeting the user's Discord application:

- App name: Picord
- Application ID: 1499058229571752148

## Current branch state

- Repository: `/mnt/hdd/Code/2026/picord`
- Branch: `master`
- Base before this review: `754c71b feat: configure Picord app default and plan Wine detection`
- New code stabilization commits created in this review:
  - `373c083 feat: add Wine Proton process identity aliases`
  - `aae3557 fix: allow Steam shortcuts catalog refresh`
- New plan file:
  - `docs/plans/2026-04-30-post-kimi-wine-identity-stabilization.md`

## What changed in Kimi's iteration

Kimi's work moved the Wine/Proton plan from documentation into a working first pass:

- Added `/proc` process table support in `internal/monitor/proctable.go`.
- Added Wine/Proton/Steam carrier classification and alias enrichment in `internal/monitor/identity.go`.
- Routed monitor scanning through `ResolveProcessIdentities()`.
- Added aliases to `profile.DetectedProcess`.
- Made profile matching alias-aware.
- Made catalog matching alias-aware.
- Added `picord debug-processes` as a process identity inspection command.
- Added unit tests for process table walking and Wine/Proton alias enrichment.

## Stabilization applied during this review

### Build/debug command fix

Kimi's iteration added the `debug-processes` command entry but the command function was missing. This review added `cmdDebugProcesses()` so the project compiles and the command shows:

- PID
- process name
- Steam app ID
- aliases
- window title

### Real Linux process table fix

Real `/proc/<pid>/status` commonly has `NSpgid` and `NSsid`, not `Pgid` and `Sid`. The first pass parsed `NSpgid` but not `NSsid`, which could leave session IDs as `0`.

This review fixed that and added guardrails so unknown `Pgid`/`Sid` value `0` does not make every process look related.

Tests added:

- namespace session parsing from `NSsid`
- unknown session peers are ignored
- unknown process-group peers are ignored

### Steam shortcuts manual refresh fix

The docs and intended user testing flow use:

```bash
bin/picord catalog refresh --source steam_shortcuts
```

Auto-refresh already had Steam shortcuts support, but the daemon API did not accept `steam_shortcuts` in manual refresh requests. This review added server support and updated the CLI help text.

Test added:

- `TestHandleCatalogRefreshAcceptsSteamShortcuts`

## Validation status

Latest validation run during this review:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```

All passed after the stabilization commits. Re-run this same gate after future code changes.

## Current implementation quality read

Good foundation:

- Wine/Proton carrier processes can now receive aliases from related processes.
- The matching pipeline can use aliases for profile and catalog matching.
- The user now has a debug entry point to inspect process identity data.
- Manual Steam shortcut catalog refresh now works through the daemon path.

Main remaining risks:

1. Steam app ID propagation is incomplete in the new resolver path.
   - The old `readProcHints` path parsed `AppId=...` and `steam://rungameid/...` from args.
   - The production resolver needs that same behavior.
   - `SteamCompatAppId` should be promoted as a Steam app ID candidate, not only a generic alias.
   - Numeric aliases should either match `AliasSteamAppID` or stay in a dedicated Steam app ID field.

2. Process-group/session enrichment is still too broad.
   - Ancestor/descendant relations are strong.
   - Same process-group and especially same-session peers can include unrelated desktop apps.
   - Gate broad peer enrichment behind shared Wine/Steam/Proton clues such as Steam app ID, `WINEPREFIX`, or `STEAM_COMPAT_DATA_PATH`.

3. Wine/Windows path aliases need normalization.
   - Args like `C:\Games\Game\Game.exe` should produce `Game.exe` and `Game`, not a full path-like alias.
   - This is both a matching issue and a privacy/debug-output issue.

4. Catalog matcher should pick the best-confidence match.
   - Current ordering can return an earlier lower-confidence match before a stronger alias match.
   - Steam app ID and strong aliases should win over generic process-name matches.

5. User-facing debugging is still thin.
   - `picord status` does not show aliases, Steam app ID, desktop ID, match reason, confidence, relation source, active app ID, RPC state, scan mode, or last scan time.
   - `debug-processes` is useful but noisy; it needs filters and possibly JSON output.

## Next plan

Detailed plan written at:

- `docs/plans/2026-04-30-post-kimi-wine-identity-stabilization.md`

Recommended execution order:

1. Steam app ID propagation and Steam-ID catalog matching.
2. Relationship gating to avoid false positives from broad PGID/SID peers.
3. Windows path alias normalization and alias privacy guardrails.
4. Best-confidence catalog matcher.
5. Verbose status and filtered debug-processes output.
6. Preserve `scan_all_processes = false` privacy/performance semantics.
7. Namespace/race hardening plus end-to-end synthetic identity-to-catalog tests.

## Manual user-testing path

After building:

```bash
make build
```

With Discord desktop running:

```bash
bin/picord debug-rpc-image --external-url <safe-image-url>
```

Refresh local catalog data:

```bash
bin/picord catalog refresh --source steam_local
bin/picord catalog refresh --source steam_shortcuts
bin/picord catalog refresh --source desktop
```

Inspect process identity while launching a Steam, non-Steam, Wine, or Proton game:

```bash
bin/picord debug-processes
```

Known caveat: before the next P0 pass, debug output may show useful aliases but matching can still miss some Proton/non-Steam games when the only strong signal is a command-line Steam app ID, `SteamCompatAppId`, or a Windows-style executable path.

## Do not lose

- The public Picord Discord application ID is `1499058229571752148`.
- Do not preserve credentials, tokens, passwords, or secrets in docs/logs.
- Keep committing and pushing every completed change.
