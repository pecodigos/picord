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
	sources, skipped := BuildSources([]string{"steam_local", "steam_shortcuts", "desktop"})
	if len(skipped) > 0 {
		t.Fatalf("unexpected skipped sources: %v", skipped)
	}
	if len(sources) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sources))
	}
	if sources[0].Name() != "steam_local" {
		t.Errorf("expected steam_local, got %s", sources[0].Name())
	}
	if sources[1].Name() != "steam_shortcuts" {
		t.Errorf("expected steam_shortcuts, got %s", sources[1].Name())
	}
	if sources[2].Name() != "desktop" {
		t.Errorf("expected desktop, got %s", sources[2].Name())
	}
}

func TestBuildSources_UnknownSkipped(t *testing.T) {
	sources, skipped := BuildSources([]string{"unknown"})
	if len(sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(sources))
	}
	if len(skipped) != 1 || skipped[0] != "unknown" {
		t.Errorf("expected unknown skipped, got %v", skipped)
	}
}

func TestBuildSources_DefaultConfig(t *testing.T) {
	sources, skipped := BuildSources(config.DefaultCatalogSources)
	if len(skipped) > 0 {
		t.Fatalf("unexpected skipped sources: %v", skipped)
	}
	if len(sources) == 0 {
		t.Fatal("expected at least one source from default config")
	}
}
