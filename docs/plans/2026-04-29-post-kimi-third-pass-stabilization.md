# Picord post-Kimi third-pass stabilization plan

Date: 2026-04-29
Branch: master
Base reviewed: d93c1ee docs: update HANDOFF after completing stabilization plan
Previous checkpoint: 83efe71 docs: plan post-Kimi follow-up stabilization

## Purpose

Kimi finished another stabilization iteration. This audit checks what changed after `83efe71`, separates real fixes from partial fixes, and defines the next implementation pass.

Do not expand the public catalog, image providers, or large metadata ingestion until the P0/P1 items below are fixed. The current product risk is runtime correctness, not lack of sources.

## What changed since the previous checkpoint

New commits reviewed:

1. `6901579 fix: store desired presence when disconnected, protect write endpoints, preserve profile enabled state`
2. `ab3929d docs: update HANDOFF after P0 fixes`
3. `522566d feat: rank catalog candidates against broad launcher profiles`
4. `6b360c1 feat: gate external_url image mode behind config validation flag`
5. `d93c1ee docs: update HANDOFF after completing stabilization plan`

Files changed by Kimi:

- `HANDOFF.md`
- `README.md`
- `cmd/picord/cli.go`
- `cmd/picord/main.go`
- `cmd/picord/main_test.go`
- `internal/config/config.go`
- `internal/profile/manager.go`
- `internal/profile/manager_test.go`
- `internal/server/server.go`
- `internal/server/server_test.go`

## Verification run during this audit

All of these passed:

```bash
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
git diff --check 83efe71..HEAD
git diff --check
pygount --format=summary --folders-to-skip=".git,node_modules,venv,.venv,__pycache__,.cache,dist,build,.next,.tox,vendor,third_party" .
```

Targeted checks also passed:

```bash
go test ./cmd/picord -run 'TestDefaultProfiles|TestSelectBestPresence|TestSetRichPresence' -count=1 -v
go test ./internal/server -run 'TestCORS' -count=1 -v
```

Current test count from `func Test|func Benchmark`: 110 across 17 `_test.go` files.

## Fixed or improved by Kimi

### 1. Desired activity can now be recorded while Discord is initially disconnected

Relevant files:

- `cmd/picord/main.go`
- `cmd/picord/main_test.go`

Kimi moved the desired-activity storage into `rpcManager.setActivity()` before the connection check. `setRichPresence()` can now build the activity and call `rm.setActivity(activity)` even if there is no active client, so an activity can be replayed after a later connect.

Remaining caveat: live disconnect/restart behavior is still incomplete. See P0-B.

### 2. Basic hostile-Origin rejection exists for unsafe API calls

Relevant files:

- `internal/server/server.go`
- `internal/server/server_test.go`

Unsafe requests with a hostile `Origin` are now rejected. Local origins are allowed.

Remaining caveat: this is not yet full CSRF/write protection. Missing-Origin requests and content-type handling remain open. See P0-C.

### 3. Editing an existing profile no longer silently clears `Enabled`

Relevant files:

- `internal/profile/manager.go`
- `internal/profile/manager_test.go`

`Manager.Add()` preserves an existing profile's `Enabled` state when updating by name, preventing the earlier UI edit path from disabling profiles only because the edit payload omitted `enabled`.

Remaining caveat: rename semantics, disabled custom profiles, and copying defaults are still problematic. See P1-B.

### 4. Catalog candidates are now ranked against profile candidates

Relevant files:

- `cmd/picord/main.go`
- `cmd/picord/main_test.go`

`selectBestPresence()` compares catalog confidence against a profile score derived from priority. High-confidence catalog hints can beat broad low-priority launcher profiles.

Remaining caveat: scoring is still heuristic and not source/user/default aware. See P1-C.

### 5. `external_url` image mode is gated behind explicit validation config

Relevant files:

- `internal/config/config.go`
- `internal/catalog/images.go`
- `cmd/picord/main.go`
- `cmd/picord/cli.go`
- `README.md`

`images.external_validated` was added. External URLs are only sent as Discord Rich Presence image references when both `mode: external_url` and `external_validated: true` are set.

Remaining caveat: live Discord validation still has not been performed. See P2-A.

## P0 next fixes

### P0-A. Restore built-in default profiles

Problem:

The 48 built-in defaults appear disabled in current runtime.

Evidence:

- `internal/profile/defaults.yaml` entries do not include `enabled: true`.
- `internal/profile/defaults.go` marks defaults with `isDefault=true`, but does not default `Enabled=true`.
- `internal/profile/manager.go:MergeDefaults()` only appends defaults when `p.Enabled` is true.

Impact:

`profile.NewManager(cfg.Profiles, profile.DefaultProfiles())` can start with zero built-in profiles. This contradicts README/HANDOFF claims, breaks built-in app detection, and makes catalog/profile ranking tests less representative of real runtime.

Implementation plan:

1. Add failing tests first:
   - `profile.DefaultProfiles()` returns a non-empty slice.
   - Every returned default has `Enabled == true` unless an explicit future field says otherwise.
   - `profile.NewManager(nil, profile.DefaultProfiles()).Match([]DetectedProcess{{Name:"steam"}})` returns the Steam built-in profile.
   - A user profile with the same name and `enabled: false` still disables that specific default.
2. Fix `DefaultProfiles()` to set `Enabled=true` for embedded defaults when the YAML omits it.
3. Keep user config semantics intact: explicit disabled overrides must remain possible.
4. Update docs only if behavior differs from the current advertised "48 built-in profiles" behavior.

Acceptance criteria:

- Built-in default profile tests pass.
- Daemon default manager contains built-in profiles without requiring users to edit config.
- User can still disable a default by adding a same-name disabled profile in config.

### P0-B. Harden RPC stale-connection detection and reconnect replay

Problem:

The first-disconnected case improved, but live Discord restart/disconnect can still fail.

Current risks:

- `rpc.Client.IsConnected()` checks local fields only; it does not know if Discord closed the Unix socket.
- `sendCommand()` does not mark the client closed after read/write EOF.
- `setRichPresence()` calls `rm.setActivity()`, then `rm.connect()`, then `rm.setActivity()` again. If `connect()` already replayed `desiredActivity`, this can send duplicates.
- `rpcManager.connect()` ignores replay errors.
- Unit tests can touch a real Discord IPC socket because test setup does not fully isolate `DISCORD_IPC_PATH` / `XDG_RUNTIME_DIR` or mock `rpcNewClient` in every path.
- If the same game remains detected while Discord restarts, monitor-level de-duplication can prevent a fresh write unless replay is reliable.

Implementation plan:

1. Add isolated mock-socket tests before changing behavior:
   - Existing client becomes stale after Discord closes socket; next `SetActivity` marks it disconnected.
   - Reconnect dials a valid new mock socket and sends exactly one `SET_ACTIVITY` for the stored desired activity.
   - Handshake failure leaves `IsConnected() == false` and does not keep a half-connected `conn`.
   - Unit tests do not dial the developer's real Discord socket.
2. In `internal/rpc.Client`, mark the client closed and close the connection on write/read command failures.
3. In `rpcManager`, make replay semantics explicit:
   - Store desired activity before any connection attempt.
   - Reconnect once when stale or nil.
   - Send exactly once after a successful reconnect.
   - Preserve desired state for retry when replay fails.
4. Consider a small retry/backoff loop owned by `rpcManager`, not by every monitor tick.

Acceptance criteria:

- Discord starts after Picord: current detected game appears without re-detection.
- Discord restarts while a game remains active: activity is restored without requiring process name changes.
- No duplicate `SET_ACTIVITY` for one reconnect event in tests.
- No real Discord socket access from unit tests.

### P0-C. Finish unsafe local API write protection

Problem:

Current CORS middleware blocks hostile `Origin`, but local write protection is still incomplete.

Current gaps:

- Missing `Origin` is treated as trusted.
- No CSRF token, local API token, or session mechanism.
- JSON mutators do not require `Content-Type: application/json`.
- Any page served from `localhost` / `127.0.0.1` is trusted.
- Forbidden responses use a text/plain `http.Error` path instead of the API's JSON error contract.

Implementation plan:

1. Decide one local write-safety model and document it in code and README:
   - Recommended: same-origin web UI plus a random local token generated at daemon start or loaded from config/state.
   - CLI must send the token for unsafe methods.
   - Browser UI reads token from the served app context, not from a global public API endpoint.
2. Add middleware tests across every unsafe endpoint:
   - `POST /api/profiles`
   - `PUT /api/profiles/:name`
   - `DELETE /api/profiles/:name`
   - `POST /api/override`
   - `DELETE /api/override`
   - `PUT /api/settings`
   - `POST /api/reload`
   - `POST /api/catalog/refresh`
   - `POST /api/catalog/profiles/from-entry/:id`
3. Require `Content-Type: application/json` for JSON body endpoints.
4. Return JSON error bodies consistently.

Acceptance criteria:

- Hostile-origin unsafe requests are rejected.
- Missing-origin unsafe requests are either token-protected or explicitly rejected according to the chosen policy.
- Wrong content type on JSON mutators returns 415 or 400.
- CLI and web UI still work with the new protection.
- Forbidden responses have `Content-Type: application/json` and `{"error":"Forbidden"}` or equivalent typed error JSON.

## P1 next fixes

### P1-A. Make runtime config reload semantics correct and race-resistant

Problem:

Reload paths update only parts of runtime state.

Current gaps:

- Config watcher reload uses `MergeUser`, so deleted profiles can remain active.
- GUI/tray reload only updates `cfg` and profiles, not image resolver, catalog enablement, source list, refresher, RPC app ID, poll interval, scan mode, or web port behavior.
- Watcher updates `imgResolver` only when `catalogStore != nil`.
- `cfg`, `currentProfile`, and `imgResolver` are captured variables accessed from multiple goroutines without a clear runtime-state lock.
- Unknown old source names such as `lutris_local` still fail the whole source build instead of being skipped with a warning.

Implementation plan:

1. Create a small runtime state owner around mutable daemon state:
   - config
   - profile manager or current profile snapshot
   - image resolver
   - catalog matcher/refresher ownership
   - RPC app/client state
2. Replace reload `MergeUser` calls with `ReplaceUser` when reloading the config file.
3. Define restart-only fields explicitly:
   - likely `web_port`
   - maybe `poll_interval` / `scan_all_processes` unless monitor can be restarted cleanly
   - maybe `app_id` unless RPC manager can reconnect cleanly with a new ID
4. Make watcher, GUI reload, and tray reload call the same reload function.
5. Normalize unknown/legacy catalog sources so valid sources still start.

Acceptance criteria:

- Reloading config with `profiles: []` removes previous user profiles.
- Reloading `images.external_validated` affects the next catalog activity through every reload path.
- Config with `[lutris_local, steam_local, desktop]` starts valid sources and reports/skips the obsolete one.
- `go test -race` includes a test that reloads while monitor/server callbacks read runtime state.
- Restart-only settings are reported clearly to the user if changed.

### P1-B. Finish profile editing, rename, disabled-profile, and default-copy behavior

Problem:

The enabled-state preservation fix does not cover the whole profile lifecycle.

Current gaps:

- Edit modal allows changing `name`, but `PUT /api/profiles/:name` overwrites the body name from the URL, so rename is silently ignored.
- Disabled custom profiles that do not override an existing profile are skipped by `MergeUser`; later saves can drop them.
- `Manager.Add()` preserves `isDefault` when updating an existing built-in default. Once defaults are fixed, "Copy to My Profiles" for a built-in same-name profile may remain default-owned and be filtered out of the user profile API/config.

Implementation plan:

1. Decide rename behavior:
   - easiest: make the name field read-only while editing and require duplicate/copy for new names;
   - or implement real rename with conflict checks.
2. Make disabled user profiles persist and round-trip through manager/API/config.
3. Make "copy default to my profiles" produce a user-owned profile or explicit override, not a hidden default.
4. Add API/UI tests for edit, rename, disabled profile persistence, and copy-default behavior.

Acceptance criteria:

- Editing details/state keeps `Enabled` and user/default ownership correct.
- Rename is either impossible in UI or implemented with conflict errors.
- Disabled custom profiles survive reload/save cycles.
- Copying a built-in profile creates a visible user profile or an explicit override visible in the UI.

### P1-C. Replace heuristic candidate scoring with explicit candidate metadata

Problem:

Current catalog/profile ranking is better than strict profile-first, but it is still coarse.

Current gaps:

- It does not distinguish user profiles from built-in defaults.
- It does not distinguish broad launcher defaults from exact app/game defaults.
- `profileScore = priority * 10` means priority-10 profiles can tie 100-confidence catalog matches; ties prefer profiles.
- Low-confidence unique catalog matches may still be hidden by broad profiles.

Implementation plan:

1. Introduce an internal candidate struct:
   - source: user_profile, default_profile, catalog
   - match kind: exact process, window title, regex, Steam AppID, Lutris slug, desktop ID, executable, title
   - profile priority / catalog confidence
   - broadness flag for launchers and generic app profiles
2. Add table tests covering:
   - Steam/Lutris/Heroic launcher defaults
   - user explicit profiles
   - emulator defaults
   - catalog confidences 50/70/80/90/100
3. Prefer explicit user profiles when they are intentional and high priority.
4. Let source-specific catalog IDs beat broad launcher defaults.

Acceptance criteria:

- User high-priority explicit profile wins when intended.
- Broad launcher defaults lose to high-confidence catalog game hints.
- Ties and broadness are documented in tests.

### P1-D. Add source-aware catalog-created profiles

Problem:

`POST /api/catalog/profiles/from-entry/:id` creates a `process_name` profile from `NormalizeTitle(entry.Title)`. `process_name` matching is exact executable equality, so a profile made from a Steam catalog entry often will not match the actual game.

Implementation plan:

1. Add source-aware match types or generated match metadata:
   - `steam_app_id`
   - `lutris_slug`
   - `desktop_id`
   - `executable`
2. Use the highest-confidence alias available when creating a profile from a catalog entry.
3. Preserve user editability in the UI without exposing confusing internal jargon.

Acceptance criteria:

- Creating a profile from a Steam catalog entry with alias `steam_app_id=620` matches `DetectedProcess{SteamAppID:"620"}`.
- Creating from a desktop entry matches `DetectedProcess{DesktopID: ...}`.
- The UI text remains non-technical.

### P1-E. Refactor catalog refresh ownership

Problem:

Background and manual refresh can overlap. The immediate refresh goroutine started by `Refresher.Start()` is not owned/cancelled the same way as the main loop.

Implementation plan:

1. Move refresh execution behind a per-source singleflight/queue.
2. Make `Stop()` cancel and wait for in-flight immediate refreshes.
3. Make manual refresh return quickly or block predictably with a timeout.
4. Cap opt-in network sources safely; avoid an HTTP request path that can run 1000 Lutris pages.

Acceptance criteria:

- Blocking fake source proves `Stop()` cancels/waits.
- Manual and background refresh for the same source cannot overlap.
- `lutris_public` manual refresh has a safe default cap.

### P1-F. Add CLI HTTP-contract tests and fix exit codes

Problem:

The CLI still lacks mock HTTP tests and can return exit 0 for HTTP errors.

Implementation plan:

1. Make CLI commands testable against a mock server.
2. Decode API responses into typed structs where practical.
3. Return nonzero on non-2xx responses.
4. Properly escape path parameters and join multi-word search queries.

Acceptance criteria:

- Mock daemon returns 500/404; each CLI command returns nonzero.
- `picord catalog search Hollow Knight` sends `q=Hollow+Knight`.
- `picord profile from-catalog <id-with-slash-or-space>` uses `url.PathEscape`.
- `picord status` handles API error JSON without panic.

## P2 validation and polish

### P2-A. Live Discord validation

Validate with a real Discord client after P0-B:

- Picord starts before Discord.
- Discord restarts while a game remains active.
- Nonzero IPC socket discovery.
- Flatpak/native Discord paths.
- External URL image mode via `picord debug-rpc-image`.
- Behavior when Discord sends unsolicited events before command responses.

Outcome must be documented as supported, experimental, or disabled.

### P2-B. Frontend hardening

Current dynamic UI escaping is improved, but inline `onclick` handlers remain and prevent a strict CSP.

Implementation plan:

1. Move inline handlers to `addEventListener`.
2. Add `Content-Security-Policy` and `X-Content-Type-Options: nosniff` headers.
3. Add a static check or DOM test proving no inline handlers remain.

### P2-C. Documentation cleanup

Keep docs synchronized after implementation:

- `HANDOFF.md`
- `README.md`
- current plan file

Fix known inaccuracies:

- test count is now 110, not 102;
- matching is score-based, not profile-first;
- external_url fallback chain is generic only after profile asset and catalog asset fallback paths;
- built-in profile docs must match the actual enabled behavior after P0-A.

## Recommended implementation order for Kimi

1. P0-A: restore built-in defaults.
2. P0-B: RPC stale/liveness/replay semantics with isolated tests.
3. P0-C: local write protection/token/content-type/JSON errors.
4. P1-A: unified runtime reload semantics and state synchronization.
5. P1-B: profile edit/rename/default-copy lifecycle.
6. P1-C: explicit candidate ranking metadata.
7. P1-D/E/F: catalog-created profiles, refresher ownership, CLI tests.
8. P2 live Discord validation and frontend/docs polish.

## Required verification before committing implementation

Run all commands before every implementation commit:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
```

For P0-B, also run targeted RPC tests with environment isolation to prove unit tests do not use a real Discord socket.

## Definition of done for the next Kimi pass

- Built-in profiles are active by default and tested.
- Discord restart/disconnect replay is reliable in tests and ready for live validation.
- Unsafe write APIs have a real local protection model and consistent JSON errors.
- Runtime reload behavior is either fully applied or explicitly restart-only per setting.
- Profile edits/copies do not silently lose user intent.
- CLI failure modes have tests and return nonzero on daemon/API errors.
- `HANDOFF.md` and README reflect actual behavior, not intended behavior.
