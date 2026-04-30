package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestEnricher_EnrichMissingImages(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/autocomplete/Celeste":
			w.Write([]byte(`{"success":true,"data":[{"id":5251694,"name":"Celeste","release_date":0}]}`))
		case "/grids/game/5251694":
			w.Write([]byte(`{"success":true,"data":[{"id":1,"url":"https://cdn2.steamgriddb.com/grid/abc.png","thumb":"","width":600,"height":900,"style":"alternate","mime":"image/png"}]}`))
		case "/search/autocomplete/NoArt":
			w.Write([]byte(`{"success":true,"data":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Insert entries with and without images.
	if err := store.UpsertEntry(ctx, Entry{
		ID: "e1", Source: "test", SourceID: "1", Kind: EntryKindGame,
		Title: "Celeste", NormalizedTitle: NormalizeTitle("Celeste"),
		ImageURL: "", UpdatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}
	if err := store.UpsertEntry(ctx, Entry{
		ID: "e2", Source: "test", SourceID: "2", Kind: EntryKindGame,
		Title: "NoArt", NormalizedTitle: NormalizeTitle("NoArt"),
		ImageURL: "", UpdatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}
	if err := store.UpsertEntry(ctx, Entry{
		ID: "e3", Source: "test", SourceID: "3", Kind: EntryKindGame,
		Title: "HasImage", NormalizedTitle: NormalizeTitle("HasImage"),
		ImageURL: "http://example.com/img.jpg", UpdatedAt: time.Now(),
	}, nil); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}

	client := NewSteamGridDBClient("test-key")
	client.BaseURL = ts.URL
	enricher := &Enricher{Store: store, Client: client}

	n, err := enricher.EnrichMissingImages(ctx, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("enriched = %d, want 1", n)
	}

	got, err := store.GetEntry(ctx, "e1")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if got.ImageURL != "https://cdn2.steamgriddb.com/grid/abc.png" {
		t.Errorf("image_url = %q", got.ImageURL)
	}
	if got.ImageKind != "steamgriddb" {
		t.Errorf("image_kind = %q", got.ImageKind)
	}
}

func TestEnricher_Disabled(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	enricher := NewEnricher(store, "")
	if enricher.Enabled() {
		t.Error("expected enricher to be disabled with empty key")
	}
	_, err = enricher.EnrichMissingImages(context.Background(), 10)
	if err == nil {
		t.Error("expected error for disabled enricher")
	}
}
