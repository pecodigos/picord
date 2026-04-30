package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SteamLocalPaths returns candidate steamapps directories.
func SteamLocalPaths() []string {
	var paths []string
	home, _ := os.UserHomeDir()
	if home != "" {
		paths = append(paths,
			filepath.Join(home, ".local", "share", "Steam", "steamapps"),
			filepath.Join(home, ".steam", "steam", "steamapps"),
			filepath.Join(home, ".steam", "root", "steamapps"),
		)
	}
	if p := os.Getenv("STEAM_COMPAT_CLIENT_INSTALL_PATH"); p != "" {
		paths = append(paths, filepath.Join(p, "steamapps"))
	}
	return paths
}

type SteamLocalSource struct {
	SteamPaths []string
}

func (s *SteamLocalSource) Name() string { return "steam_local" }

func (s *SteamLocalSource) Refresh(ctx context.Context, store *Store, opts RefreshOptions) error {
	paths := s.SteamPaths
	if len(paths) == 0 {
		paths = SteamLocalPaths()
	}

	for _, dir := range paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "appmanifest_") || !strings.HasSuffix(entry.Name(), ".acf") {
				continue
			}
			appidStr := strings.TrimPrefix(strings.TrimSuffix(entry.Name(), ".acf"), "appmanifest_")
			appid, err := strconv.Atoi(appidStr)
			if err != nil {
				continue
			}

			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}

			name, _ := parseACFStringField(data, "name")
			installdir, _ := parseACFStringField(data, "installdir")
			if name == "" {
				continue
			}

			entryID := fmt.Sprintf("steam:%d", appid)
			e := Entry{
				ID:              entryID,
				Source:          "steam",
				SourceID:        strconv.Itoa(appid),
				Kind:            EntryKindGame,
				Title:           name,
				NormalizedTitle: NormalizeTitle(name),
				ImageURL:        fmt.Sprintf("https://cdn.akamai.steamstatic.com/steam/apps/%d/header.jpg", appid),
				ImageKind:       "steam_header",
				UpdatedAt:       time.Now(),
			}
			aliases := []Alias{
				{EntryID: entryID, Kind: AliasSteamAppID, Value: strconv.Itoa(appid), Normalized: strconv.Itoa(appid), Confidence: 100},
				{EntryID: entryID, Kind: AliasTitle, Value: name, Normalized: NormalizeTitle(name), Confidence: 90},
			}
			if installdir != "" {
				aliases = append(aliases, Alias{EntryID: entryID, Kind: AliasExecutable, Value: installdir, Normalized: NormalizeTitle(installdir), Confidence: 60})
			}

			if err := store.UpsertEntry(ctx, e, aliases); err != nil {
				return fmt.Errorf("upsert steam entry %d: %w", appid, err)
			}
		}
	}
	return nil
}

// parseACFStringField extracts a top-level quoted string value for a given key.
// This is a best-effort parser for Steam appmanifest files.
func parseACFStringField(data []byte, key string) (string, error) {
	s := string(data)
	// Look for "key" followed by whitespace then "value"
	pattern := `"` + key + `"`
	idx := strings.Index(s, pattern)
	if idx < 0 {
		return "", fmt.Errorf("key %q not found", key)
	}
	// Move past the key and its closing quote
	pos := idx + len(pattern)
	// Skip whitespace and tabs
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r') {
		pos++
	}
	if pos >= len(s) || s[pos] != '"' {
		return "", fmt.Errorf("expected quoted value for key %q", key)
	}
	pos++ // skip opening quote
	end := strings.IndexByte(s[pos:], '"')
	if end < 0 {
		return "", fmt.Errorf("unterminated value for key %q", key)
	}
	return s[pos : pos+end], nil
}
