package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const lutrisAPIBase = "https://lutris.net/api/games"

type lutrisPage struct {
	Count    int           `json:"count"`
	Next     string        `json:"next"`
	Previous string        `json:"previous"`
	Results  []lutrisGame  `json:"results"`
}

type lutrisGame struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Year      int    `json:"year"`
	BannerURL string `json:"banner_url"`
}

type LutrisPublicSource struct {
	HTTPClient *http.Client
	BaseURL    string
}

func (s *LutrisPublicSource) Name() string { return "lutris_public" }

func (s *LutrisPublicSource) client() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}

func (s *LutrisPublicSource) url() string {
	if s.BaseURL != "" {
		return s.BaseURL
	}
	return lutrisAPIBase
}

func (s *LutrisPublicSource) Refresh(ctx context.Context, store *Store, opts RefreshOptions) error {
	if opts.Offline {
		return nil
	}

	cursor, etag, _, _, err := store.GetSourceState(ctx, s.Name())
	if err != nil {
		return fmt.Errorf("get source state: %w", err)
	}

	pageURL := s.url()
	if cursor != "" {
		pageURL = cursor
	}

	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 1000 // safety cap
	}

	for page := 1; page <= maxPages; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}

		resp, err := s.client().Do(req)
		if err != nil {
			_ = store.SetSourceState(ctx, s.Name(), pageURL, etag, fmt.Sprintf("request error: %v", err), time.Now())
			return fmt.Errorf("fetch page: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB max
		resp.Body.Close()
		if err != nil {
			_ = store.SetSourceState(ctx, s.Name(), pageURL, etag, fmt.Sprintf("read body: %v", err), time.Now())
			return fmt.Errorf("read body: %w", err)
		}
		if resp.StatusCode == http.StatusNotModified {
			// No new data; treat as complete for this run.
			_ = store.SetSourceState(ctx, s.Name(), "", etag, "", time.Now())
			break
		}
		if resp.StatusCode != http.StatusOK {
			_ = store.SetSourceState(ctx, s.Name(), pageURL, etag, fmt.Sprintf("status %d: %s", resp.StatusCode, string(body)), time.Now())
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		var pageData lutrisPage
		if err := json.Unmarshal(body, &pageData); err != nil {
			_ = store.SetSourceState(ctx, s.Name(), pageURL, etag, fmt.Sprintf("parse json: %v", err), time.Now())
			return fmt.Errorf("parse json: %w", err)
		}

		for _, g := range pageData.Results {
			entryID := fmt.Sprintf("lutris:%d", g.ID)
			e := Entry{
				ID:              entryID,
				Source:          "lutris",
				SourceID:        strconv.Itoa(g.ID),
				Kind:            EntryKindGame,
				Title:           g.Name,
				NormalizedTitle: NormalizeTitle(g.Name),
				ReleaseYear:     g.Year,
				ImageURL:        g.BannerURL,
				ImageKind:       "lutris_banner",
				UpdatedAt:       time.Now(),
			}
			aliases := []Alias{
				{EntryID: entryID, Kind: AliasLutrisSlug, Value: g.Slug, Normalized: NormalizeTitle(g.Slug), Confidence: 95},
				{EntryID: entryID, Kind: AliasTitle, Value: g.Name, Normalized: NormalizeTitle(g.Name), Confidence: 90},
			}
			if err := store.UpsertEntry(ctx, e, aliases); err != nil {
				return fmt.Errorf("upsert lutris entry %d: %w", g.ID, err)
			}
		}

		// Save the *next* page URL as cursor so resume continues forward.
		newCursor := pageData.Next
		newETag := resp.Header.Get("ETag")
		if newCursor == "" {
			// Completed full sync; reset cursor for next full refresh.
			newCursor = ""
		}
		_ = store.SetSourceState(ctx, s.Name(), newCursor, newETag, "", time.Now())

		if pageData.Next == "" {
			break
		}
		pageURL = pageData.Next

		// Rate-limit pause between pages.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return nil
}
