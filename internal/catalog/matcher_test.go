package catalog

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pecodigos/picord/internal/profile"
)

func TestMatcher_SteamAppID(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, Entry{
		ID: "steam:620", Source: "steam", SourceID: "620", Kind: EntryKindGame,
		Title: "Portal 2", NormalizedTitle: NormalizeTitle("Portal 2"),
	}, []Alias{
		{EntryID: "steam:620", Kind: AliasSteamAppID, Value: "620", Normalized: "620", Confidence: 100},
	})

	m := NewMatcher(store)
	result := m.Match(ctx, profile.DetectedProcess{Name: "portal2", SteamAppID: "620"})
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Entry.Title != "Portal 2" {
		t.Errorf("title=%q, want Portal 2", result.Entry.Title)
	}
	if result.Confidence != 100 {
		t.Errorf("confidence=%d, want 100", result.Confidence)
	}
}

func TestMatcher_LutrisSlug(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, Entry{
		ID: "lutris:1", Source: "lutris", SourceID: "1", Kind: EntryKindGame,
		Title: "Hollow Knight", NormalizedTitle: NormalizeTitle("Hollow Knight"),
	}, []Alias{
		{EntryID: "lutris:1", Kind: AliasLutrisSlug, Value: "hollow-knight", Normalized: NormalizeTitle("hollow-knight"), Confidence: 95},
	})

	m := NewMatcher(store)
	result := m.Match(ctx, profile.DetectedProcess{Name: "hollow_knight", LutrisSlug: "hollow-knight"})
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Entry.Title != "Hollow Knight" {
		t.Errorf("title=%q, want Hollow Knight", result.Entry.Title)
	}
	if result.Confidence != 95 {
		t.Errorf("confidence=%d, want 95", result.Confidence)
	}
}

func TestMatcher_Executable(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, Entry{
		ID: "desktop:firefox", Source: "desktop", SourceID: "firefox", Kind: EntryKindApplication,
		Title: "Firefox", NormalizedTitle: NormalizeTitle("Firefox"),
	}, []Alias{
		{EntryID: "desktop:firefox", Kind: AliasExecutable, Value: "firefox", Normalized: NormalizeTitle("firefox"), Confidence: 70},
	})

	m := NewMatcher(store)
	result := m.Match(ctx, profile.DetectedProcess{Name: "firefox"})
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Entry.Title != "Firefox" {
		t.Errorf("title=%q, want Firefox", result.Entry.Title)
	}
	if result.Confidence != 80 {
		t.Errorf("confidence=%d, want 80", result.Confidence)
	}
}

func TestMatcher_ExactTitle(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, Entry{
		ID: "test:1", Source: "test", SourceID: "1", Kind: EntryKindGame,
		Title: "Doom Eternal", NormalizedTitle: NormalizeTitle("Doom Eternal"),
	}, nil)

	m := NewMatcher(store)
	result := m.Match(ctx, profile.DetectedProcess{Name: "doom"})
	if result != nil {
		t.Fatal("expected no match for partial name")
	}

	result = m.Match(ctx, profile.DetectedProcess{Name: "Doom Eternal"})
	if result == nil {
		t.Fatal("expected exact title match")
	}
	if result.Confidence != 70 {
		t.Errorf("confidence=%d, want 70", result.Confidence)
	}
}

func TestMatcher_NoMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	m := NewMatcher(store)
	result := m.Match(context.Background(), profile.DetectedProcess{Name: "unknown"})
	if result != nil {
		t.Fatal("expected no match")
	}
}

func TestMatchResult_ToProfile(t *testing.T) {
	mr := &MatchResult{
		Entry: Entry{
			Title:    "Portal 2",
			Source:   "steam",
			Kind:     EntryKindGame,
			ImageURL: "https://example.com/img.jpg",
		},
		Confidence: 100,
		Reason:     "steam_app_id",
	}

	resolver := ImageResolver{Mode: ImageModeGeneric, GenericAssetKey: "picord"}
	p := mr.ToProfile(resolver)
	if p.Name != "Portal 2" {
		t.Errorf("name=%q, want Portal 2", p.Name)
	}
	if p.Activity.Details != "Playing Portal 2" {
		t.Errorf("details=%q, want Playing Portal 2", p.Activity.Details)
	}
	// ImageModeGeneric now smartly falls back to a valid external URL when available.
	if p.Activity.LargeImage != "https://example.com/img.jpg" {
		t.Errorf("large_image=%q, want https://example.com/img.jpg", p.Activity.LargeImage)
	}
}
