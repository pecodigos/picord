package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDesktopFile(t *testing.T) {
	df, err := parseDesktopFile(filepath.Join("testdata", "desktop", "sample.desktop"))
	if err != nil {
		t.Fatalf("parseDesktopFile: %v", err)
	}
	if df.Name != "Sample Application" {
		t.Errorf("name=%q, want Sample Application", df.Name)
	}
	if df.Exec != "/usr/bin/sample-app %U" {
		t.Errorf("exec=%q, want /usr/bin/sample-app %%U", df.Exec)
	}
	if df.Icon != "sample-app" {
		t.Errorf("icon=%q, want sample-app", df.Icon)
	}
	if df.WMClass != "SampleApp" {
		t.Errorf("wmClass=%q, want SampleApp", df.WMClass)
	}
	if df.Categories != "Game;ActionGame;" {
		t.Errorf("categories=%q, want Game;ActionGame;", df.Categories)
	}
	if !df.IsGame() {
		t.Error("expected sample.desktop to be a game")
	}
}

func TestDesktopFile_IsGame(t *testing.T) {
	cases := []struct {
		cats     string
		terminal bool
		want     bool
	}{
		{"Game;ActionGame;", false, true},
		{"Game;", false, true},
		{"Network;WebBrowser;", false, false},
		{"System;TerminalEmulator;", false, false},
		{"", false, false},
		{"Game;", true, false}, // terminal overrides game category
	}
	for _, tc := range cases {
		df := desktopFile{Categories: tc.cats, Terminal: tc.terminal}
		got := df.IsGame()
		if got != tc.want {
			t.Errorf("IsGame(cats=%q, terminal=%v) = %v, want %v", tc.cats, tc.terminal, got, tc.want)
		}
	}
}

func TestDesktopExecBase(t *testing.T) {
	cases := []struct {
		exec string
		want string
	}{
		{"/usr/bin/sample-app %U", "sample-app"},
		{"   ", ""},
		{"\"/opt/My Game/game\" %u", "game"},
		{"%u", ""},
	}
	for _, tc := range cases {
		if got := desktopExecBase(tc.exec); got != tc.want {
			t.Errorf("desktopExecBase(%q) = %q, want %q", tc.exec, got, tc.want)
		}
	}
}

func TestDesktopSource_Refresh(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	appDir := filepath.Join(dir, "applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Copy testdata desktop file (has Categories=Game).
	srcPath := filepath.Join("testdata", "desktop", "sample.desktop")
	srcData, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "sample.desktop"), srcData, 0644); err != nil {
		t.Fatal(err)
	}

	src := &DesktopSource{Roots: []string{appDir}}
	ctx := context.Background()
	if err := src.Refresh(ctx, store, RefreshOptions{}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	results, err := store.SearchByAlias(ctx, AliasDesktopID, "sample")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Sample Application" {
		t.Errorf("expected Sample Application, got %+v", results)
	}
	if results[0].Kind != EntryKindGame {
		t.Errorf("expected kind=game, got %q", results[0].Kind)
	}
}

func TestDesktopSource_RefreshSkipsExcludedApps(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	appDir := filepath.Join(dir, "applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Game desktop file (has Categories=Game).
	gameDesktop := `[Desktop Entry]
Name=Hollow Knight
Exec=/usr/bin/hollow-knight
Icon=hollow-knight
Categories=Game;AdventureGame;
`
	if err := os.WriteFile(filepath.Join(appDir, "hollow-knight.desktop"), []byte(gameDesktop), 0644); err != nil {
		t.Fatal(err)
	}

	// Browser desktop file (excluded by ID).
	browserDesktop := `[Desktop Entry]
Name=Firefox
Exec=/usr/bin/firefox
Icon=firefox
Categories=Network;WebBrowser;
`
	if err := os.WriteFile(filepath.Join(appDir, "firefox.desktop"), []byte(browserDesktop), 0644); err != nil {
		t.Fatal(err)
	}

	// Terminal desktop file (excluded by Terminal=true).
	terminalDesktop := `[Desktop Entry]
Name=Kitty
Exec=/usr/bin/kitty
Icon=kitty
Categories=System;TerminalEmulator;
Terminal=true
`
	if err := os.WriteFile(filepath.Join(appDir, "kitty.desktop"), []byte(terminalDesktop), 0644); err != nil {
		t.Fatal(err)
	}

	// Launcher desktop file (excluded by ID).
	launcherDesktop := `[Desktop Entry]
Name=Steam
Exec=/usr/bin/steam
Icon=steam
Categories=Network;FileTransfer;
`
	if err := os.WriteFile(filepath.Join(appDir, "steam.desktop"), []byte(launcherDesktop), 0644); err != nil {
		t.Fatal(err)
	}

	// Non-game app desktop file (NOT excluded — should be included).
	appDesktop := `[Desktop Entry]
Name=OBS Studio
Exec=/usr/bin/obs
Icon=obs
Categories=AudioVideo;Recorder;
`
	if err := os.WriteFile(filepath.Join(appDir, "obs.desktop"), []byte(appDesktop), 0644); err != nil {
		t.Fatal(err)
	}

	src := &DesktopSource{Roots: []string{appDir}}
	ctx := context.Background()
	if err := src.Refresh(ctx, store, RefreshOptions{}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Game should be present.
	gameResults, err := store.SearchByAlias(ctx, AliasDesktopID, "hollow-knight")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(gameResults) != 1 {
		t.Errorf("expected 1 game result, got %d", len(gameResults))
	}

	// Firefox should NOT be present.
	ffResults, err := store.SearchByAlias(ctx, AliasDesktopID, "firefox")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(ffResults) != 0 {
		t.Errorf("expected 0 firefox results, got %d", len(ffResults))
	}

	// Kitty should NOT be present.
	kittyResults, err := store.SearchByAlias(ctx, AliasDesktopID, "kitty")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(kittyResults) != 0 {
		t.Errorf("expected 0 kitty results, got %d", len(kittyResults))
	}

	// Steam should NOT be present.
	steamResults, err := store.SearchByAlias(ctx, AliasDesktopID, "steam")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(steamResults) != 0 {
		t.Errorf("expected 0 steam results, got %d", len(steamResults))
	}

	// OBS SHOULD be present (non-game app, not excluded).
	obsResults, err := store.SearchByAlias(ctx, AliasDesktopID, "obs")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(obsResults) != 1 {
		t.Errorf("expected 1 obs result, got %d", len(obsResults))
	}
	if obsResults[0].Kind != EntryKindApplication {
		t.Errorf("expected obs kind=application, got %q", obsResults[0].Kind)
	}
}

func TestDesktopSource_RefreshCleansUpExcludedEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Manually insert a Firefox desktop entry (simulating old behavior).
	if err := store.UpsertEntry(ctx, Entry{
		ID: "desktop:firefox", Source: "desktop", SourceID: "firefox",
		Kind: EntryKindApplication, Title: "Firefox",
		NormalizedTitle: NormalizeTitle("Firefox"),
		UpdatedAt:       time.Now(),
	}, []Alias{
		{EntryID: "desktop:firefox", Kind: AliasDesktopID, Value: "firefox", Normalized: NormalizeTitle("firefox"), Confidence: 90},
	}); err != nil {
		t.Fatalf("UpsertEntry failed: %v", err)
	}

	// Verify Firefox exists before refresh.
	before, err := store.SearchByAlias(ctx, AliasDesktopID, "firefox")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("expected 1 firefox entry before cleanup, got %d", len(before))
	}

	// Run refresh on an empty directory — cleanup should still run.
	appDir := filepath.Join(dir, "applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := &DesktopSource{Roots: []string{appDir}}
	if err := src.Refresh(ctx, store, RefreshOptions{}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Firefox should be gone after cleanup.
	after, err := store.SearchByAlias(ctx, AliasDesktopID, "firefox")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 firefox entries after cleanup, got %d", len(after))
	}
}
