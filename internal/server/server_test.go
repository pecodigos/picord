package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pecodigos/picord/internal/catalog"
	"github.com/pecodigos/picord/internal/config"
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

func TestHandleCatalogRefreshAcceptsSteamShortcuts(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	srv := New(NewAppState(), profile.NewManager(nil, nil), store)
	body, _ := json.Marshal(map[string]any{
		"source": "steam_shortcuts",
		"roots":  []string{filepath.Join(dir, "missing-shortcuts.vdf")},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleCatalogRefresh(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected steam_shortcuts refresh to be accepted, got %d: %s", rr.Code, rr.Body.String())
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

func TestHandleSettingsReturnsConfigAndMasksSecret(t *testing.T) {
	state := NewAppState()
	state.SetAutoDetect(false)
	srv := New(state, profile.NewManager(nil, nil), nil)
	srv.SetSettingsProvider(func() config.AppConfig {
		cfg := config.DefaultConfig()
		cfg.ScanAllProcesses = false
		cfg.ShowTrayIcon = false
		cfg.Catalog.SteamGridDBAPIKey = "secret-key"
		cfg.Detection.ShowTools = false
		return cfg
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr := httptest.NewRecorder()
	srv.handleSettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp settingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AutoDetect {
		t.Error("expected auto_detect=false")
	}
	if resp.ScanAllProcesses {
		t.Error("expected scan_all_processes=false")
	}
	if resp.ShowTrayIcon {
		t.Error("expected show_tray_icon=false")
	}
	if resp.Detection.ShowTools {
		t.Error("expected show_tools=false")
	}
	if resp.Catalog.SteamGridDBAPIKey != "" {
		t.Fatalf("expected API key to be masked, got %q", resp.Catalog.SteamGridDBAPIKey)
	}
}

func TestHandleSettingsPutPersistsConfigAndAutoDetect(t *testing.T) {
	state := NewAppState()
	saved := config.DefaultConfig()
	srv := New(state, profile.NewManager(nil, nil), nil)
	srv.SetSettingsProvider(func() config.AppConfig { return saved })
	srv.OnSettingsSaved = func(cfg config.AppConfig) error {
		saved = cfg
		return nil
	}
	srv.OnAutoDetectSet = func(v bool) { state.SetAutoDetect(v) }

	body, _ := json.Marshal(map[string]any{
		"auto_detect":        false,
		"scan_all_processes": false,
		"detection": map[string]bool{
			"show_games": true,
			"show_tools": false,
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.handleSettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if state.AutoDetectEnabled() {
		t.Error("expected auto-detect to be disabled")
	}
	if saved.ScanAllProcesses {
		t.Error("expected scan_all_processes to be persisted false")
	}
	if saved.Detection.ShowTools {
		t.Error("expected show_tools to be persisted false")
	}
}

func TestHandleProfileByIDCanDisableProfile(t *testing.T) {
	pm := profile.NewManager(nil, nil)
	pm.Add(profile.Profile{Name: "Tool", Match: profile.MatchRule{Type: profile.MatchProcessName, Value: "tool"}, Priority: 5})
	srv := New(NewAppState(), pm, nil)

	body, _ := json.Marshal(profile.Profile{
		Name:     "Tool",
		Enabled:  false,
		Match:    profile.MatchRule{Type: profile.MatchProcessName, Value: "tool"},
		Priority: 5,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/profiles/Tool", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.handleProfileByID(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	p := pm.Get("Tool")
	if p == nil {
		t.Fatal("expected profile to remain addressable")
	}
	if p.Enabled {
		t.Error("expected profile enabled=false to be preserved")
	}
}

func TestHandleStatusVerboseGatesIdentityFields(t *testing.T) {
	state := NewAppState()
	state.SetScanSnapshot(ScanSnapshot{
		Procs: []profile.DetectedProcess{{
			PID:        123,
			Name:       "wine",
			SteamAppID: "620",
			DesktopID:  "portal2.desktop",
			Aliases:    []string{"Portal 2", "portal2.exe"},
			ExePath:    "/home/user/private/portal2.exe",
			Cwd:        "/home/user/private",
			Args:       []string{"portal2.exe", "--token=secret"},
		}},
		State: ScanStateScanned,
		Mode:  ScanModeAll,
	})
	srv := New(state, profile.NewManager(nil, nil), nil)

	for _, tc := range []struct {
		name    string
		url     string
		verbose bool
	}{
		{name: "default", url: "/api/status", verbose: false},
		{name: "verbose", url: "/api/status?verbose=1", verbose: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rr := httptest.NewRecorder()
			srv.handleStatus(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			var resp statusResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.DetectedProcs) != 1 {
				t.Fatalf("expected 1 process, got %d", len(resp.DetectedProcs))
			}
			proc := resp.DetectedProcs[0]
			if got := len(proc.Aliases) > 0 || proc.SteamAppID != "" || proc.DesktopID != ""; got != tc.verbose {
				t.Fatalf("verbose fields present=%v, want %v: %+v", got, tc.verbose, proc)
			}
			body := rr.Body.String()
			for _, secret := range []string{"exe_path", "cwd", "args", "/home/user/private", "--token=secret"} {
				if bytes.Contains([]byte(body), []byte(secret)) {
					t.Fatalf("status response leaked %q: %s", secret, body)
				}
			}
		})
	}
}

func TestHandleStatusPendingScanState(t *testing.T) {
	state := NewAppState()
	srv := New(state, profile.NewManager(nil, nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	srv.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ScanState != "pending" {
		t.Errorf("expected scan_state=pending, got %q", resp.ScanState)
	}
	if resp.LastScanTime != "" {
		t.Errorf("expected empty last_scan_time for pending state, got %q", resp.LastScanTime)
	}
}

func TestScanSnapshotAtomicity(t *testing.T) {
	state := NewAppState()

	state.SetScanSnapshot(ScanSnapshot{
		Procs: []profile.DetectedProcess{{PID: 1, Name: "game"}},
		Time:  time.Now(),
		Mode:  ScanModeAll,
		State: ScanStateScanned,
	})

	snap := state.ScanSnapshot()
	if len(snap.Procs) != 1 || snap.Procs[0].Name != "game" {
		t.Errorf("expected atomic snapshot with game process, got %+v", snap)
	}
	if snap.State != ScanStateScanned {
		t.Errorf("expected scanned state, got %q", snap.State)
	}
	if snap.Mode != ScanModeAll {
		t.Errorf("expected all_processes mode, got %q", snap.Mode)
	}
}

func TestHandleStatusMatchInfoVerbose(t *testing.T) {
	state := NewAppState()
	state.SetActive("Portal 2", "portal2")
	state.SetMatchInfo(profile.MatchInfo{
		Source:       "catalog",
		ProfileName:  "Portal 2",
		ProcessName:  "portal2",
		Reason:       "steam_app_id",
		Confidence:   100,
		DiscordAppID: "1499058229571752148",
		RPConnected:  true,
	})
	srv := New(state, profile.NewManager(nil, nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/status?verbose=1", nil)
	rr := httptest.NewRecorder()
	srv.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.MatchInfo == nil {
		t.Fatal("expected match_info in verbose status")
	}
	if resp.MatchInfo.Source != "catalog" {
		t.Errorf("expected source=catalog, got %q", resp.MatchInfo.Source)
	}
	if resp.MatchInfo.Reason != "steam_app_id" {
		t.Errorf("expected reason=steam_app_id, got %q", resp.MatchInfo.Reason)
	}
	if resp.MatchInfo.Confidence != 100 {
		t.Errorf("expected confidence=100, got %d", resp.MatchInfo.Confidence)
	}
}

func TestHandleStatusMatchInfoHiddenByDefault(t *testing.T) {
	state := NewAppState()
	state.SetMatchInfo(profile.MatchInfo{Source: "catalog", Reason: "steam_app_id"})
	srv := New(state, profile.NewManager(nil, nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	srv.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp statusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.MatchInfo != nil {
		t.Error("expected match_info to be hidden in default status")
	}
}

func TestRootReturnsJSON404NotHTML(t *testing.T) {
	srv := New(NewAppState(), profile.NewManager(nil, nil), nil)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	body := rr.Body.String()
	for _, htmlSig := range []string{"<html", "<body", "<head", "<!DOCTYPE", "<script", "<style", "<div", "<!doctype"} {
		if strings.Contains(strings.ToLower(body), htmlSig) {
			t.Errorf("root response contains HTML signature %q: %s", htmlSig, body)
		}
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("expected valid JSON response, got: %v", err)
	}
}

func TestHandleProfileByID_Rename(t *testing.T) {
	pm := profile.NewManager(nil, nil)
	pm.Add(profile.Profile{Name: "OldName", Match: profile.MatchRule{Type: profile.MatchProcessName, Value: "old"}, Priority: 5})
	srv := New(NewAppState(), pm, nil)

	body, _ := json.Marshal(profile.Profile{
		Name:     "NewName",
		Enabled:  true,
		Match:    profile.MatchRule{Type: profile.MatchProcessName, Value: "new"},
		Priority: 10,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/profiles/OldName", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleProfileByID(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Old profile should be gone.
	if pm.Get("OldName") != nil {
		t.Error("expected OldName to be deleted")
	}
	// New profile should exist with updated values.
	newP := pm.Get("NewName")
	if newP == nil {
		t.Fatal("expected NewName to exist")
	}
	if newP.Priority != 10 {
		t.Errorf("priority = %d, want 10", newP.Priority)
	}
	if newP.Match.Value != "new" {
		t.Errorf("match value = %q, want new", newP.Match.Value)
	}
}
