# Picord post-Kimi follow-up stabilization plan

> Date: 2026-04-29
> Base reviewed: `bb25942 fix: harden catalog UI and local API`
> Previous plan: `docs/plans/2026-04-29-post-kimi-stabilization.md`
> Purpose: record what Kimi's second stabilization pass fixed, identify the remaining blockers, and define the next implementation order.

## 0. Audit summary

Kimi's latest pass landed four commits after `58bb300 docs: plan post-Kimi stabilization`:

- `d249714 fix: make catalog detection active by default`
- `d2dbcb2 fix: replay presence after Discord reconnect`
- `671c973 fix: harden catalog sync and storage`
- `bb25942 fix: harden catalog UI and local API`

The pass resolved several high-priority items from the previous plan:

- Removed the tracked root `picord` binary and added `/picord` to `.gitignore`.
- Fixed new default catalog sources to use implemented adapters (`steam_local`, `desktop`) instead of unsupported `lutris_local`.
- Made catalog fallback iterate detected processes after a profile miss, so catalog-only matches can now become active.
- Added explicit catalog API DTOs using snake_case JSON fields consumed by the web UI.
- Added/expanded tests around catalog matching, default sources, Lutris max pages, source-state errors, store update behavior, monitor hints, RPC reconnect replay, socket discovery, and server catalog endpoints.
- Switched catalog storage to pure-Go `modernc.org/sqlite`.
- Improved `/api/status` privacy by returning sanitized process fields only.
- Replaced dynamic web UI `innerHTML` rendering with DOM/textContent construction.
- Made `api.js` throw on non-2xx responses.
- Added long CLI flags and some catalog command fixes.

Verification run during this audit:

```bash
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
git diff --check 58bb300..HEAD
git diff --check
```

All passed.

Current repo metrics from `pygount`:

- 42 Go files.
- 4,491 Go code lines.
- 102 test functions/benchmarks across 17 `_test.go` files.
- Generated build artifact remains ignored at `bin/picord`.

Conclusion: the project is much closer to usable, but the next pass should still be stabilization-first. Do not expand the public catalog or image providers until the daemon's Discord availability, local API write safety, profile editing, and refresher lifecycle are reliable.

## 1. Highest-priority findings

### P0. Desired presence is still lost if Discord is disconnected at match time

Files:

- `cmd/picord/main.go:429-432`
- `cmd/picord/main.go:270-294`
- `cmd/picord/main.go:82-100`

Kimi added `rpcManager.desiredActivity` and replay-on-connect. That works only after `rm.setActivity(activity)` has been called at least once.

Current `setRichPresence` returns immediately when `rm.isConnected()` is false, before it renders and stores the desired activity:

```go
func setRichPresence(rm *rpcManager, p *profile.Profile, proc *profile.DetectedProcess) {
    if !rm.isConnected() {
        return
    }
    ...
    rm.setActivity(activity)
}
```

Impact:

- Picord starts, Discord is down, a game is detected.
- The monitor sets `currentProfile` but `desiredActivity` stays nil.
- Discord starts later and `rpcMgr.connect()` succeeds.
- There is no desired activity to replay.
- If the same game remains active, no new send happens because dedupe is based on profile name.

Required fix:

1. Build the `rpc.RichActivity` before checking connectivity.
2. Call `rm.setActivity(activity)` even when disconnected; `rm.setActivity` already records `desiredActivity` before returning `not connected` when there is no client.
3. Treat a `not connected` result as a warning/retry state, not as a reason to lose desired presence.
4. Dedupe on rendered activity/process fingerprint and last-successful-send state, not only `currentProfile.Name`.
5. Add tests for:
   - Active game detected before Discord exists; later connect replays the activity.
   - Discord restarts while the same game stays active; reconnect replays without needing profile name change.

Acceptance:

- With a detected catalog/profile match and no Discord socket, `rpcManager.desiredActivity` is non-nil.
- After a later successful mock reconnect, one `SET_ACTIVITY` frame is emitted for that stored activity.

### P0. Local write API is still CSRF-able and unauthenticated

Files:

- `internal/server/server.go:128-148`
- `internal/server/server.go:256-313`
- `internal/server/server.go:425-505`
- `internal/server/server.go:519-540`

Kimi narrowed CORS response headers to local origins, but the middleware still does not reject unsafe requests. A hostile page can still send simple `POST` requests with `text/plain` or form content to loopback endpoints, and local processes can call every mutating endpoint without any token.

Current risk endpoints:

- `POST /api/override`
- `DELETE /api/override`
- `PUT /api/settings`
- `POST /api/reload`
- `POST /api/profiles`
- `PUT /api/profiles/:name`
- `DELETE /api/profiles/:name`
- `POST /api/catalog/refresh`
- `POST /api/catalog/profiles/from-entry/:id`

Required fix:

1. Add a single write-protection middleware for unsafe methods.
2. Reject unsafe requests when `Origin` is non-local.
3. Decide policy for missing `Origin`:
   - allow CLI/no-origin requests only if they include a local API token, or
   - allow no-origin loopback requests but require strict `Content-Type: application/json` for JSON mutators.
4. Require `Content-Type: application/json` for JSON bodies.
5. Generate a per-run or persisted local token and make the embedded web UI include it, or use a SameSite session cookie minted by the local page load.
6. Add tests proving hostile-origin writes are rejected, not merely hidden by CORS.

Acceptance:

- `POST /api/reload` from `Origin: https://evil.example` returns 403.
- JSON mutators reject `text/plain` bodies.
- Browser UI still works from `http://127.0.0.1:<port>`.
- CLI either supplies the token or has a documented trusted local path.

### P0. Editing profiles from the UI can silently disable them

Files:

- `internal/server/web/js/app.js:394-417`
- `internal/server/server.go:223-231`
- `internal/profile/manager.go:117-120`

The web edit form submits a profile without an `enabled` field. Go decodes the missing bool as `false`; `PUT /api/profiles/:name` then stores that disabled profile.

A second UX/API mismatch exists: the edit modal allows changing `name`, but the server overwrites `p.Name` with the URL path name, so the rename is silently ignored.

Required fix:

1. Preserve the existing profile's `Enabled` value when omitted, or add an explicit enabled control to the form.
2. Define rename behavior:
   - simplest: make name read-only while editing and keep `PUT` as update-by-identity, or
   - implement rename as delete-old/add-new with conflict checks.
3. Add regression tests:
   - editing an enabled profile keeps it enabled.
   - the UI/API rename behavior is explicit and verified.

Acceptance:

- Editing details/state of an enabled profile does not disable it.
- Users cannot be tricked by an editable name field that is ignored.

## 2. Next reliability work

### P1. Catalog matches can still be masked by broad built-in profiles

Files:

- `cmd/picord/main.go:270-298`
- `internal/profile/defaults.yaml`
- `internal/catalog/matcher.go:35-91`

Catalog matching only runs after a full profile miss. With `scan_all_processes: true`, broad launcher profiles like Steam, Lutris, and Heroic can match launcher processes and prevent a higher-confidence catalog match on the actual game process from being considered.

Required fix:

- Build profile and catalog candidates together and rank them.
- Treat catalog confidence 90-100 from Steam/Desktop/Lutris IDs as stronger than broad default launcher profiles.
- Keep user-created explicit profiles high priority so user overrides still win.
- Add a regression: detected processes include `steam` and `portal2` with `SteamAppID=620`; expected active presence is Portal 2, not Steam.

### P1. Discord socket discovery and liveness are still shallow

Files:

- `internal/rpc/client.go:22-59`
- `internal/rpc/client.go:171-205`
- `internal/rpc/client.go:254-281`
- `internal/rpc/client_test.go:298-390`

Current gaps:

- `DiscoverSocket` returns the first path that exists, even if it is a stale regular file.
- Tests reinforce this by creating regular files instead of real Unix sockets.
- `NewClient` dials only that one candidate; it does not try later valid sockets when the first path is stale.
- `Client.IsConnected` only checks local fields and does not detect remote EOF until a command runs.
- `Client.Reconnect` sets `c.conn` before handshake and can leave state confusing if handshake fails.
- `sendCommand` still reads exactly one response frame and does not route by nonce, so unsolicited Discord events can still break command/response pairing.

Required fix:

1. Build a candidate list, then dial candidates in order until one handshakes.
2. Prefer `ModeSocket` checks, but do not rely only on stat; the dial/handshake result is authoritative.
3. On handshake failure, close the conn and mark disconnected.
4. Add either a background reader/nonce router or at least a liveness probe/close monitor.
5. Add tests with stale `discord-ipc-0` plus real `discord-ipc-1`, remote close, handshake failure, and unsolicited event before response.

### P1. Refresher lifecycle and manual refresh need a single owner

Files:

- `internal/catalog/refresher.go:35-107`
- `internal/server/server.go:425-462`
- `internal/catalog/source_lutris.go:66-149`

Current gaps:

- `Start` launches the immediate refresh as an untracked goroutine.
- `Stop` waits only for the loop goroutine, so cleanup can close the store while the immediate refresh is still using it.
- The per-source context is not canceled by `Stop`; a source can block until the five-minute timeout.
- Manual `/api/catalog/refresh` runs synchronously in the HTTP request path and can overlap with background refresh of the same source.
- `lutris_public` defaults to up to 1000 pages in a request path if no cap is provided.

Required fix:

1. Give `Refresher` an owned cancellable context.
2. Run immediate refresh inside the tracked loop or track it in the wait group.
3. Add per-source singleflight/mutex to prevent overlap.
4. Convert manual refresh to enqueue a job, or impose a small default API cap and return progress/status.
5. Add a blocking fake source test proving `Stop` cancels/waits for in-flight work.
6. Add concurrent manual/background refresh tests.

### P1. Existing configs with `lutris_local` still need migration/normalization

Files:

- `internal/config/config.go:69-97`
- `internal/catalog/refresher.go:109-127`
- `cmd/picord/main.go:192-198`

New generated configs are fixed, but old user configs may still contain `lutris_local`. `BuildSources` returns one error for an unknown source, which prevents all configured sources from starting.

Required fix:

- Normalize loaded catalog source names:
  - drop known removed source `lutris_local` with a warning, or
  - map it only after a real local Lutris source exists.
- Make source building skip unknown sources and return warnings plus valid adapters, instead of failing the whole list.
- Add a test that an old config containing `lutris_local`, `steam_local`, and `desktop` still starts the valid sources.

### P1. Catalog-created profiles probably do not match real games

Files:

- `internal/server/server.go:489-505`
- `internal/profile/types.go:3-27`
- `internal/profile/matcher.go:21-54`

`POST /api/catalog/profiles/from-entry/:id` creates a profile with:

```go
Match.Type = process_name
Match.Value = catalog.NormalizeTitle(entry.Title)
```

But `process_name` matching is exact executable-name equality. A profile for `Doom Eternal` with match value `doom eternal` is unlikely to match `DOOMEternalx64vk.exe`, Proton launchers, or Steam AppID hints.

Required fix:

- Add source-aware profile match types:
  - `steam_app_id`
  - `desktop_id`
  - `lutris_slug`
  - optionally `executable`
- Or store catalog aliases and choose the highest-confidence alias when generating a profile.
- Add a test: create a profile from a Steam catalog entry, then match a detected process with `SteamAppID`.

## 3. Catalog/source/data quality improvements

### P2. Process hints are incomplete

Files:

- `internal/monitor/hints.go:39-65`
- `internal/catalog/matcher.go:45-59`

Current hints:

- Steam AppID from env/cmdline.
- Desktop ID only from `GIO_LAUNCHED_DESKTOP_FILE`.
- Lutris slug exists in the profile model but is not populated.

Next work:

- Parse Lutris command lines and paths when launched through Lutris.
- Derive Flatpak desktop/app IDs from env/cmdline/cgroup where available.
- Use exe/cwd paths to infer desktop IDs from installed `.desktop` entries.
- Add tests for Lutris, Flatpak, and desktop-launched games.

### P2. Steam local source misses non-default libraries

Files:

- `internal/catalog/source_steam.go:13-28`

`SteamLocalPaths` checks common steamapps dirs and `STEAM_COMPAT_CLIENT_INSTALL_PATH`, but does not parse `libraryfolders.vdf`. Users with games installed on secondary drives will have missing catalog entries.

Next work:

- Parse Steam `libraryfolders.vdf` from the primary Steam root.
- Add every listed `steamapps` directory to the scan set.
- Add tests with a fake `libraryfolders.vdf` and secondary appmanifest.

### P2. Store migrations and ID conflict behavior need hardening

Files:

- `internal/catalog/migrations.go:3-55`
- `internal/catalog/store.go:53-101`

Current gaps:

- `schema_version` exists but is not used to apply migrations incrementally.
- `UpsertEntry` conflicts on `(source, source_id)` but does not update `id`; if a generated ID changes for the same source/source_id, alias replacement can target the new ID while the existing entry keeps the old ID.

Next work:

- Implement real versioned migrations.
- Define entry IDs as immutable and validate conflicts, or update IDs and dependent aliases/images transactionally.
- Add conflict tests for same source/source_id with changed ID.

## 4. API/UI/CLI polish

### P2. CLI should return nonzero on HTTP failures and escape path segments

Files:

- `cmd/picord/cli.go:112-120`
- `cmd/picord/cli.go:214-220`
- `cmd/picord/cli.go:325-346`
- `cmd/picord/cli.go:405-412`

Current gaps:

- `printResponse` prints errors but callers still return exit code 0.
- `catalog status/search` print bodies without checking HTTP status.
- `cmdStatus` decodes into `map[string]any` and type-asserts fields directly.
- `profiles from-catalog` concatenates `entryID` into the path instead of using `url.PathEscape`.
- `catalog search` only uses `args[1]`, so unquoted multi-word queries are truncated.

Next work:

- Add an API helper that returns body + status + error.
- Return nonzero for all non-2xx responses.
- Decode `/api/status` into a typed struct.
- Use `url.PathEscape` for entry IDs.
- Join remaining search args with spaces.
- Add CLI tests with a mock HTTP server.

### P2. Web UI should become CSP-compatible

Files:

- `internal/server/web/index.html`
- `internal/server/web/js/app.js`
- `internal/server/server.go:508-540`

Kimi removed dynamic unsafe HTML rendering, but static inline `onclick` handlers remain, and the server does not send browser hardening headers.

Next work:

- Move inline handlers to `addEventListener` in `app.js`.
- Add `Content-Security-Policy` compatible with the static app.
- Add `X-Content-Type-Options: nosniff`.
- Add a hostile-string UI regression fixture or server-side response test.

## 5. Image and live Discord validation

Still do not rely on external Rich Presence image URLs by default.

Required manual validation:

```bash
./bin/picord debug-rpc-image --app-id <DISCORD_APP_ID> \
  --external-url https://cdn.akamai.steamstatic.com/steam/apps/620/header.jpg
```

With a real Discord client running, confirm:

- Does the presence update appear?
- Does the remote image render?
- Does Discord accept the payload consistently after reconnect?
- Are unsolicited frames/events observed?

If external URLs fail, keep `images.mode: generic` or Discord asset-key mode as the only supported presence image modes. The catalog may still store/cache image URLs for UI preview.

## 6. Recommended implementation order

### Phase A: reconnect replay correctness and profile edit safety

1. Fix `setRichPresence` so desired activity is recorded even while disconnected.
2. Add start-before-Discord and same-game-reconnect replay tests.
3. Fix UI/API profile edit enabled preservation and rename behavior.
4. Add profile edit regression tests.

### Phase B: local write API protection

1. Add unsafe-method guard middleware.
2. Require local origin/token/content-type per policy.
3. Update web UI and CLI as needed.
4. Add hostile-origin and wrong-content-type tests.

### Phase C: candidate ranking and profile match types

1. Rank user profiles, broad defaults, and catalog candidates in one selection path.
2. Add source-aware match types or alias-driven catalog profile creation.
3. Add launcher-masking regression tests.

### Phase D: RPC socket/liveness and refresher lifecycle

1. Dial socket candidates in order and use real Unix socket tests.
2. Clean up reconnect handshake failure state.
3. Add liveness or background reader/nonce routing.
4. Refactor refresher around cancellable context and per-source singleflight.
5. Convert manual refresh to a job or cap it safely.

### Phase E: data/source expansion only after reliability

1. Parse Steam `libraryfolders.vdf`.
2. Populate Lutris/Flatpak/desktop hints.
3. Harden migrations/ID conflict semantics.
4. Run live Discord image validation.
5. Update README once behavior is confirmed.

## 7. Completion criteria for the next pass

Before another catalog-growth pass, require all of these:

- `go test -count=1 ./...`, `go vet ./...`, `go test -race ./...`, `make build`, and `git diff --check` pass.
- Picord records and replays desired presence when Discord starts after a game is detected.
- Profile edits preserve enabled state.
- Mutating HTTP endpoints reject hostile-origin browser requests.
- Catalog candidates can beat broad launcher defaults when source-specific confidence is high.
- Refresher shutdown cannot race store close.
- CLI returns nonzero on HTTP errors.
- `HANDOFF.md` reflects the actual status.
- All changes are committed and pushed.
