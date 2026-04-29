package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hollow Knight", "hollow knight"},
		{"Hollow Knight™", "hollow knight"},
		{"DOOM: The Dark Ages", "doom the dark ages"},
		{"  Multiple   Spaces  ", "multiple spaces"},
		{"Café Noir!!!", "café noir"},
		{"", ""},
		{"!!!", ""},
		{"A-B-C", "a b c"},
	}

	for _, tt := range tests {
		got := NormalizeTitle(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeTitle(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestStore_MigrationCreatesTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "catalog.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Verify tables exist by running a simple query.
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='entries'`).Scan(&n); err != nil {
		t.Fatalf("checking entries table: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected entries table to exist")
	}
}

func TestStore_UpsertAndGetEntry(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	e := Entry{
		ID:              "steam:620",
		Source:          "steam",
		SourceID:        "620",
		Kind:            EntryKindGame,
		Title:           "Portal 2",
		NormalizedTitle: NormalizeTitle("Portal 2"),
		ReleaseYear:     2011,
		ImageURL:        "https://cdn.akamai.steamstatic.com/steam/apps/620/header.jpg",
		ImageKind:       "steam_header",
		UpdatedAt:       time.Now(),
	}

	aliases := []Alias{
		{EntryID: e.ID, Kind: AliasTitle, Value: "Portal 2", Normalized: NormalizeTitle("Portal 2"), Confidence: 100},
		{EntryID: e.ID, Kind: AliasSteamAppID, Value: "620", Normalized: "620", Confidence: 100},
	}

	if err := store.UpsertEntry(ctx, e, aliases); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}

	got, err := store.GetEntry(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry to exist")
	}
	if got.Title != e.Title {
		t.Errorf("title mismatch: got %q, want %q", got.Title, e.Title)
	}
	if got.SourceID != e.SourceID {
		t.Errorf("source_id mismatch: got %q, want %q", got.SourceID, e.SourceID)
	}

	// Verify alias count.
	aliasCount, err := store.CountAliases(ctx)
	if err != nil {
		t.Fatalf("CountAliases failed: %v", err)
	}
	if aliasCount != 2 {
		t.Errorf("expected 2 aliases, got %d", aliasCount)
	}
}

func TestStore_SearchByAlias(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	e := Entry{
		ID:              "lutris:hollow-knight",
		Source:          "lutris",
		SourceID:        "hollow-knight",
		Kind:            EntryKindGame,
		Title:           "Hollow Knight",
		NormalizedTitle: NormalizeTitle("Hollow Knight"),
		UpdatedAt:       time.Now(),
	}
	aliases := []Alias{
		{EntryID: e.ID, Kind: AliasLutrisSlug, Value: "hollow-knight", Normalized: NormalizeTitle("hollow-knight"), Confidence: 95},
		{EntryID: e.ID, Kind: AliasTitle, Value: "Hollow Knight", Normalized: NormalizeTitle("Hollow Knight"), Confidence: 90},
	}

	if err := store.UpsertEntry(ctx, e, aliases); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}

	results, err := store.SearchByAlias(ctx, AliasLutrisSlug, "hollow-knight")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Hollow Knight" {
		t.Errorf("expected Hollow Knight, got %q", results[0].Title)
	}
}

func TestStore_SearchTitlePrefix(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	entries := []Entry{
		{ID: "a", Source: "test", SourceID: "1", Kind: EntryKindGame, Title: "Alpha", NormalizedTitle: NormalizeTitle("Alpha"), UpdatedAt: time.Now()},
		{ID: "b", Source: "test", SourceID: "2", Kind: EntryKindGame, Title: "Beta", NormalizedTitle: NormalizeTitle("Beta"), UpdatedAt: time.Now()},
		{ID: "ab", Source: "test", SourceID: "3", Kind: EntryKindGame, Title: "Alphabet", NormalizedTitle: NormalizeTitle("Alphabet"), UpdatedAt: time.Now()},
	}
	for _, e := range entries {
		if err := store.UpsertEntry(ctx, e, nil); err != nil {
			t.Fatalf("UpsertEntry failed: %v", err)
		}
	}

	results, err := store.SearchTitlePrefix(ctx, "Alp")
	if err != nil {
		t.Fatalf("SearchTitlePrefix failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Should include Alpha and Alphabet.
	titles := make(map[string]bool)
	for _, r := range results {
		titles[r.Title] = true
	}
	if !titles["Alpha"] || !titles["Alphabet"] {
		t.Errorf("expected Alpha and Alphabet, got %+v", titles)
	}
}

func TestStore_ExactTitleMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.UpsertEntry(ctx, Entry{
		ID: "e1", Source: "test", SourceID: "1", Kind: EntryKindGame,
		Title: "DOOM Eternal", NormalizedTitle: NormalizeTitle("DOOM Eternal"),
		UpdatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}

	results, err := store.ExactTitleMatch(ctx, "DOOM Eternal")
	if err != nil {
		t.Fatalf("ExactTitleMatch failed: %v", err)
	}
	if len(results) != 1 || results[0].Title != "DOOM Eternal" {
		t.Errorf("expected DOOM Eternal, got %+v", results)
	}
}

func TestStore_SourceState(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	if err := store.SetSourceState(ctx, "lutris", "page3", "etag123", "", now); err != nil {
		t.Fatalf("SetSourceState failed: %v", err)
	}

	cursor, etag, updatedAt, lastError, err := store.GetSourceState(ctx, "lutris")
	if err != nil {
		t.Fatalf("GetSourceState failed: %v", err)
	}
	if cursor != "page3" {
		t.Errorf("cursor=%q, want page3", cursor)
	}
	if etag != "etag123" {
		t.Errorf("etag=%q, want etag123", etag)
	}
	if !updatedAt.Equal(now) {
		t.Errorf("updatedAt=%v, want %v", updatedAt, now)
	}
	if lastError != "" {
		t.Errorf("lastError=%q, want empty", lastError)
	}
}
