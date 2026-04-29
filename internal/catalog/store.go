package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetConnMaxLifetime(0)
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) migrate() error {
	for _, stmt := range migrations {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

func (s *Store) UpsertEntry(ctx context.Context, e Entry, aliases []Alias) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO entries (id, source, source_id, kind, title, normalized_title, release_year, image_url, image_kind, discord_asset_key, updated_at_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source, source_id) DO UPDATE SET
			kind = excluded.kind,
			title = excluded.title,
			normalized_title = excluded.normalized_title,
			release_year = excluded.release_year,
			image_url = excluded.image_url,
			image_kind = excluded.image_kind,
			discord_asset_key = excluded.discord_asset_key,
			updated_at_unix = excluded.updated_at_unix
	`, e.ID, e.Source, e.SourceID, string(e.Kind), e.Title, e.NormalizedTitle, e.ReleaseYear, e.ImageURL, e.ImageKind, e.DiscordAssetKey, e.UpdatedAt.Unix())
	if err != nil {
		return fmt.Errorf("upsert entry: %w", err)
	}

	// Always remove old aliases for this entry so we can replace them.
	_, err = tx.ExecContext(ctx, `DELETE FROM aliases WHERE entry_id = ?`, e.ID)
	if err != nil {
		return fmt.Errorf("delete old aliases: %w", err)
	}
	if len(aliases) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO aliases (entry_id, kind, value, normalized, confidence)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(entry_id, kind, normalized) DO UPDATE SET
				value = excluded.value,
				confidence = excluded.confidence
		`)
		if err != nil {
			return fmt.Errorf("prepare alias insert: %w", err)
		}
		defer stmt.Close()
		for _, a := range aliases {
			if _, err := stmt.ExecContext(ctx, a.EntryID, string(a.Kind), a.Value, a.Normalized, a.Confidence); err != nil {
				return fmt.Errorf("insert alias: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *Store) GetEntry(ctx context.Context, id string) (*Entry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, source, source_id, kind, title, normalized_title, release_year, image_url, image_kind, discord_asset_key, updated_at_unix
		FROM entries WHERE id = ?
	`, id)

	var e Entry
	var ts int64
	if err := row.Scan(&e.ID, &e.Source, &e.SourceID, &e.Kind, &e.Title, &e.NormalizedTitle, &e.ReleaseYear, &e.ImageURL, &e.ImageKind, &e.DiscordAssetKey, &ts); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan entry: %w", err)
	}
	e.UpdatedAt = time.Unix(ts, 0)
	return &e, nil
}

func (s *Store) SearchByAlias(ctx context.Context, kind AliasKind, value string) ([]Entry, error) {
	normalized := NormalizeTitle(value)
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.source, e.source_id, e.kind, e.title, e.normalized_title, e.release_year, e.image_url, e.image_kind, e.discord_asset_key, e.updated_at_unix
		FROM entries e
		JOIN aliases a ON e.id = a.entry_id
		WHERE a.kind = ? AND a.normalized = ?
		ORDER BY a.confidence DESC
	`, string(kind), normalized)
	if err != nil {
		return nil, fmt.Errorf("query alias: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (s *Store) SearchTitlePrefix(ctx context.Context, prefix string) ([]Entry, error) {
	normalized := NormalizeTitle(prefix)
	like := normalized + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, source_id, kind, title, normalized_title, release_year, image_url, image_kind, discord_asset_key, updated_at_unix
		FROM entries
		WHERE normalized_title LIKE ?
		ORDER BY title
		LIMIT 100
	`, like)
	if err != nil {
		return nil, fmt.Errorf("query title prefix: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var entries []Entry
	for rows.Next() {
		var e Entry
		var ts int64
		if err := rows.Scan(&e.ID, &e.Source, &e.SourceID, &e.Kind, &e.Title, &e.NormalizedTitle, &e.ReleaseYear, &e.ImageURL, &e.ImageKind, &e.DiscordAssetKey, &ts); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		e.UpdatedAt = time.Unix(ts, 0)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return entries, nil
}

func (s *Store) GetSourceState(ctx context.Context, source string) (cursor, etag string, updatedAt time.Time, lastError string, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT cursor, etag, updated_at_unix, last_error FROM source_state WHERE source = ?`, source)
	var ts int64
	err = row.Scan(&cursor, &etag, &ts, &lastError)
	if err == sql.ErrNoRows {
		return "", "", time.Time{}, "", nil
	}
	if err != nil {
		return "", "", time.Time{}, "", fmt.Errorf("scan source state: %w", err)
	}
	return cursor, etag, time.Unix(ts, 0), lastError, nil
}

func (s *Store) SetSourceState(ctx context.Context, source, cursor, etag, lastError string, updatedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO source_state (source, cursor, etag, updated_at_unix, last_error)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(source) DO UPDATE SET
			cursor = excluded.cursor,
			etag = excluded.etag,
			updated_at_unix = excluded.updated_at_unix,
			last_error = excluded.last_error
	`, source, cursor, etag, updatedAt.Unix(), lastError)
	if err != nil {
		return fmt.Errorf("upsert source state: %w", err)
	}
	return nil
}

// ExactTitleMatch returns entries whose normalized title exactly matches the query.
func (s *Store) ExactTitleMatch(ctx context.Context, title string) ([]Entry, error) {
	normalized := NormalizeTitle(title)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, source_id, kind, title, normalized_title, release_year, image_url, image_kind, discord_asset_key, updated_at_unix
		FROM entries
		WHERE normalized_title = ?
	`, normalized)
	if err != nil {
		return nil, fmt.Errorf("query exact title: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// SearchAll returns entries matching a query across titles and aliases.
func (s *Store) SearchAll(ctx context.Context, query string) ([]Entry, error) {
	normalized := NormalizeTitle(query)
	if normalized == "" {
		return nil, nil
	}
	like := "%" + normalized + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source, source_id, kind, title, normalized_title, release_year, image_url, image_kind, discord_asset_key, updated_at_unix
		FROM entries
		WHERE normalized_title LIKE ?
		ORDER BY title
		LIMIT 100
	`, like)
	if err != nil {
		return nil, fmt.Errorf("query search all: %w", err)
	}
	defer rows.Close()

	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}

	// Also search aliases and union results.
	aliasRows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.source, e.source_id, e.kind, e.title, e.normalized_title, e.release_year, e.image_url, e.image_kind, e.discord_asset_key, e.updated_at_unix
		FROM entries e
		JOIN aliases a ON e.id = a.entry_id
		WHERE a.normalized LIKE ?
		ORDER BY e.title
		LIMIT 100
	`, like)
	if err != nil {
		return nil, fmt.Errorf("query alias search: %w", err)
	}
	defer aliasRows.Close()

	aliasEntries, err := scanEntries(aliasRows)
	if err != nil {
		return nil, err
	}

	// Deduplicate by ID, preferring earlier results.
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.ID] = true
	}
	for _, e := range aliasEntries {
		if !seen[e.ID] {
			seen[e.ID] = true
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (s *Store) CountEntries(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count entries: %w", err)
	}
	return n, nil
}

func (s *Store) CountAliases(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM aliases`)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("count aliases: %w", err)
	}
	return n, nil
}
