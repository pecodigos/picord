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

func TestLutrisPublicSource_ResumesFromCursor(t *testing.T) {
	callCount := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		if page == "1" {
			resp := fmt.Sprintf(`{"count":4,"next":"%s?page=2","previous":null,"results":[{"id":1,"name":"Game 1","slug":"game-1","year":2020,"banner_url":""}]}`, server.URL)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(resp))
		} else if page == "2" {
			resp := `{"count":4,"next":"","previous":null,"results":[{"id":2,"name":"Game 2","slug":"game-2","year":2020,"banner_url":""}]}`
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(resp))
		}
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

	// First run: fetch page 1 only.
	if err := src.Refresh(ctx, store, RefreshOptions{MaxPages: 1}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second run: should resume from cursor (page 2).
	if err := src.Refresh(ctx, store, RefreshOptions{MaxPages: 10}); err != nil {
		t.Fatalf("Refresh resume failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls total, got %d", callCount)
	}

	count, _ := store.CountEntries(ctx)
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}

	// After completion, cursor should be empty.
	cursor, _, _, _, _ := store.GetSourceState(ctx, src.Name())
	if cursor != "" {
		t.Errorf("expected empty cursor after completion, got %q", cursor)
	}
}

func TestLutrisPublicSource_ETag304(t *testing.T) {
	callCount := 0
	etag := `"abc123"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		resp := `{"count":1,"next":"","previous":null,"results":[{"id":1,"name":"Game 1","slug":"game-1","year":2020,"banner_url":""}]}`
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", etag)
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

	// First refresh.
	if err := src.Refresh(ctx, store, RefreshOptions{MaxPages: 1}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Second refresh with same ETag should get 304 and skip.
	if err := src.Refresh(ctx, store, RefreshOptions{MaxPages: 1}); err != nil {
		t.Fatalf("Refresh with ETag failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls total, got %d", callCount)
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
