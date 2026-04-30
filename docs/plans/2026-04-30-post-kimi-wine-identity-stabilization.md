# Post-Kimi Wine/Proton identity stabilization plan

Date: 2026-04-30
Branch: `master`
Baseline commits reviewed:
- `373c083 feat: add Wine Proton process identity aliases`
- `aae3557 fix: allow Steam shortcuts catalog refresh`

## Current read

Kimi's latest iteration moved Picord from "plan only" to a working first pass for Wine/Proton process identity:

- `internal/monitor/proctable.go` builds a `/proc` process table with PID, PPID, process group, session, executable path, cwd, args, window title, and allowlisted process hints.
- `internal/monitor/identity.go` classifies Wine/Proton/Steam carrier processes and enriches them with aliases from related processes.
- `internal/monitor/monitor.go` routes normal scanning through `ResolveProcessIdentities()`.
- `internal/profile/matcher.go` checks process aliases for profile process-name and regex matches.
- `internal/catalog/matcher.go` checks aliases when matching detected processes to catalog entries.
- `cmd/picord debug-processes` now exists and prints PID, process name, Steam app ID, aliases, and window title.

Stabilization already applied in this pass:

- Added the missing `cmdDebugProcesses` implementation so the new CLI command compiles.
- Fixed real Linux `/proc/<pid>/status` handling for `NSsid` and prevented unknown `Pgid`/`Sid` values of `0` from grouping every process together.
- Added tests for namespace session parsing and unknown process-group/session guardrails.
- Added manual daemon API support for `catalog refresh --source steam_shortcuts`, matching the existing docs and CLI flow.
- Verified the tree with `git diff --check`, `go test -count=1 ./...`, `go vet ./...`, `go test -race ./...`, and `make build`.

## Remaining risk summary

The implementation is a good foundation, but it is still a first-pass resolver. The largest remaining risks are false positives from broad process relationships, missed Steam shortcut matches because Steam app IDs are not normalized through the new resolver path, and insufficient debug/status output for the user to understand why a Wine/Proton game did or did not match.

## P0 next steps

### 1. Make Steam AppID propagation match the production resolver path

Problem:

The old `readProcHints` path parses command-line tokens such as `AppId=620` and `steam://rungameid/620`, but the production scanner now uses `ResolveProcessIdentities()` and does not preserve all of that behavior. `SteamCompatAppId` is also read as a hint but only becomes a generic alias, not a proper Steam app ID candidate. Numeric aliases currently do not search catalog `AliasSteamAppID` rows.

Implementation:

- Move Steam app ID extraction into a shared helper used by the process table / identity resolver.
- Extract Steam IDs from:
  - allowlisted env: `SteamAppId`, `SteamGameId`, `SteamAppID`, `SteamOverlayGameId`, `SteamCompatAppId`
  - args: `AppId=...`, `--appid ...`, `steam://rungameid/...`, `steam://run/...`
- Promote related-process Steam IDs onto carriers when the relationship is strong enough.
- Either:
  - treat numeric aliases as `AliasSteamAppID` in `catalog.Matcher`, or
  - keep Steam IDs separate from generic aliases and match them through `DetectedProcess.SteamAppID` only.

Acceptance tests:

- Synthetic `/proc` process with `steam://rungameid/620` sets `DetectedProcess.SteamAppID == "620"`.
- Synthetic `/proc` process with `AppId=620` sets `DetectedProcess.SteamAppID == "620"`.
- Synthetic Proton process with `SteamCompatAppId=620` sets or propagates Steam app ID `620`.
- Catalog store with `AliasSteamAppID=620` matches a carrier enriched from a related Steam ID.

### 2. Gate process-group/session peer enrichment to avoid false positives

Problem:

The resolver currently considers descendants, ancestors, process-group peers, and session peers for carriers. Descendants/ancestors are generally strong; process group and session can be too broad on desktop Linux. A carrier could inherit aliases from unrelated same-session applications and update Discord Rich Presence for the wrong game.

Implementation:

- Keep ancestor/descendant enrichment as the default strong relation.
- Keep same-PGID peers only when at least one Wine/Proton/Steam clue is shared, such as:
  - same `WINEPREFIX`
  - same `STEAM_COMPAT_DATA_PATH`
  - same Steam app ID
  - same executable/cwd subtree under a Steam compatibility prefix
- Disable same-session peers by default, or only use them as a last-resort debug-only signal with a shared clue requirement.
- Track relation source internally so debug output can explain why an alias was added.

Acceptance tests:

- `wine` plus unrelated `firefox` in the same session does not add `firefox` aliases.
- `wine` plus `Game.exe` child still adds `Game.exe` and `Game` aliases.
- `pressure-vessel` / Proton wrapper plus same-PGID game process with shared Steam compat hints still enriches correctly.

### 3. Normalize Wine/Windows path aliases and prevent path leakage

Problem:

`ExtractAliases` currently uses Linux path handling. Wine args often look like `C:\Games\Game\Game.exe` or `Z:\home\user\Games\Game.exe`. These can remain full path-like aliases, which hurts matching and can leak private path fragments in `debug-processes`.

Implementation:

- Add a shared basename helper that understands both `/` and `\` separators.
- Strip wrapping quotes.
- For `.exe`, emit only:
  - `Game.exe`
  - `Game`
- Do not keep full path aliases from args.
- Reuse or align with the existing Steam shortcut executable basename behavior.

Acceptance tests:

- `C:\Games\Lethal Company\Lethal Company.exe` yields aliases `Lethal Company.exe` and `Lethal Company`.
- `Z:\home\user\Games\Game.exe` yields `Game.exe` and `Game` only.
- Alias list contains no `/` or `\` from Windows path args.

### 4. Make catalog matching choose the best confidence match, not first match

Problem:

Catalog matching now has confidence comments/scoring, but some lower-confidence checks can return before higher-confidence alias matches. A carrier named `wine` with a strong alias should not lose because the observed process name matched a weaker/default entry first.

Implementation:

- Evaluate all available candidates and return the highest-confidence match.
- Tie-break deterministically by source priority and title/executable specificity.
- Add reason/confidence to the match result if the surrounding type can support it.

Acceptance tests:

- If `proc.Name` matches one catalog entry at 80 but an alias matches another at 85/95/100, the alias match wins.
- Steam app ID match remains the highest priority.

## P1 next steps

### 5. Improve status/debug observability for real user testing

Problem:

`picord status` and `/api/status` currently hide aliases and match reasons. `debug-processes` is useful but too noisy and does not show whether a process would actually match catalog/profile/RPC.

Implementation:

- Add `picord status --verbose` or `/api/status?verbose=1`.
- Include sanitized debug-only fields:
  - aliases
  - Steam app ID / desktop ID
  - identity source / relation source
  - related PIDs
  - catalog/profile match title
  - match reason/confidence
  - active Discord app ID
  - RPC connected / last error if available
  - last scan time and scan mode
- Keep full env, cwd, args, and full paths out of default status.
- Add `debug-processes` filters:
  - `--wine` / `--proton`
  - `--with-aliases`
  - `--pid`
  - `--name`
  - `--json`
  - possibly `--all` to show noise that is hidden by default.

Acceptance tests:

- Status default remains concise and privacy-safe.
- Verbose status exposes aliases without cwd/args/env/full paths.
- `debug-processes --with-aliases` only shows processes with useful identity data.
- JSON output is stable enough for bug reports.

### 6. Preserve scan-all privacy/performance semantics

Problem:

`scanProcesses(false)` used to mean "only inspect Discord IPC candidates". The new resolver snapshots all processes before filtering, which is a privacy/performance regression for users with `scan_all_processes = false`.

Implementation:

- Split scanning into two modes:
  - full resolver path for `scan_all_processes = true`
  - candidate-first path for `scan_all_processes = false`
- In candidate-first mode, read expensive env/cmdline/exe/cwd data only for Discord IPC candidates and selected related PIDs.

Acceptance tests:

- With `scanAll=false`, unrelated processes are not read for env/cmdline hints.
- With `scanAll=true`, full enrichment remains available.

### 7. Harden `/proc` parsing for namespace stacks and races

Problem:

`NSpgid` and `NSsid` can contain multiple values in nested PID namespaces. `/proc` is not an atomic snapshot; processes can exit mid-read. The resolver should be explicit about which namespace ID it uses and tolerate partial data.

Implementation:

- Parse multi-value `NSpgid` and `NSsid` fields deterministically.
- Decide whether to use first or last value based on what matches `/proc` paths from the host namespace, then document it in code.
- Avoid overwriting namespace-aware values with `Pgid`/`Sid` if both exist.
- Add fallback parsing from `/proc/<pid>/stat` when status lacks usable group/session fields.
- Add visited/cycle protection in ancestry/descendant traversal.

Acceptance tests:

- `NSpgid:\t5000\t7` and `NSsid:\t4000\t7` parse as documented.
- If both namespace and non-namespace fields exist, the intended field wins.
- Ancestor/descendant traversal terminates with malformed cyclic table data.

## P2 next steps

### 8. Consolidate old and new hint parsing

- Keep one shared parsing implementation for Steam IDs, desktop IDs, executable paths, cwd, args, and allowlisted environment hints.
- Ensure tests validate the production `ResolveProcessIdentities()` path, not only legacy helpers.

### 9. Add end-to-end synthetic identity-to-catalog tests

Build a test fixture that covers:

1. Steam shortcut import from `shortcuts.vdf`.
2. Synthetic Proton/Wine process table with a generic carrier process.
3. Steam app ID and executable/title alias propagation.
4. Catalog match selection.
5. Sanitized status/debug output that explains the winner.

### 10. Polish CLI robustness

- `picord status` should handle non-200 responses and schema changes without panics.
- `catalog search` should join unquoted multi-word query args or produce a clear usage error.
- Catalog refresh output should be friendly and include source, entries changed, and errors.

## Suggested execution order

1. Steam app ID propagation and catalog Steam-ID matching.
2. Relationship gating to remove broad session false positives.
3. Windows path alias normalization.
4. Best-confidence catalog matcher.
5. Status/debug observability.
6. scan-all privacy/performance split.
7. Namespace/race hardening and end-to-end fixtures.

## Validation gate for each implementation pass

Run before every commit:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```
