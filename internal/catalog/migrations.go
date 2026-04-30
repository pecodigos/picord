package catalog

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	);`,
	`INSERT OR IGNORE INTO schema_version (version) VALUES (0);`,
	`CREATE TABLE IF NOT EXISTS entries (
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
	);`,
	`CREATE INDEX IF NOT EXISTS idx_entries_normalized_title ON entries(normalized_title);`,
	`CREATE TABLE IF NOT EXISTS aliases (
		entry_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		value TEXT NOT NULL,
		normalized TEXT NOT NULL,
		confidence INTEGER NOT NULL,
		PRIMARY KEY(entry_id, kind, normalized),
		FOREIGN KEY(entry_id) REFERENCES entries(id) ON DELETE CASCADE
	);`,
	`CREATE INDEX IF NOT EXISTS idx_alias_kind_normalized ON aliases(kind, normalized);`,
	`CREATE TABLE IF NOT EXISTS images (
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
	);`,
	`CREATE TABLE IF NOT EXISTS source_state (
		source TEXT PRIMARY KEY,
		cursor TEXT NOT NULL DEFAULT '',
		etag TEXT NOT NULL DEFAULT '',
		updated_at_unix INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT ''
	);`,
}
