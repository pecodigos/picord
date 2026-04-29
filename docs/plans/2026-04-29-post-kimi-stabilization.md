# Picord post-Kimi stabilization plan

> Date: 2026-04-29
> Base reviewed: `ecf26b0 docs: document rich game catalog`
> Purpose: convert Kimi's catalog iteration from a broad prototype into a reliable runtime feature.

## 0. Audit summary

Kimi shipped a large first pass of the rich catalog system:

- `internal/catalog` SQLite store, schema, matcher, sources, image resolver, refresher, and tests.
- `/proc` hint collection for Steam IDs and expanded detected-process fields.
- Catalog HTTP endpoints, CLI commands, web search/suggestion UI, and docs.
- Background catalog refresh wiring and a `debug-rpc-image` command.
- Profile-manager and reconnect stabilization from the earlier Phase 0 plan.

The code builds and tests pass, but several integration bugs mean the new catalog is not yet dependable in the real daemon. Treat the next iteration as a stabilization pass, not a provider-expansion pass.

Verified during audit:

```bash
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
git diff --check
```

Codebase delta since the previous catalog plan:

- 12 commits after `0a939b2`.
- 44 files changed.
- About 4.1k insertions.
- New tracked root binary: `picord` (~20 MiB) — should not be in git.

## 1. Priority findings

### P0. Runtime catalog auto-detection is effectively unreachable

Files:

- `cmd/picord/main.go:265-280`
- `internal/profile/matcher.go:57-90`

Current flow:

1. The monitor callback calls `profileMgr.Match(procs)`.
2. If no built-in/user profile matches, `profileMgr.Match` returns `(nil, nil)`.
3. Catalog fallback is gated by `catalogMatcher != nil && proc != nil`.
4. Because `proc` is nil on profile miss, no catalog process is ever tried.

Impact: catalog search/import can work, but automatic catalog presence will not trigger for ordinary games unless a classic profile already matched.

Required fix:

- After profile miss, iterate all detected processes and run `catalogMatcher.Match(ctx, proc)` for each.
- Choose the best catalog result by confidence; tie-break by source confidence and process/window relevance.
- Add daemon-level tests around this selection logic, preferably by extracting a small pure function from the monitor callback.

Acceptance test:

- With no matching profiles and a catalog entry aliased to `SteamAppID=620`, a detected process with `SteamAppID=620` becomes the active presence.

### P0. Default catalog refresh is broken by an unsupported source

Files:

- `internal/config/config.go:45-49`
- `cmd/picord/main.go:390-394`
- `internal/catalog/refresher.go:109-125`

Defaults include `lutris_local`, but `BuildSources` supports only:

- `steam_local`
- `lutris_public`
- `desktop`

Impact: default startup reports an unknown source and does not start the background refresher at all.

Required fix:

- Short term: change default sources to `steam_local` and `desktop`; keep `lutris_public` opt-in because it is network-heavy.
- Longer term: implement a real `lutris_local` source if local Lutris metadata is available on Linux installs.
- Add a test that `BuildSources(config.DefaultCatalogSources)` or equivalent succeeds.

### P0. Catalog API JSON does not match the web UI

Files:

- `internal/catalog/types.go:13-24`
- `internal/server/server.go:328-373`
- `internal/server/web/js/app.js:48-58,102-109`

`catalog.Entry` has no JSON tags, so Go emits fields like `ID`, `Source`, `ReleaseYear`, `ImageURL`. The web UI reads lowercase/snake-case fields like `e.id`, `e.source`, `e.release_year`, `e.image_url`.

Impact: catalog cards can render `undefined`, and Save Profile can post an undefined entry ID.

Required fix:

- Prefer explicit API DTOs for catalog responses instead of exposing store structs directly.
- Use stable lowercase/snake-case JSON names.
- Add API tests that assert the exact JSON shape consumed by the UI.

### P0. RPC reconnect connects but does not replay desired presence

Files:

- `cmd/picord/main.go:267-272`
- `cmd/picord/main.go:405-450`
- `cmd/picord/main.go:306-325`

If Discord is unavailable or reconnects while the same game remains active, Picord can set `currentProfile` before successfully sending the activity. The reconnect goroutine only connects; it does not resend the desired activity.

Impact: starting Picord before Discord can leave Rich Presence empty until the active match changes.

Required fix:

- Introduce a desired-presence state: profile/activity + process + rendered payload + last-sent fingerprint.
- Only mark a payload as sent after `SetActivity` succeeds.
- On successful reconnect, replay the desired payload.
- Add tests for start-before-Discord and reconnect-same-game behavior.

### P0. Startup leaks one Discord IPC connection on successful initial connect

File:

- `cmd/picord/main.go:144-151`

Startup calls `rpcNewClient(cfg.AppID)` as a probe and discards the successful client, then calls `rpcMgr.connect()` which creates a second client.

Required fix:

- Call `rpcMgr.connect()` once and log its error.
- Do not create a throwaway RPC client.

### P0. Discord IPC socket discovery has path-generation bugs

File:

- `internal/rpc/client.go:21-79`

Observed issues:

- `DISCORD_IPC_PATH` is checked after Flatpak paths in `DiscoverSocket`, despite being an explicit override.
- The indexed fallback appends to a path already ending in `discord-ipc-0`, generating paths like `discord-ipc-0-1` instead of `discord-ipc-1`.
- Common nonzero sockets can be missed.

Required fix:

- Generate candidates from runtime directories and socket indices directly.
- Check env override first.
- Add tests for `discord-ipc-1`, Flatpak sockets, `/tmp`, `/run/user/$UID`, and env override ordering.

### P0. Remove committed binary artifact

Files:

- `picord` tracked in git, ~20 MiB ELF binary.
- `.gitignore` ignores `bin/` but not the root `picord` binary.

Required fix:

- `git rm picord`
- Add `/picord` to `.gitignore`.
- Keep `bin/picord` ignored as the build output.

## 2. P1 findings

### Catalog hints are advertised but not populated

Files:

- `internal/profile/matcher.go:9-19`
- `internal/monitor/hints.go:16-61`
- `internal/monitor/monitor.go:117-127`
- `internal/catalog/matcher.go:45-59`

`DetectedProcess` has `LutrisSlug` and `DesktopID`, and the catalog matcher has high-confidence branches for them, but the monitor only populates Steam AppID.

Next steps:

- Either correct docs to say only Steam AppID is currently extracted, or populate the fields.
- Add safe extraction for Lutris and desktop/Flatpak IDs from command line, cwd, executable path, desktop metadata, and known env vars.
- Add monitor tests for each populated hint.

### Image cache and external URL mode are not wired end-to-end

Files:

- `internal/catalog/images.go`
- `internal/catalog/migrations.go:35-47`
- `internal/config/config.go:22-27`
- `cmd/picord/main.go:182-186,203-207`

`DownloadImage`, image table schema, `cache_enabled`, and `max_cache_mb` exist, but there are no store image CRUD methods and no runtime lazy-cache path. `ExternalEnabled` is hard-coded false, so `images.mode: external_url` never sends an external URL.

Next steps:

- Keep `generic` or `asset_key` as the safe default.
- Add explicit live-validation state before enabling external URLs.
- Implement image CRUD and lazy caching only on user-visible demand: detected game, opened catalog entry, explicit preview.
- Add cache-size enforcement or hide/remove config fields until implemented.

### SQLite store needs integrity and migration hardening

Files:

- `internal/catalog/store.go`
- `internal/catalog/migrations.go`

Issues:

- SQLite foreign keys are declared but not enabled with `PRAGMA foreign_keys=ON`.
- `schema_version` is inserted but not used for versioned migrations.
- `UpsertEntry` only deletes aliases when `len(aliases) > 0`, leaving stale aliases if a refresh supplies none.
- Upsert conflict on `(source, source_id)` does not update `id`, which can be surprising if IDs ever change.

Next steps:

- Enable foreign keys and test cascade behavior.
- Add proper migration version application.
- Always replace aliases, including replacing with an empty set when intended.
- Define immutable entry ID semantics, or explicitly update related rows inside a transaction.

### Lutris public source needs safer sync semantics

File:

- `internal/catalog/source_lutris.go`

Issues:

- Saved cursor is the current page URL, not the next page URL.
- Completed sync can resume from the last page rather than a fresh full refresh.
- ETag is stored but not used with `If-None-Match`.
- Pages are fetched in a tight loop up to 1000 pages by default.

Next steps:

- Treat `MaxPages` runs as partial syncs that store the next cursor.
- Treat completed full syncs as complete and reset cursor appropriately.
- Send conditional requests when an ETag is available.
- Add per-page rate limiting and context-aware sleeps.
- Add tests for partial resume, completed sync, error resume, 304 handling, and rate limiting.

### Refresher lifecycle can race with store shutdown

Files:

- `internal/catalog/refresher.go:35-107`
- `cmd/picord/main.go:453-474`

`Start` launches the immediate refresh in a detached goroutine outside the WaitGroup. `Stop` waits for the loop goroutine only, so cleanup can close the store while a refresh still uses it.

Next steps:

- Track every refresh goroutine in the WaitGroup.
- Use a cancellable context owned by the refresher.
- Prevent overlapping refreshes.
- Test that `Stop` waits for in-flight refresh work.

### Local API exposes too much and allows cross-origin control

Files:

- `internal/server/server.go:90-145,476-487`
- `internal/profile/matcher.go:9-19`

`/api/status` returns `exe_path`, `cwd`, and full process args. Args can contain private URLs or tokens. CORS is `Access-Control-Allow-Origin: *` with write methods.

Next steps:

- Return sanitized status DTOs by default: pid, name, window title, source hints; omit args/cwd/exe unless debug mode explicitly requests them.
- Restrict CORS to same-origin localhost, or require a local auth token for write endpoints.
- Add tests that sensitive process fields are omitted.

### Status handler has nested-lock risk

File:

- `internal/server/server.go:129-145`

`handleStatus` holds `state.mu.RLock()` and then calls methods that also lock the same RWMutex. This can deadlock when a writer is pending.

Next steps:

- Snapshot all fields under one lock without calling other lock-taking methods.
- Add a race/concurrency test.

### Catalog-created profiles likely do not match real processes

File:

- `internal/server/server.go:446-459`

The generated profile uses `process_name` with `catalog.NormalizeTitle(entry.Title)` (for example, `doom eternal`). Existing process-name matching expects exact process executable names.

Next steps:

- Prefer executable aliases when available.
- Add new source-aware match types or matching rules: `steam_app_id`, `desktop_id`, `lutris_slug`, `executable`.
- Store chosen alias in the generated profile.
- Add tests that a profile created from a Steam catalog entry matches a detected Steam process.

### CLI and docs mismatches

Files:

- `cmd/picord/cli.go`
- `README.md`

Issues:

- CLI implements `profiles from-catalog`, while usage and README mention `profile from-catalog`.
- `catalog search` should URL-escape queries.
- Long flags advertised for `override` are parsed after `flag.Parse`, so unknown long flags can exit before manual parsing.

Next steps:

- Align command names and docs.
- Use `url.QueryEscape`.
- Register long flags with the flag package or use a small shared flag parser.
- Add CLI tests for spaces, ampersands, and long flags.

## 3. P2 findings

- Config reload is only partial. Define which config fields are hot-reloadable and which require restart; protect shared runtime state with a mutex.
- Web UI uses `innerHTML` and inline `onclick` with data from imported catalogs and process names. Replace with DOM creation, `textContent`, and data attributes.
- `api.js` does not throw on HTTP errors; callers can treat failed responses as success.
- Same-profile activity updates are skipped because the daemon compares only profile name, not rendered activity/process identity.
- Regex profile matching recompiles patterns on every scan; cache compiled regexes in the profile manager.
- Add timestamps (`start_time`) support only after the runtime path is stable.

## 4. Implementation order

### Phase A — Make the shipped catalog work at runtime

1. Remove tracked binary artifact.
   - `git rm picord`
   - Add `/picord` to `.gitignore`.
   - Verify `git status` no longer shows root binary after build.

2. Fix defaults and source construction.
   - Default sources: `steam_local`, `desktop`.
   - Keep `lutris_public` opt-in for manual refresh.
   - Add tests for default source construction.

3. Fix catalog fallback selection.
   - Extract match selection from the monitor callback.
   - Iterate detected processes after profile miss.
   - Pick best catalog match by confidence.
   - Add tests for profile priority over catalog, catalog fallback, and clearing when no matches remain.

4. Fix catalog JSON DTOs.
   - Add explicit response structs.
   - Update tests and UI expectations.
   - Verify catalog search cards render actual titles and entry IDs.

5. Run:
   - `go test -count=1 ./...`
   - `go vet ./...`
   - `go test -race ./...`
   - `make build`

Commit message suggestion:

```bash
git commit -m "fix: make catalog detection active by default"
```

### Phase B — Make presence reliable across Discord lifecycle

1. Remove startup probe leak.
2. Add desired-presence tracking and replay on reconnect.
3. Fix socket discovery candidate generation.
4. Add tests for reconnect replay and nonzero socket discovery.
5. Run live Discord validation for:
   - startup before Discord,
   - Discord restart while a game is active,
   - asset-key image mode,
   - external image URL mode through `debug-rpc-image`.

Commit message suggestion:

```bash
git commit -m "fix: replay presence after Discord reconnect"
```

### Phase C — Harden catalog storage and sources

1. Enable SQLite foreign keys and versioned migrations.
2. Fix alias replacement semantics.
3. Repair Lutris cursor/ETag/rate-limit behavior.
4. Improve Steam local import:
   - parse `libraryfolders.vdf`,
   - discover non-default libraries,
   - derive better executable aliases from installed game directories where safe.
5. Add or remove/document `lutris_local`.
6. Populate desktop/Lutris hints or remove unreachable matcher branches from docs.

Commit message suggestion:

```bash
git commit -m "fix: harden catalog sync and storage"
```

### Phase D — Harden local API, UI, and CLI

1. Sanitize `/api/status`.
2. Restrict CORS or add local auth token for write endpoints.
3. Replace UI `innerHTML`/inline handlers with safe DOM rendering.
4. Make `api.js` throw on non-2xx responses.
5. Fix CLI command naming, URL escaping, and long flags.
6. Fix profile creation from catalog to use executable/source aliases.

Commit message suggestion:

```bash
git commit -m "fix: harden catalog UI and local API"
```

### Phase E — Finish image strategy honestly

Do not expand image providers yet. First make current image behavior explicit and safe.

1. Persist live external-image validation result, or require a config flag that says validation was completed.
2. Implement image table CRUD.
3. Implement lazy image caching for selected/visible entries only.
4. Add cache-size cleanup for `max_cache_mb`.
5. Show image preview/source status in the local UI.
6. Keep Rich Presence default on `generic` or Discord asset keys until live validation succeeds.

Commit message suggestion:

```bash
git commit -m "feat: add lazy catalog image cache"
```

## 5. Definition of done for the next iteration

The next iteration should be considered complete when all of these are true:

1. A catalog-only detected game can set Rich Presence without a classic profile.
2. Picord starts with default config and no catalog source errors.
3. If Discord starts after Picord, the current desired presence appears automatically after reconnect.
4. Catalog search UI displays real titles/IDs and Save Profile uses a real entry ID.
5. `/api/status` no longer exposes full args/cwd/exe by default.
6. `picord` root binary is not tracked.
7. Verification passes:

```bash
go test -count=1 ./...
go vet ./...
go test -race ./...
make build
git diff --check
```

8. `HANDOFF.md` is updated with actual status, not aspirational status.
9. Each logical fix is committed and pushed.

## 6. Guidance for the next implementer

- Do not add new metadata providers until P0/P1 reliability issues are fixed.
- Keep local-first behavior: Steam manifests, desktop files, local Lutris metadata if implemented.
- Do not eagerly download public image datasets.
- Do not assume external Rich Presence image URLs work until validated against a live Discord client.
- Do not expose process args or filesystem paths in default UI/API output.
- Use tests before fixes where feasible, especially for P0 items.
- Keep commits small and push after verification.
