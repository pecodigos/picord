package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pecodigos/picord/internal/catalog"
	"github.com/pecodigos/picord/internal/profile"
)

func TestHandleCatalogStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "test:1", Source: "test", SourceID: "1", Kind: catalog.EntryKindGame,
		Title: "Game", NormalizedTitle: catalog.NormalizeTitle("Game"),
	}, nil)

	srv := New(NewAppState(), profile.NewManager(nil, nil), store)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/status", nil)
	rr := httptest.NewRecorder()
	srv.handleCatalogStatus(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", resp["enabled"])
	}
	if resp["entry_count"] != float64(1) {
		t.Errorf("expected entry_count=1, got %v", resp["entry_count"])
	}
}

func TestHandleCatalogSearch(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "test:1", Source: "test", SourceID: "1", Kind: catalog.EntryKindGame,
		Title: "Hollow Knight", NormalizedTitle: catalog.NormalizeTitle("Hollow Knight"),
	}, nil)

	srv := New(NewAppState(), profile.NewManager(nil, nil), store)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/search?q=hollow", nil)
	rr := httptest.NewRecorder()
	srv.handleCatalogSearch(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var results []catalogEntryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Hollow Knight" {
		t.Errorf("expected Hollow Knight, got %+v", results)
	}
}

func TestHandleCatalogSearch_MissingQ(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	srv := New(NewAppState(), profile.NewManager(nil, nil), store)
	req := httptest.NewRequest(http.MethodGet, "/api/catalog/search", nil)
	rr := httptest.NewRecorder()
	srv.handleCatalogSearch(rr, req)

	if rr.Code != 400 {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleCatalogEntry(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "test:1", Source: "test", SourceID: "1", Kind: catalog.EntryKindGame,
		Title: "Portal 2", NormalizedTitle: catalog.NormalizeTitle("Portal 2"),
	}, nil)

	srv := New(NewAppState(), profile.NewManager(nil, nil), store)
	req := httptest.NewRequest(http.MethodGet, "/api/catalog/entries/test:1", nil)
	req.RequestURI = "/api/catalog/entries/test:1"
	req.URL.Path = "/api/catalog/entries/test:1"
	rr := httptest.NewRecorder()
	srv.handleCatalogEntry(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var entry catalogEntryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry.Title != "Portal 2" {
		t.Errorf("expected Portal 2, got %q", entry.Title)
	}
}

func TestHandleCatalogRefresh(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Create a fake applications dir with one desktop file.
	appsDir := filepath.Join(dir, "applications")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		t.Fatal(err)
	}
	desktopSrc := filepath.Join("..", "catalog", "testdata", "desktop", "sample.desktop")
	desktopData, err := os.ReadFile(desktopSrc)
	if err != nil {
		t.Fatalf("read testdata desktop: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appsDir, "sample.desktop"), desktopData, 0644); err != nil {
		t.Fatal(err)
	}

	srv := New(NewAppState(), profile.NewManager(nil, nil), store)
	body, _ := json.Marshal(map[string]any{"source": "desktop", "roots": []string{appsDir}})
	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleCatalogRefresh(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	entry, err := store.GetEntry(context.Background(), "desktop:sample")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry == nil || entry.Title != "Sample Application" {
		t.Errorf("expected Sample Application entry after refresh, got %+v", entry)
	}
}

func TestHandleCatalogProfileFromEntry(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "test:1", Source: "test", SourceID: "1", Kind: catalog.EntryKindGame,
		Title: "Doom Eternal", NormalizedTitle: catalog.NormalizeTitle("Doom Eternal"),
	}, nil)

	pm := profile.NewManager(nil, nil)
	srv := New(NewAppState(), pm, store)
	req := httptest.NewRequest(http.MethodPost, "/api/catalog/profiles/from-entry/test:1", nil)
	req.RequestURI = "/api/catalog/profiles/from-entry/test:1"
	req.URL.Path = "/api/catalog/profiles/from-entry/test:1"
	rr := httptest.NewRecorder()
	srv.handleCatalogProfileFromEntry(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	p := pm.Get("Doom Eternal")
	if p == nil {
		t.Fatal("expected Doom Eternal profile to be created")
	}
	if p.Activity.LargeText != "Doom Eternal" {
		t.Errorf("large_text=%q, want Doom Eternal", p.Activity.LargeText)
	}
}

func TestSecurity_RejectCrossOriginPOST(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	srv.SetToken("test-token")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/override", nil)
	req.Header.Set("Origin", "https://evil.com")
	req.Header.Set("X-Picord-Token", "test-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for cross-origin POST, got %d", rr.Code)
	}
}

func TestSecurity_AllowLocalOriginPOSTWithToken(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	srv.SetToken("test-token")
	handler := srv.Handler()

	body := []byte(`{"name":"test","activity":{"details":"test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/override", bytes.NewReader(body))
	req.Header.Set("Origin", "http://localhost:17970")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Picord-Token", "test-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Errorf("expected local origin POST with token to be allowed, got 403")
	}
}

func TestSecurity_RejectMissingToken(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	srv.SetToken("test-token")
	handler := srv.Handler()

	body := []byte(`{"name":"test","activity":{"details":"test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/override", bytes.NewReader(body))
	req.Header.Set("Origin", "http://localhost:17970")
	req.Header.Set("Content-Type", "application/json")
	// No X-Picord-Token header.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing token, got %d", rr.Code)
	}
}

func TestSecurity_RejectWrongToken(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	srv.SetToken("test-token")
	handler := srv.Handler()

	body := []byte(`{"name":"test","activity":{"details":"test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/override", bytes.NewReader(body))
	req.Header.Set("Origin", "http://localhost:17970")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Picord-Token", "wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for wrong token, got %d", rr.Code)
	}
}

func TestSecurity_RejectBadContentType(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	srv.SetToken("test-token")
	handler := srv.Handler()

	body := []byte(`{"name":"test","activity":{"details":"test"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/override", bytes.NewReader(body))
	req.Header.Set("Origin", "http://localhost:17970")
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Picord-Token", "test-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for bad content-type, got %d", rr.Code)
	}
}

func TestSecurity_GETWithoutTokenAllowed(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	srv.SetToken("test-token")
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Origin", "http://localhost:17970")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Errorf("expected GET without token to be allowed, got 403")
	}
}
