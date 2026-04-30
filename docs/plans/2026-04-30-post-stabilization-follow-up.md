# Post-stabilization follow-up plan

Date: 2026-04-30
Branch: `master`
Context: The 7-step post-Kimi stabilization pass is implemented. A second audit found several correctness and observability risks. This plan covers the next implementation tranche after the immediate guardrails now added in this pass.

## Current state after this pass

Implemented and covered by tests:

- Steam AppID extraction is shared between the legacy hint helper and the production process table path.
- Steam AppIDs are digit-only; signed values such as `+620` and `-620` are ignored.
- Catalog matching evaluates all entries returned by alias/title searches before source-priority tie-breaking.
- `steam_shortcut` has Steam-like source priority in ties.
- Windows-style cmdline names and aliases are reduced to safe basenames.
- Default `/api/status` keeps identity IDs and aliases out; `/api/status?verbose=1` adds sanitized identity fields without exe/cwd/args.
- `picord debug-processes --json` emits sanitized DTOs and remains valid JSON for empty results.
- `debug-processes --name` searches names, aliases, window titles, Steam AppIDs, and desktop IDs.
- Multi-value `NSpgid`/`NSsid` parsing uses the procfs-visible leftmost value.
- Descendant and ancestor traversal both have cycle protection.

Validation to keep before commit:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```

## Remaining P0: make identity enrichment explainable and safer

### 1. Relation-source aware alias propagation

Problem:

Carriers still receive aliases from all ancestors and descendants without knowing which relationship contributed each alias. Ancestors can be shell/terminal launchers. That can create noisy aliases and future false positives.

Implementation:

- Change `collectRelatedPIDs` to return relation records instead of bare PIDs:
  - `pid`
  - relation type: `descendant`, `ancestor`, `pgid_peer`, later maybe `sid_peer_debug`
  - gate result: `direct`, `shared_steam_app_id`, `shared_wineprefix`, `shared_compat_path`, `compat_subtree`
- Propagate aliases by default from descendants.
- Propagate aliases from ancestors only when the ancestor is itself a Wine/Proton/Steam launcher with a shared strong clue.
- Keep propagating structured SteamAppID from strong ancestors if the child lacks one.
- Keep same-PGID peers behind the existing shared Wine/Proton clue gates.
- Do not propagate aliases from common shells/terminals (`bash`, `zsh`, `fish`, `sh`, `gnome-terminal`, `konsole`, `kitty`, `alacritty`) unless a strong shared clue is present.

Acceptance tests:

- `bash -> wine` with no game child does not give `wine` a `bash` alias.
- `steam/proton -> Game.exe` can propagate SteamAppID to the final IPC game process.
- Carrier with two conflicting related SteamAppIDs does not silently choose an arbitrary ID.
- Debug/status output can say which relation supplied an alias.

### 2. `scan_all_processes=false` same-PGID Wine/Proton enrichment without broad reads

Problem:

Candidate-first scanning enriches IPC candidates plus ancestors/descendants. It can miss same-PGID Wine/Proton peers because those peers are not enriched before the gate is evaluated. Fully enriching all same-PGID peers would weaken the privacy promise.

Implementation:

- Add a lightweight, allowlisted peer-hint read path for same-PGID peers only after an IPC candidate has a Wine/Proton clue.
- Peer hint path may read only:
  - comm/status already in the lite table
  - allowlisted Steam/Wine/Proton env keys
  - maybe cwd/exe only if the candidate already has a compat prefix and the peer path is under that prefix
- Fully enrich a peer only after the lightweight clues pass `sharesWineProtonClue`.
- Keep final output filtered to IPC candidate PIDs.
- Record scan mode as `all_processes` or `ipc_candidates` in status.

Acceptance tests:

- `scanAll=false`: pressure-vessel IPC candidate + same-PGID `Game.exe` with same SteamAppID enriches the candidate aliases and SteamAppID.
- `scanAll=false`: same-PGID firefox without shared clues is not fully read and does not contribute aliases.
- `scanAll=true`: full resolver behavior is unchanged.

### 3. Atomic scan snapshots and first-scan state

Problem:

`SetDetected` and `SetLastScanTime` are separate. Status can observe a new process list with an old or empty timestamp. Startup status cannot distinguish pending first scan from no detections.

Implementation:

- Replace separate setters with `SetScanSnapshot(procs, scanTime, scanMode, scanState)`.
- Store `time.Time` internally; format at JSON boundary.
- Add status fields:
  - `scan_state`: `pending`, `scanned`, `error`
  - `scan_mode`: `all_processes` or `ipc_candidates`
  - `last_scan_time`
- Optionally perform one immediate scan on monitor start instead of waiting for the first ticker.

Acceptance tests:

- Status after a snapshot returns matching process data, scan mode, and timestamp atomically.
- Fresh daemon status reports `scan_state=pending` instead of a misleading empty timestamp.
- Race test around concurrent status reads and snapshot writes passes.

## Remaining P1: better matching diagnostics and deterministic ranking

### 4. Match reason and confidence in status/debug output

Problem:

Verbose status shows identity fields, but it still cannot answer why a game did or did not become the active Rich Presence.

Implementation:

- Track the selected presence source: `profile`, `catalog`, `default`, `none`.
- Add sanitized match metadata:
  - matched title/profile name
  - match reason
  - confidence/score
  - active Discord app ID
  - RPC connected and last error when available
- Add a `picord debug-match` command or extend `debug-processes --json` with optional match diagnostics.

Acceptance tests:

- Synthetic catalog SteamAppID match reports reason `steam_app_id`, confidence `100`.
- Profile alias match reports profile source and profile score.
- No-match state reports enough context without leaking cwd/args/env/full paths.

### 5. Catalog matcher scoring cleanup

Problem:

Matcher now evaluates all returned candidates, but source priority is the only confidence tie-break. Alias confidence stored in the catalog is still ignored by the matcher.

Implementation:

- Add reason priority: direct SteamAppID > direct Lutris slug > direct desktop ID > alias SteamAppID > alias executable/title > executable > exact title > substring.
- Return alias confidence from `SearchByAlias` or add a new store method returning `Entry + Alias`.
- Combine method confidence and alias confidence explicitly, e.g. `min(methodConfidence, aliasConfidence)`.
- Add stable final tie-break by source priority, entry ID, then title.

Acceptance tests:

- Low-confidence alias from Steam install dir cannot outrank a stronger direct match.
- Same-confidence same-source entries resolve deterministically.
- Steam shortcut vs desktop tie follows documented source priority.

### 6. Profile matcher guardrails

Problem:

An empty window-title or regex profile can match everything. Equal-score profile/process ties are not fully deterministic.

Implementation:

- Ignore blank match values for all match types.
- Use stable sorting or explicit original index tie-breaks in `FindBestMatch`.
- Add optional match-type specificity: exact process name > window title > regex.

Acceptance tests:

- Empty window-title profile does not match every process.
- Empty regex profile does not match every process.
- Equal-score profiles pick the first configured profile deterministically.

## Remaining P2: UX, docs, and manual validation

### 7. CLI/API robustness

- `picord status` should check non-200 responses before decoding.
- If verbose status becomes token-protected, CLI API helpers need to send `X-Picord-Token` when available.
- `catalog search` should join multi-word query args or print a clear error.
- `debug-processes --wine --proton` semantics should be explicit: mutually exclusive or OR.

### 8. Manual Wine/Proton testing guide

Add a short README/HANDOFF checklist:

```bash
bin/picord status --verbose
bin/picord debug-processes --wine --with-aliases
bin/picord debug-processes --name "<game>" --json
```

Expected healthy signals:

- Last scan is recent.
- Scan mode is visible.
- Wine/Proton carrier has the game basename alias.
- SteamAppID is present for Steam/Proton games.
- Match reason explains the selected active presence.

### 9. End-to-end synthetic fixture

Create a fixture that covers the whole flow:

1. Steam shortcut/catalog entry exists.
2. Synthetic Proton/Wine process table includes a carrier and a real game process.
3. SteamAppID and executable aliases propagate.
4. Catalog/profile matcher chooses the intended title.
5. Status/debug JSON explains the chosen presence without private paths.
