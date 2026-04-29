package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pecodigos/picord/internal/config"
)

type fakeSource struct {
	name      string
	callCount int
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Refresh(ctx context.Context, store *Store, opts RefreshOptions) error {
	f.callCount++
	return nil
}

func TestRefresher_StartStop(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	src := &fakeSource{name: "fake"}
	r := NewRefresher(store, []Source{src}, 100*time.Millisecond)
	r.Start()

	// Wait for at least one refresh to run.
	time.Sleep(200 * time.Millisecond)
	r.Stop()

	if src.callCount < 1 {
		t.Errorf("expected at least 1 refresh call, got %d", src.callCount)
	}
}

func TestRefresher_StopWaits(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	src := &fakeSource{name: "fake"}
	r := NewRefresher(store, []Source{src}, time.Hour)
	r.Start()
	r.Stop()

	// After Stop, no more calls should happen.
	countAfterStop := src.callCount
	time.Sleep(100 * time.Millisecond)
	if src.callCount != countAfterStop {
		t.Errorf("expected no calls after stop, went from %d to %d", countAfterStop, src.callCount)
	}
}

func TestBuildSources(t *testing.T) {
	sources, err := BuildSources([]string{"steam_local", "desktop"})
	if err != nil {
		t.Fatalf("BuildSources failed: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Name() != "steam_local" {
		t.Errorf("expected steam_local, got %s", sources[0].Name())
	}
	if sources[1].Name() != "desktop" {
		t.Errorf("expected desktop, got %s", sources[1].Name())
	}
}

func TestBuildSources_Unknown(t *testing.T) {
	_, err := BuildSources([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestBuildSources_DefaultConfig(t *testing.T) {
	sources, err := BuildSources(config.DefaultCatalogSources)
	if err != nil {
		t.Fatalf("BuildSources with DefaultCatalogSources failed: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("expected at least one source from default config")
	}
}
