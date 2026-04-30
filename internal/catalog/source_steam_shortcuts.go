package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SteamShortcutsSource struct {
	Paths []string
}

func (s *SteamShortcutsSource) Name() string { return "steam_shortcuts" }

func (s *SteamShortcutsSource) Refresh(ctx context.Context, store *Store, opts RefreshOptions) error {
	paths := s.Paths
	if len(paths) == 0 {
		paths = SteamShortcutsPaths()
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read shortcuts.vdf %s: %w", path, err)
		}

		vdf, err := parseBinaryVDF(data)
		if err != nil {
			return fmt.Errorf("parse shortcuts.vdf %s: %w", path, err)
		}

		shortcuts, err := parseSteamShortcuts(vdf)
		if err != nil {
			return fmt.Errorf("extract shortcuts from %s: %w", path, err)
		}

		for _, sc := range shortcuts {
			entryID := fmt.Sprintf("steam_shortcut:%d", sc.AppID)
			e := Entry{
				ID:              entryID,
				Source:          "steam_shortcut",
				SourceID:        fmt.Sprintf("%d", sc.AppID),
				Kind:            EntryKindGame,
				Title:           sc.AppName,
				NormalizedTitle: NormalizeTitle(sc.AppName),
				ImageURL:        "",
				ImageKind:       "",
				UpdatedAt:       time.Now(),
			}

			aliases := []Alias{
				{EntryID: entryID, Kind: AliasSteamAppID, Value: fmt.Sprintf("%d", sc.AppID), Normalized: fmt.Sprintf("%d", sc.AppID), Confidence: 100},
				{EntryID: entryID, Kind: AliasTitle, Value: sc.AppName, Normalized: NormalizeTitle(sc.AppName), Confidence: 95},
			}

			// Add executable alias from the Exe path basename.
			exeBase := shortcutExeBase(sc.Exe)
			if exeBase != "" {
				aliases = append(aliases, Alias{
					EntryID:    entryID,
					Kind:       AliasExecutable,
					Value:      exeBase,
					Normalized: NormalizeTitle(exeBase),
					Confidence: 80,
				})
				// Also add without .exe suffix for Wine/Proton games.
				if strings.HasSuffix(strings.ToLower(exeBase), ".exe") {
					baseNoExt := strings.TrimSuffix(exeBase, filepath.Ext(exeBase))
					aliases = append(aliases, Alias{
						EntryID:    entryID,
						Kind:       AliasExecutable,
						Value:      baseNoExt,
						Normalized: NormalizeTitle(baseNoExt),
						Confidence: 75,
					})
				}
			}

			if err := store.UpsertEntry(ctx, e, aliases); err != nil {
				return fmt.Errorf("upsert shortcut entry %d: %w", sc.AppID, err)
			}
		}
	}
	return nil
}
