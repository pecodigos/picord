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

func TestMatcher_NumericAliasAsSteamAppID(t *testing.T) {
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
	// Process with numeric alias but no explicit SteamAppID field
	result := m.Match(ctx, profile.DetectedProcess{Name: "wine", Aliases: []string{"620", "portal2"}})
	if result == nil {
		t.Fatal("expected match from numeric alias as SteamAppID")
	}
	if result.Entry.Title != "Portal 2" {
		t.Errorf("title=%q, want Portal 2", result.Entry.Title)
	}
	if result.Confidence != 95 {
		t.Errorf("confidence=%d, want 95", result.Confidence)
	}
}

func TestMatcher_RejectsSignedSteamAppIDs(t *testing.T) {
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
	for _, proc := range []profile.DetectedProcess{
		{Name: "wine", SteamAppID: "+620"},
		{Name: "wine", SteamAppID: "-620"},
		{Name: "wine", Aliases: []string{"+620"}},
		{Name: "wine", Aliases: []string{"-620"}},
	} {
		if got := m.Match(ctx, proc); got != nil {
			t.Fatalf("expected no match for signed Steam AppID %+v, got %+v", proc, got)
		}
	}
}

func TestMatcher_EvaluatesAllAliasCandidatesForTieBreak(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Insert the lower-priority source first. The matcher must still consider the
	// later Steam candidate instead of accepting SearchByAlias()[0].
	_ = store.UpsertEntry(ctx, Entry{
		ID: "desktop:portal2", Source: "desktop", SourceID: "portal2", Kind: EntryKindGame,
		Title: "Portal 2 Launcher", NormalizedTitle: NormalizeTitle("Portal 2 Launcher"),
	}, []Alias{
		{EntryID: "desktop:portal2", Kind: AliasExecutable, Value: "portal2", Normalized: NormalizeTitle("portal2"), Confidence: 80},
	})
	_ = store.UpsertEntry(ctx, Entry{
		ID: "steam:620", Source: "steam", SourceID: "620", Kind: EntryKindGame,
		Title: "Portal 2", NormalizedTitle: NormalizeTitle("Portal 2"),
	}, []Alias{
		{EntryID: "steam:620", Kind: AliasExecutable, Value: "portal2", Normalized: NormalizeTitle("portal2"), Confidence: 80},
	})

	m := NewMatcher(store)
	result := m.Match(ctx, profile.DetectedProcess{Name: "portal2"})
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Entry.ID != "steam:620" {
		t.Fatalf("expected Steam entry to win source-priority tie, got %+v", result.Entry)
	}
}

func TestMatcher_AliasBeatsExecutable(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Entry A matches by executable at confidence 80
	_ = store.UpsertEntry(ctx, Entry{
		ID: "desktop:steam", Source: "desktop", SourceID: "steam", Kind: EntryKindApplication,
		Title: "Steam", NormalizedTitle: NormalizeTitle("Steam"),
	}, []Alias{
		{EntryID: "desktop:steam", Kind: AliasExecutable, Value: "steam", Normalized: NormalizeTitle("steam"), Confidence: 70},
	})
	// Entry B matches by alias at confidence 85
	_ = store.UpsertEntry(ctx, Entry{
		ID: "steam:620", Source: "steam", SourceID: "620", Kind: EntryKindGame,
		Title: "Portal 2", NormalizedTitle: NormalizeTitle("Portal 2"),
	}, []Alias{
		{EntryID: "steam:620", Kind: AliasExecutable, Value: "portal2", Normalized: NormalizeTitle("portal2"), Confidence: 85},
	})

	m := NewMatcher(store)
	// Process name matches "steam" at 80, but alias "portal2" matches at 85
	result := m.Match(ctx, profile.DetectedProcess{Name: "steam", Aliases: []string{"portal2"}})
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Entry.Title != "Portal 2" {
		t.Errorf("expected alias match to win, got title=%q", result.Entry.Title)
	}
	if result.Confidence != 85 {
		t.Errorf("expected confidence=85, got %d", result.Confidence)
	}
}

func TestMatcher_SteamAppIDBeatsAll(t *testing.T) {
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
		{EntryID: "steam:620", Kind: AliasExecutable, Value: "portal2", Normalized: NormalizeTitle("portal2"), Confidence: 85},
	})

	m := NewMatcher(store)
	result := m.Match(ctx, profile.DetectedProcess{Name: "portal2", SteamAppID: "620", Aliases: []string{"portal2"}})
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Confidence != 100 {
		t.Errorf("expected SteamAppID confidence=100 to win, got %d", result.Confidence)
	}
	if result.Reason != "steam_app_id" {
		t.Errorf("expected reason=steam_app_id, got %q", result.Reason)
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
