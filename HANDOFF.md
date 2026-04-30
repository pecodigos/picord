# Picord handoff

Updated: 2026-04-30

## Current focus

The 7-step post-Kimi Wine/Proton stabilization pass is complete. This pass performed a second deep audit, fixed the highest-confidence issues found immediately, and wrote the next follow-up plan.

Picord is still targeting the user's Discord application:

- App name: Picord
- Application ID: 1499058229571752148

## Current branch state

- Repository: `/mnt/hdd/Code/2026/picord`
- Branch: `master`
- Main completed plan: `docs/plans/2026-04-30-post-kimi-wine-identity-stabilization.md`
- Follow-up plan: `docs/plans/2026-04-30-post-stabilization-follow-up.md`

## What is now implemented

Wine/Proton identity detection now includes:

- `/proc` process table support with PID, PPID, PGID, SID, exe path, cwd, cmdline args, allowlisted env hints, and window titles.
- Steam AppID extraction from args and allowlisted env:
  - `AppId=620`
  - `steam://rungameid/620`
  - `steam://run/620`
  - `--appid 620`
  - `SteamAppId`, `SteamGameId`, `SteamAppID`, `SteamOverlayGameId`, `SteamCompatAppId`
- `DetectedProcess.SteamAppID` propagation through the resolver.
- Wine/Proton/Steam carrier classification and alias enrichment from related processes.
- Relationship gating for same-PGID peers behind Wine/Proton/Steam shared clues.
- SID peers disabled by default.
- Windows path alias normalization using basenames only.
- Catalog matching over all candidates with confidence and source-priority tie-breaking.
- Profile/catalog matching aware of aliases.
- `scan_all_processes = false` candidate-first scanning that returns only Discord IPC candidates.
- `/api/status?verbose=1` and `picord status --verbose` with sanitized identity fields.
- `picord debug-processes` filters:
  - `--wine`
  - `--proton`
  - `--with-aliases`
  - `--name`
  - `--pid`
  - `--json`

## Extra fixes from the second audit

The second audit found and fixed several latent issues after the original 7 steps:

1. Namespace parsing corrected.
   - Multi-value `NSpgid` and `NSsid` now use the leftmost procfs-visible value, not the inner namespace value.
   - Test: `TestReadProcStatusMultiValueNamespace`.

2. Ancestor traversal hardened.
   - `Ancestors()` now has cycle protection, matching `Descendants()`.
   - Test: `TestAncestorsCycleProtection`.

3. Windows cmdline process names sanitized.
   - A cmdline argv0 such as `C:\Users\alice\Games\Lethal Company\Lethal Company.exe` becomes `Lethal Company.exe`.
   - Aliases do not expose full Windows paths.
   - Test: `TestResolveProcessIdentities_SanitizesWindowsCmdlineName`.

4. Steam AppID validation tightened.
   - App IDs are digit-only; `+620` and `-620` are ignored.
   - Tests in monitor and catalog matcher cover signed values.

5. Legacy hint parsing consolidated.
   - `readProcHints` now uses the same `ExtractSteamAppID` helper and includes `SteamCompatAppId`.

6. Catalog ambiguity improved.
   - `Matcher` now appends all entries returned by alias/title searches instead of only `entries[0]`.
   - `steam_shortcut` source gets Steam-like tie priority.
   - Test: `TestMatcher_EvaluatesAllAliasCandidatesForTieBreak`.

7. Status/debug privacy improved.
   - Default `/api/status` does not include aliases, Steam app ID, or desktop ID.
   - Verbose status includes those fields but never exe path, cwd, args, or env.
   - `debug-processes --json` emits sanitized DTOs and stays valid JSON even when no rows match.
   - `debug-processes --name` searches aliases, window title, Steam AppID, and desktop ID, not just process name.

## Validation status

Latest validation passed:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```

`make build` produced `bin/picord`.

## Main remaining risks

1. Relation-source tracking is still missing.
   - The resolver can say what aliases exist, but not yet which relation supplied them.
   - Ancestor aliases need tighter source-aware propagation to avoid shell/terminal noise.

2. `scan_all_processes=false` may miss same-PGID Wine/Proton peer aliases.
   - It avoids broad env/cmdline reads, but it only fully enriches IPC candidates plus ancestors/descendants.
   - Same-PGID peers need a lightweight allowlisted hint pass before full enrichment.

3. Status still lacks match/RPC explanation.
   - Verbose status shows identity data, but not selected presence source, match reason, match confidence, active Discord app ID, RPC state, or scan mode.

4. Matcher scoring can be more deterministic.
   - Alias confidence stored in catalog rows is not yet incorporated.
   - Reason priority and stable final tie-breaks should be added.

5. Profile matcher guardrails remain.
   - Empty window-title or regex profiles can still be too broad.
   - Equal-score tie behavior should be made explicit and stable.

## Next plan

Detailed follow-up plan:

- `docs/plans/2026-04-30-post-stabilization-follow-up.md`

Recommended next execution order:

1. Relation-source aware alias propagation.
2. `scan_all_processes=false` same-PGID peer enrichment without broad reads.
3. Atomic scan snapshots and first-scan state.
4. Match reason/confidence/status/RPC diagnostics.
5. Catalog matcher reason priority and alias-confidence scoring.
6. Profile matcher blank-value and tie guardrails.
7. CLI/API robustness and manual Wine/Proton testing docs.

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

Launch a Steam, non-Steam, Wine, or Proton game, then inspect:

```bash
bin/picord status --verbose
bin/picord debug-processes --wine --with-aliases
bin/picord debug-processes --name "<game>" --json
```

Expected healthy signs:

- Last Scan is recent.
- Steam/Proton game has a Steam App ID.
- Wine/Proton carrier has a game basename alias, e.g. `Lethal Company.exe` and `Lethal Company`.
- Debug JSON does not include `exe_path`, `cwd`, `args`, env values, or private full paths.

## Do not lose

- The public Picord Discord application ID is `1499058229571752148`.
- Do not preserve credentials, tokens, passwords, or secrets in docs/logs.
- Keep committing and pushing every completed change.
