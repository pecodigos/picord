package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultSteamGridDBBaseURL = "https://www.steamgriddb.com/api/v2"

// SteamGridDBClient queries SteamGridDB for game artwork.
type SteamGridDBClient struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewSteamGridDBClient creates a client. apiKey may be empty; methods will
// return errors if it is.
func NewSteamGridDBClient(apiKey string) *SteamGridDBClient {
	return &SteamGridDBClient{
		APIKey:  apiKey,
		BaseURL: defaultSteamGridDBBaseURL,
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *SteamGridDBClient) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultSteamGridDBBaseURL
}

func (c *SteamGridDBClient) req(ctx context.Context, method, path string) (*http.Response, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("steamgriddb api key not configured")
	}
	// Separate query string from path before joining.
	query := ""
	if idx := strings.Index(path, "?"); idx >= 0 {
		query = path[idx:]
		path = path[:idx]
	}
	u, err := url.JoinPath(c.base(), path)
	if err != nil {
		return nil, err
	}
	u += query
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	return c.Client.Do(req)
}

// sgdbSearchResponse is the API response for /search/autocomplete/{term}.
type sgdbSearchResponse struct {
	Success bool `json:"success"`
	Data    []struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Release int    `json:"release_date"`
		Types   []string
	} `json:"data"`
}

// Search finds games by title. It returns the best-matching game ID and title.
func (c *SteamGridDBClient) Search(ctx context.Context, title string) (gameID int, gameTitle string, err error) {
	if strings.TrimSpace(title) == "" {
		return 0, "", fmt.Errorf("empty title")
	}
	resp, err := c.req(ctx, http.MethodGet, "search/autocomplete/"+url.PathEscape(title))
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("sgdb search status %d", resp.StatusCode)
	}
	var body sgdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, "", err
	}
	if !body.Success || len(body.Data) == 0 {
		return 0, "", fmt.Errorf("no results")
	}
	// Return first (best) match.
	return body.Data[0].ID, body.Data[0].Name, nil
}

// sgdbGridResponse is the API response for /grids/game/{id}.
type sgdbGridResponse struct {
	Success bool `json:"success"`
	Data    []struct {
		ID       int    `json:"id"`
		URL      string `json:"url"`
		Thumb    string `json:"thumb"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Style    string `json:"style"`
		Mime     string `json:"mime"`
	} `json:"data"`
}

// GridPreference defines which grid style to prefer.
type GridPreference struct {
	Style string // "alternate", "blurred", "white_logo", "material", etc.
}

// FindGrid queries SteamGridDB for grid artwork for a game ID.
// It returns the best-matching grid URL and dimensions.
func (c *SteamGridDBClient) FindGrid(ctx context.Context, gameID int, pref GridPreference) (imgURL string, width, height int, err error) {
	if gameID <= 0 {
		return "", 0, 0, fmt.Errorf("invalid game id")
	}
	path := fmt.Sprintf("grids/game/%d", gameID)
	if pref.Style != "" {
		path += "?styles=" + url.QueryEscape(pref.Style)
	}
	resp, err := c.req(ctx, http.MethodGet, path)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, 0, fmt.Errorf("sgdb grids status %d", resp.StatusCode)
	}
	var body sgdbGridResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", 0, 0, err
	}
	if !body.Success || len(body.Data) == 0 {
		return "", 0, 0, fmt.Errorf("no grids")
	}
	best := body.Data[0]
	for _, g := range body.Data {
		if pref.Style != "" && g.Style == pref.Style {
			best = g
			break
		}
	}
	return best.URL, best.Width, best.Height, nil
}

// EnrichEntry searches SteamGridDB by title and returns the best grid image URL.
func (c *SteamGridDBClient) EnrichEntry(ctx context.Context, title string) (imgURL, resolvedTitle string, err error) {
	gameID, gameTitle, err := c.Search(ctx, title)
	if err != nil {
		return "", "", fmt.Errorf("search: %w", err)
	}
	url, _, _, err := c.FindGrid(ctx, gameID, GridPreference{Style: "alternate"})
	if err != nil {
		// Fallback: try without style preference.
		url, _, _, err = c.FindGrid(ctx, gameID, GridPreference{})
		if err != nil {
			return "", "", fmt.Errorf("grids: %w", err)
		}
	}
	return url, gameTitle, nil
}
