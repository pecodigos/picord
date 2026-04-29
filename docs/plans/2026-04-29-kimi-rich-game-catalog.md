# Picord Rich Game Catalog Implementation Plan

> For Kimi K2.6: implement this plan task-by-task. Use TDD, keep changes small, and commit after every logical task.

Goal: Turn Picord from a small profile matcher into a Linux Discord Rich Presence daemon with a large local game/app catalog: title metadata, matching aliases, and image metadata/cache, with smart runtime detection for Steam, Proton/Wine, Lutris, desktop apps, and ordinary Linux games.

Architecture: Keep the existing Go daemon, local web UI, YAML user config, and Discord IPC client. Add a local catalog database under XDG data, an image cache under XDG cache, source adapters for public/local metadata, richer process hints from /proc, and a catalog matcher that can generate ephemeral Rich Presence when no user profile matches. Do not embed or download every image; store image URLs and cache only the entries Picord detects, previews, or the user opens.

Tech stack: Go 1.21, existing stdlib HTTP server, existing YAML config, SQLite or an equivalent local embedded DB, gzip JSON seed files if a seed is embedded, stdlib net/http for source adapters, and existing Discord IPC code.

---

## 0. Current project analysis

Repository state observed on 2026-04-29:

- Project root: /mnt/hdd/Code/2026/picord
- Main language: Go 1.21
- Approximate size: 21 Go files, about 2,034 Go code lines; total project about 3,187 code lines excluding skipped folders.
- Current built-in database: internal/profile/defaults.yaml with 48 profiles.
- Current matching signals: process_name exact, window_title substring, regex over process name.
- Current process detection: scans numeric /proc/<pid> entries by default with scan_all_processes: true.
- Current UI/API: embedded SPA plus API for status, profiles, defaults, override, settings, reload.
- Current Discord image handling: activity.large_image and activity.small_image are passed as Discord Rich Presence asset keys. The README tells users to upload images in the Discord Developer Portal.
- Current tests: go test ./... was green with 42 tests after RPC mock coverage was added.

Important current limitations and bugs to fix before scaling:

1. Discord connection after startup failure is broken.
   - cmd/picord/main.go creates rpcClient once.
   - If Discord is not running at startup, rpcClient remains nil.
   - The reconnect goroutine only reconnects when rpcClient != nil, so Picord cannot begin publishing if Discord starts later.
   - Fix before catalog integration, because a larger catalog is pointless if Discord startup order breaks Rich Presence.

2. profile.Manager uses map[string]*Profile pointers into a slice.
   - internal/profile/manager.go stores &m.profiles[i].
   - Appending can reallocate the slice, sorting swaps values under existing pointers, and deleting shifts elements.
   - This can make byName point at stale or wrong profiles.
   - Fix before adding any catalog/profile bridge. Use map[string]int or rebuild the index after every mutation/sort.

3. Monitor logging is too noisy for scan_all_processes.
   - internal/monitor/monitor.go logs every detected process every poll.
   - With all-process scanning this can flood stdout/systemd logs every 2 seconds.
   - Gate per-process logging behind debug mode or remove it before catalog detection adds more data.

4. Existing profile matching is O(profile_count * process_count).
   - Fine for 48 built-ins, not fine for hundreds of thousands of catalog entries.
   - The catalog matcher must be indexed by high-confidence keys: Steam AppID, Lutris slug/id, desktop ID, exe basename, normalized title, then lower-confidence window title search.

5. Discord image display has a hard product constraint.
   - Current code sends image keys, not HTTP URLs.
   - A database can know image URLs, but Discord may not display them unless the Local RPC supports external image URLs or a Discord application asset exists.
   - Do not build an unlimited asset uploader first. First validate image modes against a live Discord client.

---

## 1. Product target

Picord should be able to do this:

1. Detect a running game/app on Linux.
2. Extract useful runtime hints:
   - process name
   - executable path
   - command-line args
   - current working directory
   - visible window title
   - Steam AppID from environment/cmdline/manifests
   - Lutris game id/slug/name where available
   - desktop entry id where available
   - Wine/Proton exe path where available
3. Look up the best catalog entry.
4. Set Discord Rich Presence with:
   - title: canonical game/app title
   - details/state: sane defaults such as "Playing {title}" and source-specific text
   - large image: resolved through a safe image mode
   - large image text: title
5. Let users override or save the generated match as a profile.
6. Refresh catalog metadata from public/local sources without aggressive scraping.
7. Keep working offline after metadata has been cached.

Non-goals for the first implementation:

- Do not download images for every catalog entry.
- Do not embed a massive static image dataset in the binary.
- Do not use unofficial Discord account-token APIs.
- Do not require API keys for the baseline.
- Do not replace the existing simple user profile system.

---

## 2. Smart data-source strategy

Use a layered strategy: local installed metadata first, public indexed metadata second, optional API-key enrichers later.

### Source priority table

| Priority | Source | Purpose | Verified during planning | Image strategy | Notes |
|---|---|---|---|---|---|
| 1 | Local Steam installation | Detect installed Steam games accurately | Steam CDN image URLs for appid 620 returned JPEGs | Build image URLs from appid; use appdetails on demand | Parse appmanifest_*.acf and Steam env vars. No network required for title/appid. |
| 2 | Steam Store appdetails | Enrich known Steam AppIDs | https://store.steampowered.com/api/appdetails?appids=620&filters=basic returned Portal 2 JSON | header_image and CDN patterns | Use only for known appids or on-demand search. Cache responses and rate-limit. |
| 3 | Lutris public API | Large public game title catalog | https://lutris.net/api/games returned count around 337k and paginated results | banner_url from API | Good baseline for broad title database. Rate-limit and support resumable cursors. |
| 4 | Local Lutris installation | Detect installed Lutris games | Not yet probed in repo | local game configs plus Lutris API banner | Parse Lutris local config/database if present. Treat schema as best-effort. |
| 5 | .desktop files and icon themes | App titles/icons outside game launchers | Local Linux standard | local icon path | Covers native apps and many non-Steam games. |
| 6 | PCGamingWiki / Wikidata | Optional enrichment | PCGamingWiki MediaWiki and Wikidata SPARQL responded | public/licensed images only | Use carefully; not baseline. Respect rate limits and licensing. |
| 7 | Community overrides in repo | High-confidence aliases and fixes | Existing defaults.yaml proves curated data works | known image URL or asset key | Small curated YAML files, not a giant static database. |
| Later | IGDB, RAWG, SteamGridDB, MobyGames | Richer metadata/images | Many require keys/auth | better covers/posters | Optional only, behind config and explicit keys. |

### Steam notes

Runtime detection is more important than bulk Steam indexing.

Implement these hints first:

- /proc/<pid>/environ keys: SteamAppId, SteamGameId, SteamAppID, SteamOverlayGameId.
- /proc/<pid>/cmdline token patterns: AppId=<digits>, steam://rungameid/<digits>, /steamapps/common/...
- Steam manifest paths:
  - ~/.local/share/Steam/steamapps/appmanifest_*.acf
  - ~/.steam/steam/steamapps/appmanifest_*.acf
  - paths under $STEAM_COMPAT_CLIENT_INSTALL_PATH if present
- For Proton/Wine, detect generic wine/proton processes but use SteamGameId or exe path to avoid reporting only "Wine".

Image URL candidates for a known Steam AppID:

- https://cdn.akamai.steamstatic.com/steam/apps/{appid}/header.jpg
- https://cdn.akamai.steamstatic.com/steam/apps/{appid}/library_600x900.jpg
- https://cdn.akamai.steamstatic.com/steam/apps/{appid}/capsule_616x353.jpg

Do not assume every candidate exists. Probe with HEAD or a bounded GET, validate content type, cache successful URLs.

### Lutris notes

The public endpoint is suitable for a large baseline title catalog:

- GET https://lutris.net/api/games
- Supports pagination with next URLs.
- Result shape observed: id, name, slug, year, banner_url.

Implementation rules:

- Store title/id/slug/year/banner_url.
- Do not download banner images during full refresh.
- Save cursor/page progress in source_state so refresh can resume.
- Add a max pages option for tests and first-run safety.
- Rate-limit to avoid hammering the service.

### Image reality check

Picord needs two distinct image concepts:

1. Catalog image URL / cached local image.
   - For Picord UI previews and metadata.
   - Always useful.

2. Discord Rich Presence image reference.
   - What goes into large_image/small_image.
   - Current code expects a Discord app asset key.
   - May or may not support external URLs depending on current Discord Local RPC behavior.

Implement an ImageResolver with explicit modes:

```go
type ImageMode string

const (
    ImageModeAssetKey    ImageMode = "asset_key"     // current behavior
    ImageModeExternalURL ImageMode = "external_url"  // only enable after live validation
    ImageModeGeneric     ImageMode = "generic"       // safe fallback
)
```

Rules:

- asset_key mode: use entry.DiscordAssetKey or user profile large_image. If missing, fall back to generic.
- external_url mode: only enabled by config after a live test command proves Discord accepts the chosen format.
- generic mode: use one known generic asset key such as picord_game, or omit image if no key exists.
- Never attempt to upload every game image to one Discord application. The developer portal asset model is not designed for unlimited public catalogs.

---

## 3. Target data model

Create a new package: internal/catalog.

Recommended files:

- internal/catalog/types.go
- internal/catalog/normalize.go
- internal/catalog/store.go
- internal/catalog/migrations.go
- internal/catalog/matcher.go
- internal/catalog/images.go
- internal/catalog/source.go
- internal/catalog/source_steam.go
- internal/catalog/source_lutris.go
- internal/catalog/source_desktop.go
- internal/catalog/store_test.go
- internal/catalog/matcher_test.go
- internal/catalog/source_steam_test.go
- internal/catalog/source_lutris_test.go
- internal/catalog/images_test.go

Suggested core types:

```go
package catalog

type EntryKind string

const (
    EntryKindGame        EntryKind = "game"
    EntryKindApplication EntryKind = "application"
    EntryKindLauncher    EntryKind = "launcher"
)

type Entry struct {
    ID              string
    Source          string
    SourceID        string
    Kind            EntryKind
    Title           string
    NormalizedTitle string
    ReleaseYear     int
    ImageURL        string
    ImageKind       string
    DiscordAssetKey string
    UpdatedAtUnix   int64
}

type AliasKind string

const (
    AliasTitle       AliasKind = "title"
    AliasExecutable  AliasKind = "executable"
    AliasWindowTitle AliasKind = "window_title"
    AliasSteamAppID  AliasKind = "steam_app_id"
    AliasLutrisSlug  AliasKind = "lutris_slug"
    AliasDesktopID   AliasKind = "desktop_id"
)

type Alias struct {
    EntryID    string
    Kind       AliasKind
    Value      string
    Normalized string
    Confidence int
}

type Image struct {
    EntryID       string
    URL           string
    CachePath     string
    SHA256        string
    Width         int
    Height        int
    MIME          string
    Status        string
    FetchedAtUnix int64
}

type DetectionHints struct {
    PID         int
    Name        string
    ExePath     string
    Cwd         string
    Args        []string
    WindowTitle string
    Env         map[string]string
    SteamAppID  string
    LutrisSlug  string
    DesktopID   string
}

type MatchResult struct {
    Entry      Entry
    Confidence int
    Reason     string
}
```

Suggested SQLite schema:

```sql
CREATE TABLE IF NOT EXISTS entries (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  source_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  normalized_title TEXT NOT NULL,
  release_year INTEGER NOT NULL DEFAULT 0,
  image_url TEXT NOT NULL DEFAULT '',
  image_kind TEXT NOT NULL DEFAULT '',
  discord_asset_key TEXT NOT NULL DEFAULT '',
  updated_at_unix INTEGER NOT NULL,
  UNIQUE(source, source_id)
);

CREATE INDEX IF NOT EXISTS idx_entries_normalized_title ON entries(normalized_title);

CREATE TABLE IF NOT EXISTS aliases (
  entry_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  value TEXT NOT NULL,
  normalized TEXT NOT NULL,
  confidence INTEGER NOT NULL,
  PRIMARY KEY(entry_id, kind, normalized),
  FOREIGN KEY(entry_id) REFERENCES entries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_alias_kind_normalized ON aliases(kind, normalized);

CREATE TABLE IF NOT EXISTS images (
  entry_id TEXT NOT NULL,
  url TEXT NOT NULL,
  cache_path TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  mime TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'new',
  fetched_at_unix INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(entry_id, url),
  FOREIGN KEY(entry_id) REFERENCES entries(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS source_state (
  source TEXT PRIMARY KEY,
  cursor TEXT NOT NULL DEFAULT '',
  etag TEXT NOT NULL DEFAULT '',
  updated_at_unix INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
```

If SQLite creates too much dependency friction, use bbolt or a JSONL index for the first pass, but prefer SQLite because search, source state, and aliases become simpler.

---

## 4. Configuration additions

Add these config fields after tests:

```go
type CatalogConfig struct {
    Enabled      bool     `yaml:"enabled" json:"enabled"`
    AutoRefresh  bool     `yaml:"auto_refresh" json:"auto_refresh"`
    Sources      []string `yaml:"sources" json:"sources"`
    RefreshHours int      `yaml:"refresh_hours" json:"refresh_hours"`
}

type ImageConfig struct {
    Mode          string `yaml:"mode" json:"mode"`
    CacheEnabled  bool   `yaml:"cache_enabled" json:"cache_enabled"`
    MaxCacheMB    int    `yaml:"max_cache_mb" json:"max_cache_mb"`
    GenericAssetKey string `yaml:"generic_asset_key" json:"generic_asset_key"`
}
```

Suggested defaults:

```yaml
catalog:
  enabled: true
  auto_refresh: true
  sources: [steam_local, lutris_local, desktop]
  refresh_hours: 24
images:
  mode: generic
  cache_enabled: true
  max_cache_mb: 512
  generic_asset_key: "picord_game"
```

Do not enable lutris_public full refresh by default until UI/CLI makes the data size clear to the user.

---

## 5. Implementation phases

### Phase 0: Stabilize existing foundations

Objective: fix current bugs that will block reliable catalog behavior.

Files:

- Modify: internal/profile/manager.go
- Modify: internal/profile/manager_test.go
- Modify: cmd/picord/main.go
- Modify: cmd/picord/main_test.go
- Modify: internal/monitor/monitor.go
- Modify: internal/monitor/monitor_test.go

Tasks:

1. Write a failing test proving profile.Manager Get/Add/Delete remains correct after many appends, sort, update, and delete.
2. Change byName from map[string]*Profile to map[string]int, or rebuild byName after every mutation and sort.
3. Run: go test ./internal/profile -run Manager -v. Expected: PASS.
4. Write a failing test or injectable helper for daemon RPC startup where initial NewClient fails and later Discord appears.
5. Refactor RPC lifecycle into a small wrapper that can create a client when nil.
6. Run: go test ./cmd/picord -v. Expected: PASS.
7. Gate noisy monitor per-process logging behind a debug flag or remove it.
8. Run: go test ./... && go vet ./... && make build. Expected: all pass.
9. Commit:

```bash
git add internal/profile internal/monitor cmd/picord
git commit -m "fix: stabilize profile manager and Discord reconnect"
```

### Phase 1: Add catalog storage

Objective: create a tested local catalog database without touching daemon behavior yet.

Files:

- Create: internal/catalog/types.go
- Create: internal/catalog/normalize.go
- Create: internal/catalog/store.go
- Create: internal/catalog/migrations.go
- Create: internal/catalog/store_test.go
- Modify: go.mod/go.sum if SQLite dependency is added

Tasks:

1. Pick the embedded DB dependency.
   - Prefer SQLite.
   - If using modernc.org/sqlite, verify it supports Go 1.21 or pin a compatible version.
   - If dependency fails, use bbolt and document why.
2. Write tests for NormalizeTitle:
   - "Hollow Knight™" -> "hollow knight"
   - "DOOM: The Dark Ages" -> "doom the dark ages"
   - repeated spaces collapse.
3. Implement NormalizeTitle.
4. Write store migration test using t.TempDir.
5. Implement Open, Close, Migrate.
6. Write UpsertEntry + aliases test.
7. Implement UpsertEntry, GetEntry, SearchByAlias, SearchTitlePrefix.
8. Run:

```bash
go test ./internal/catalog -run 'Normalize|Store' -v
go test ./...
go vet ./...
```

9. Commit:

```bash
git add go.mod go.sum internal/catalog
git commit -m "feat(catalog): add local catalog store"
```

### Phase 2: Extend process detection with runtime hints

Objective: detect game identity, not just process basenames.

Files:

- Modify: internal/profile/matcher.go or move DetectedProcess to a shared package if needed
- Modify: internal/monitor/monitor.go
- Modify: internal/monitor/monitor_test.go
- Create: internal/monitor/hints.go
- Create: internal/monitor/hints_test.go
- Modify: internal/server/web/js/app.js and cmd/picord/cli.go only if JSON shape changes

Tasks:

1. Add fields to DetectedProcess while preserving existing JSON compatibility:

```go
type DetectedProcess struct {
    PID         int      `json:"pid"`
    Name        string   `json:"name"`
    WindowTitle string   `json:"window_title,omitempty"`
    ExePath     string   `json:"exe_path,omitempty"`
    Cwd         string   `json:"cwd,omitempty"`
    Args        []string `json:"args,omitempty"`
    SteamAppID  string   `json:"steam_app_id,omitempty"`
    LutrisSlug  string   `json:"lutris_slug,omitempty"`
    DesktopID   string   `json:"desktop_id,omitempty"`
}
```

2. Update UI/CLI to read pid/name lowercase and keep fallback to PID/Name if needed.
3. Add /proc fixture tests for:
   - cmdline parsing
   - /proc/<pid>/exe symlink
   - /proc/<pid>/cwd symlink
   - environ allowlist parsing
   - SteamAppId/SteamGameId extraction
   - AppId=<digits> cmdline extraction
4. Never expose full environment to the API. Only expose allowlisted IDs.
5. Run:

```bash
go test ./internal/monitor -v
go test ./cmd/picord -v
go test ./...
go vet ./...
```

6. Commit:

```bash
git add internal/monitor internal/profile internal/server/web cmd/picord
git commit -m "feat(monitor): collect game identity hints"
```

### Phase 3: Implement source adapters

Objective: populate catalog from reliable local/public sources.

Files:

- Create: internal/catalog/source.go
- Create: internal/catalog/source_steam.go
- Create: internal/catalog/source_steam_test.go
- Create: internal/catalog/source_lutris.go
- Create: internal/catalog/source_lutris_test.go
- Create: internal/catalog/source_desktop.go
- Create: internal/catalog/source_desktop_test.go
- Create: internal/catalog/testdata/steam/appmanifest_620.acf
- Create: internal/catalog/testdata/lutris_page1.json
- Create: internal/catalog/testdata/desktop/sample.desktop

Source interface:

```go
type RefreshOptions struct {
    MaxPages int
    Offline  bool
}

type Source interface {
    Name() string
    Refresh(ctx context.Context, store *Store, opts RefreshOptions) error
}
```

Tasks:

1. Steam local source:
   - Write ACF parser tests.
   - Parse appmanifest_*.acf appid/name/installdir.
   - Insert Entry source=steam, source_id=appid, title=name.
   - Insert aliases: steam_app_id, title, executable hints if known.
   - Add image_url from Steam CDN candidate, but do not fetch it.
2. Steam appdetails on-demand helper:
   - Use httptest in tests.
   - Fetch for one appid, parse name/header_image.
   - Cache failures in source_state.
3. Lutris public source:
   - Use httptest paginated fixtures.
   - Parse id/name/slug/year/banner_url.
   - Save cursor after each page.
   - Respect MaxPages.
4. Desktop source:
   - Parse .desktop files from configurable roots.
   - Extract Name, Exec basename, Icon, StartupWMClass.
   - Insert application entries and aliases.
5. Run:

```bash
go test ./internal/catalog -run 'Steam|Lutris|Desktop' -v
go test ./...
go vet ./...
```

6. Commit:

```bash
git add internal/catalog
git commit -m "feat(catalog): import Steam Lutris and desktop metadata"
```

### Phase 4: Add image cache and resolver

Objective: store image metadata safely and prepare Discord image resolution.

Files:

- Create: internal/catalog/images.go
- Create: internal/catalog/images_test.go
- Create: internal/rpc/image_mode_test.go if RPC formatting is touched
- Modify: internal/profile/types.go if Activity needs image source metadata
- Modify: cmd/picord/main.go setRichPresence path

Tasks:

1. Implement image cache path:
   - default: $XDG_CACHE_HOME/picord/images
   - fallback: ~/.cache/picord/images
2. Write tests with httptest image responses:
   - accepts image/jpeg and image/png
   - rejects HTML/text
   - enforces max response size
   - deduplicates by SHA256
   - records width/height via image.DecodeConfig
3. Implement ImageResolver:
   - asset_key mode returns entry.DiscordAssetKey or profile activity large_image.
   - generic mode returns cfg.Images.GenericAssetKey or empty string.
   - external_url mode returns entry.ImageURL only if config explicitly enables it and validation flag exists.
4. Add a command later in Phase 8 for live validation; do not enable external_url by default now.
5. Run:

```bash
go test ./internal/catalog -run Image -v
go test ./...
go vet ./...
```

6. Commit:

```bash
git add internal/catalog internal/profile cmd/picord
git commit -m "feat(catalog): cache and resolve catalog images"
```

### Phase 5: Catalog matcher and daemon integration

Objective: generate presence from catalog entries when no user/default profile matches.

Files:

- Create: internal/catalog/matcher.go
- Create: internal/catalog/matcher_test.go
- Modify: cmd/picord/main.go
- Modify: internal/config/config.go
- Modify: internal/config/config_test.go
- Modify: internal/profile/render.go if new variables are added

Matching order:

1. User manual override wins.
2. User/default profile match wins.
3. Catalog exact Steam AppID match.
4. Catalog exact Lutris slug/id match.
5. Catalog exact desktop id match.
6. Catalog executable alias match.
7. Catalog normalized title/window title match with high threshold.
8. No match -> clear presence.

Suggested confidence:

- Steam AppID: 100
- Lutris id/slug: 95
- desktop id: 90
- executable alias: 80
- exact normalized title/window title: 70
- substring window title: 50, only if unique

Tasks:

1. Write matcher tests for each signal and tie-breaker.
2. Implement indexed Store lookup methods used by matcher.
3. Add CatalogConfig/ImageConfig defaults.
4. In runDaemon, open catalog store after config load.
5. In monitor callback, if profileMgr.Match returns nil and catalog enabled, call catalog matcher.
6. Convert catalog Entry to an ephemeral profile/activity:

```go
Details: "Playing {title}"
State: source-specific, e.g. "Steam" or "Lutris"
LargeText: title
LargeImage: resolved image reference
```

7. Add template variables if useful:
   - {title}
   - {source}
   - {steam_app_id}
8. Run:

```bash
go test ./internal/catalog -run Matcher -v
go test ./internal/config -v
go test ./cmd/picord -v
go test ./...
go vet ./...
make build
```

9. Commit:

```bash
git add internal/catalog internal/config internal/profile cmd/picord
git commit -m "feat(catalog): match detected games from local catalog"
```

### Phase 6: API and CLI

Objective: let users inspect and control the catalog.

Files:

- Modify: internal/server/server.go
- Create: internal/server/server_test.go
- Modify: cmd/picord/cli.go
- Create/modify: cmd/picord/cli_test.go if practical

New API endpoints:

- GET /api/catalog/status
- GET /api/catalog/search?q=<query>
- GET /api/catalog/entries/<id>
- POST /api/catalog/refresh with body {source, max_pages}
- POST /api/catalog/profiles/from-entry/<id>
- GET /api/images/<sha256> or /api/catalog/images/<entry_id> for cached previews

New CLI commands:

```bash
picord catalog status
picord catalog search "Hollow Knight"
picord catalog refresh --source steam_local
picord catalog refresh --source lutris_public --max-pages 3
picord profile from-catalog <entry-id>
```

Tasks:

1. Add server tests for status/search/refresh request validation.
2. Add Store injection into server.New or create a server.Options struct.
3. Implement endpoints with clear JSON errors.
4. Add CLI commands with concise terminal output.
5. Run:

```bash
go test ./internal/server -v
go test ./cmd/picord -v
go test ./...
go vet ./...
```

6. Commit:

```bash
git add internal/server cmd/picord
git commit -m "feat(catalog): expose catalog API and CLI"
```

### Phase 7: Web UI

Objective: make the catalog visible and usable.

Files:

- Modify: internal/server/web/index.html
- Modify: internal/server/web/js/app.js
- Modify: internal/server/web/js/api.js if needed
- Modify: internal/server/web/css/style.css

UI additions:

1. Status panel shows Catalog: enabled/disabled, source count, last refresh.
2. Detected Processes table adds:
   - Suggested Title
   - Confidence
   - Source
   - Image preview when cached or directly viewable
   - "Save Profile" button
3. New Catalog section:
   - Search box
   - Results list/cards with title/source/year/image
   - Refresh buttons for steam_local, desktop, lutris_local, and opt-in lutris_public
4. Profile modal adds "Search catalog" helper that fills title, match, image text.
5. Copy must stay user-friendly; avoid backend jargon in UI labels.

Tasks:

1. Add API helpers for catalog endpoints.
2. Render catalog search results from fixture-like API responses manually in browser.
3. Add graceful empty/error states.
4. Ensure existing profile CRUD still works.
5. Run:

```bash
go test ./...
go vet ./...
make build
```

6. Manual verify:
   - Start ./bin/picord --debug run
   - Open http://127.0.0.1:17970
   - Search catalog
   - Refresh local Steam/desktop sources
   - Save a profile from a catalog result

7. Commit:

```bash
git add internal/server/web
git commit -m "feat(ui): add catalog search and suggestions"
```

### Phase 8: Live Discord image validation

Objective: determine what image modes actually work with Discord Local RPC on Linux.

Files:

- Modify: internal/rpc/client.go if needed
- Modify: internal/rpc/client_test.go
- Modify: cmd/picord/cli.go
- Modify: README.md
- Modify: HANDOFF.md

Tasks:

1. Add a debug CLI command:

```bash
picord debug-rpc-image --asset-key picord_game
picord debug-rpc-image --external-url https://cdn.akamai.steamstatic.com/steam/apps/620/header.jpg
```

2. The command should:
   - connect to Discord IPC
   - set a temporary activity
   - log exact payload
   - wait until user kills it or timeout expires
   - clear activity on exit
3. Test payload formatting with mock socket.
4. Manually run with a real Discord client and record results in HANDOFF.md.
5. If external URL mode works, document exact accepted format and add config validation.
6. If external URL mode fails, keep generic/asset_key as default and do not pretend catalog images will appear inside Discord.
7. Run:

```bash
go test ./internal/rpc ./cmd/picord -v
go test ./...
go vet ./...
make build
```

8. Commit:

```bash
git add internal/rpc cmd/picord README.md HANDOFF.md
git commit -m "feat(rpc): add live image validation command"
```

### Phase 9: Background refresh and opt-in full catalog

Objective: support a large title database without blocking startup.

Files:

- Create: internal/catalog/refresher.go
- Create: internal/catalog/refresher_test.go
- Modify: cmd/picord/main.go
- Modify: internal/config/config.go
- Modify: README.md

Tasks:

1. Implement refresher goroutine:
   - only runs when catalog.enabled and catalog.auto_refresh
   - starts with local sources
   - runs public sources only if configured
   - respects refresh_hours
   - saves source_state status/errors
2. Add source-level rate limits.
3. Ensure daemon startup never blocks on network refresh.
4. Add cancellation on cleanup.
5. Run:

```bash
go test ./internal/catalog -run Refresher -v
go test ./...
go vet ./...
make build
```

6. Commit:

```bash
git add internal/catalog internal/config cmd/picord README.md
git commit -m "feat(catalog): refresh metadata in background"
```

### Phase 10: Documentation and packaging polish

Objective: make the feature understandable and safe.

Files:

- Modify: README.md
- Modify: HANDOFF.md
- Maybe create: docs/catalog.md

Tasks:

1. Document the difference between catalog images and Discord image assets.
2. Document source privacy:
   - local /proc env allowlist only
   - no full environment exposed
   - public refresh opt-in for large network sources
3. Document storage paths:
   - config: ~/.config/picord/config.yaml
   - database: ~/.local/share/picord/catalog.db
   - cache: ~/.cache/picord/images
4. Document CLI catalog commands.
5. Update HANDOFF architecture and test counts.
6. Run:

```bash
go test ./...
go vet ./...
make build
git status --short
```

7. Commit:

```bash
git add README.md HANDOFF.md docs
git commit -m "docs: document rich game catalog"
```

---

## 6. Acceptance criteria

Minimum acceptable implementation:

- go test ./... passes.
- go vet ./... passes.
- make build passes.
- Existing manual profiles still work.
- Existing default profiles still work.
- If Discord is started after Picord, Picord eventually connects and can set presence.
- A locally installed Steam game can be identified by Steam AppID without a manual profile.
- The catalog stores at least title, source id, aliases, and image URL for imported entries.
- Image cache downloads only on demand.
- The web UI can search the catalog and save a profile from a result.
- Lutris public refresh is resumable and bounded in tests.
- Discord image mode is explicit. If external URLs are not validated, the UI/docs must say catalog images are previews/cache and Discord uses generic or asset-key images.

Stretch goals after minimum:

- PCGamingWiki/Wikidata enrichment.
- FTS title search.
- Per-game elapsed time timestamps.
- Community override export/import.
- Optional SteamGridDB/IGDB/RAWG enrichers behind user-provided keys.

---

## 7. Verification commands for every phase

Kimi should run these before final response for each committed phase:

```bash
go test ./...
go vet ./...
make build
git status --short --branch
```

If a phase touches web files, manually start the daemon and inspect the page:

```bash
./bin/picord --debug run
# Open http://127.0.0.1:17970
```

If a phase touches source adapters, run a short opt-in refresh, not a full import:

```bash
./bin/picord catalog refresh --source steam_local
./bin/picord catalog refresh --source desktop
./bin/picord catalog refresh --source lutris_public --max-pages 1
```

---

## 8. Commit discipline

Commit after each logical task. Use Conventional Commits.

Examples:

```bash
git commit -m "fix: stabilize profile manager index"
git commit -m "feat(catalog): add local catalog store"
git commit -m "feat(monitor): extract Steam runtime hints"
git commit -m "feat(catalog): import Lutris metadata"
git commit -m "feat(ui): show catalog suggestions"
git commit -m "docs: document catalog image modes"
```

Never leave generated databases, caches, or downloaded images committed. Add ignores for:

```gitignore
*.db
*.db-shm
*.db-wal
.cache/
.local/
dist/
```

---

## 9. Final guidance for Kimi K2.6

Think in this order:

1. Reliable daemon first.
2. Reliable identity hints second.
3. Local installed metadata third.
4. Large public catalogs fourth.
5. Images as URLs/cache fifth.
6. Discord image display only after live validation.

The smart path is not "download every game image." The smart path is: identify the game from high-confidence launcher/runtime hints, store a broad title and image URL catalog locally, cache images lazily, and use a clearly validated Discord asset mode.
