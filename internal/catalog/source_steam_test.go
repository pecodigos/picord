package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseACFStringField(t *testing.T) {
	data := `"AppState"
{
	"appid"		"620"
	"name"		"Portal 2"
	"installdir"		"Portal 2"
}`

	name, err := parseACFStringField([]byte(data), "name")
	if err != nil {
		t.Fatalf("parseACFStringField name: %v", err)
	}
	if name != "Portal 2" {
		t.Errorf("name=%q, want Portal 2", name)
	}

	appid, err := parseACFStringField([]byte(data), "appid")
	if err != nil {
		t.Fatalf("parseACFStringField appid: %v", err)
	}
	if appid != "620" {
		t.Errorf("appid=%q, want 620", appid)
	}

	_, err = parseACFStringField([]byte(data), "missing")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestSteamLocalSource_Refresh(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// Copy testdata manifest into a fake steamapps dir.
	steamapps := filepath.Join(dir, "steamapps")
	if err := os.MkdirAll(steamapps, 0755); err != nil {
		t.Fatal(err)
	}
	manifestSrc := filepath.Join("testdata", "steam", "appmanifest_620.acf")
	manifestDst := filepath.Join(steamapps, "appmanifest_620.acf")
	data, err := os.ReadFile(manifestSrc)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := os.WriteFile(manifestDst, data, 0644); err != nil {
		t.Fatal(err)
	}

	src := &SteamLocalSource{SteamPaths: []string{steamapps}}
	ctx := context.Background()
	if err := src.Refresh(ctx, store, RefreshOptions{}); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	entry, err := store.GetEntry(ctx, "steam:620")
	if err != nil {
		t.Fatalf("GetEntry failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected steam:620 entry")
	}
	if entry.Title != "Portal 2" {
		t.Errorf("title=%q, want Portal 2", entry.Title)
	}
	if entry.SourceID != "620" {
		t.Errorf("source_id=%q, want 620", entry.SourceID)
	}

	// Verify aliases
	aliases, err := store.SearchByAlias(ctx, AliasSteamAppID, "620")
	if err != nil {
		t.Fatalf("SearchByAlias failed: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Title != "Portal 2" {
		t.Errorf("expected 1 alias result for Portal 2, got %+v", aliases)
	}
}
