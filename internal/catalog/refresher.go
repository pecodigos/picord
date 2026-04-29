package catalog

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Refresher runs background catalog metadata refreshes on a schedule.
type Refresher struct {
	store     *Store
	sources   []Source
	interval  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mu        sync.Mutex
	running   bool
}

// NewRefresher creates a refresher. If interval is 0, it defaults to 24 hours.
func NewRefresher(store *Store, sources []Source, interval time.Duration) *Refresher {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &Refresher{
		store:    store,
		sources:  sources,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background refresh loop. It is safe to call multiple times.
func (r *Refresher) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return
	}
	r.running = true
	r.wg.Add(1)
	go r.loop()
}

// Stop signals the refresher to stop and waits for the current iteration.
func (r *Refresher) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stopCh)
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Refresher) loop() {
	defer r.wg.Done()

	// Run once immediately without blocking startup.
	go r.refreshAll()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.refreshAll()
		}
	}
}

func (r *Refresher) refreshAll() {
	if r.store == nil {
		return
	}
	for _, src := range r.sources {
		select {
		case <-r.stopCh:
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		log.Printf("[catalog] refreshing source=%s", src.Name())
		err := src.Refresh(ctx, r.store, RefreshOptions{})
		cancel()
		if err != nil {
			log.Printf("[catalog] refresh source=%s error: %v", src.Name(), err)
		} else {
			log.Printf("[catalog] refresh source=%s completed", src.Name())
		}

		// Small rate-limit pause between sources.
		select {
		case <-r.stopCh:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// BuildSources creates source adapters from config source names.
// Supported sources: steam_local, lutris_public, desktop.
// lutris_local is not yet implemented.
func BuildSources(sourceNames []string) ([]Source, error) {
	var sources []Source
	for _, name := range sourceNames {
		switch name {
		case "steam_local":
			sources = append(sources, &SteamLocalSource{})
		case "lutris_public":
			sources = append(sources, &LutrisPublicSource{})
		case "desktop":
			sources = append(sources, &DesktopSource{})
		default:
			return nil, fmt.Errorf("unknown catalog source: %s", name)
		}
	}
	return sources, nil
}
