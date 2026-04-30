package catalog

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Enricher fills in missing catalog artwork using SteamGridDB.
type Enricher struct {
	Store  *Store
	Client *SteamGridDBClient
}

// NewEnricher creates an enricher. If apiKey is empty, enrichment is a no-op.
func NewEnricher(store *Store, apiKey string) *Enricher {
	var client *SteamGridDBClient
	if apiKey != "" {
		client = NewSteamGridDBClient(apiKey)
	}
	return &Enricher{Store: store, Client: client}
}

// Enabled returns true if the enricher has a valid API key.
func (e *Enricher) Enabled() bool {
	return e.Client != nil
}

// EnrichMissingImages queries the catalog for entries without images, searches
// SteamGridDB by title, and updates matching entries. It returns the number of
// entries enriched and any fatal error.
func (e *Enricher) EnrichMissingImages(ctx context.Context, batchSize int) (enriched int, err error) {
	if !e.Enabled() {
		return 0, fmt.Errorf("steamgriddb api key not configured")
	}
	if batchSize <= 0 {
		batchSize = 50
	}

	entries, err := e.Store.EntriesMissingImages(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("fetch missing images: %w", err)
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return enriched, ctx.Err()
		default:
		}

		if entry.Title == "" {
			continue
		}

		imgURL, resolvedTitle, err := e.Client.EnrichEntry(ctx, entry.Title)
		if err != nil {
			log.Printf("[enricher] no image for %q: %v", entry.Title, err)
			continue
		}

		if err := e.Store.UpdateEntryImage(ctx, entry.ID, imgURL, "steamgriddb"); err != nil {
			log.Printf("[enricher] failed to update %s: %v", entry.ID, err)
			continue
		}

		log.Printf("[enricher] enriched %q (title=%q) -> %s", entry.ID, resolvedTitle, imgURL)
		enriched++

		// Small rate-limit pause between requests.
		select {
		case <-ctx.Done():
			return enriched, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	return enriched, nil
}
