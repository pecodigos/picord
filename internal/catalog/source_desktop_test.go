package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseDesktopFile(t *testing.T) {
	name, execCmd, icon, wmClass, err := parseDesktopFile(filepath.Join("testdata", "desktop", "sample.desktop"))
	if err != nil {
		t.Fatalf("parseDesktopFile: %v", err)
	}
	if name != "Sample Application" {
		t.Errorf("name=%q, want Sample Application", name)
	}
	if execCmd != "/usr/bin/sample-app %U" {
		t.Errorf("exec=%q, want /usr/bin/sample-app %%U", execCmd)
	}
	if icon != "sample-app" {
		t.Errorf("icon=%q, want sample-app", icon)
	}
	if wmClass != "SampleApp" {
		t.Errorf("wmClass=%q, want SampleApp", wmClass)
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
	// Copy testdata desktop file.
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
}
