package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLutrisPublicSource_Refresh(t *testing.T) {
	page1, err := os.ReadFile(filepath.Join("testdata", "lutris_page1.json"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(page1)
	}))
	defer server.Close()

	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	src := &LutrisPublicSource{BaseURL: server.URL}
	ctx := context.Background()
	if err := src.Refresh(ctx, store, RefreshOptions{MaxPages: 1}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	count, err := store.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}

	results, err := store.SearchByAlias(ctx, AliasLutrisSlug, "hollow-knight")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Hollow Knight" {
		t.Errorf("expected Hollow Knight, got %+v", results)
	}

	// Verify source state was saved.
	cursor, _, _, lastError, err := store.GetSourceState(ctx, src.Name())
	if err != nil {
		t.Fatalf("GetSourceState failed: %v", err)
	}
	if cursor == "" {
		t.Error("expected cursor to be saved")
	}
	if lastError != "" {
		t.Errorf("expected no error, got %q", lastError)
	}
}

func TestLutrisPublicSource_RespectsMaxPages(t *testing.T) {
	callCount := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := fmt.Sprintf(`{"count":10,"next":"%s?page=%d","previous":null,"results":[{"id":%d,"name":"Game %d","slug":"game-%d","year":2020,"banner_url":""}]}`, server.URL, callCount+1, callCount, callCount, callCount)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	src := &LutrisPublicSource{BaseURL: server.URL}
	ctx := context.Background()
	if err := src.Refresh(ctx, store, RefreshOptions{MaxPages: 3}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	if callCount != 3 {
		t.Errorf("expected 3 API calls, got %d", callCount)
	}

	count, err := store.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 entries, got %d", count)
	}
}

func TestLutrisPublicSource_OfflineSkips(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	src := &LutrisPublicSource{}
	ctx := context.Background()
	if err := src.Refresh(ctx, store, RefreshOptions{Offline: true}); err != nil {
		t.Fatalf("Refresh offline should not error: %v", err)
	}

	count, err := store.CountEntries(ctx)
	if err != nil {
		t.Fatalf("CountEntries failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 entries when offline, got %d", count)
	}
}
