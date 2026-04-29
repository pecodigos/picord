package catalog

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DesktopPaths returns candidate directories containing .desktop files.
func DesktopPaths() []string {
	var paths []string
	home, _ := os.UserHomeDir()
	if home != "" {
		paths = append(paths,
			filepath.Join(home, ".local", "share", "applications"),
		)
	}
	paths = append(paths,
		"/usr/share/applications",
		"/usr/local/share/applications",
		"/var/lib/flatpak/exports/share/applications",
	)
	if home != "" {
		paths = append(paths, filepath.Join(home, ".local", "share", "flatpak", "exports", "share", "applications"))
	}
	return paths
}

type DesktopSource struct {
	Roots []string
}

func (s *DesktopSource) Name() string { return "desktop" }

func (s *DesktopSource) Refresh(ctx context.Context, store *Store, opts RefreshOptions) error {
	roots := s.Roots
	if len(roots) == 0 {
		roots = DesktopPaths()
	}

	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".desktop") {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			name, execCmd, icon, wmClass, err := parseDesktopFile(path)
			if err != nil || name == "" {
				continue
			}

			desktopID := strings.TrimSuffix(entry.Name(), ".desktop")
			entryID := fmt.Sprintf("desktop:%s", desktopID)
			e := Entry{
				ID:              entryID,
				Source:          "desktop",
				SourceID:        desktopID,
				Kind:            EntryKindApplication,
				Title:           name,
				NormalizedTitle: NormalizeTitle(name),
				ImageURL:        icon,
				ImageKind:       "desktop_icon",
				UpdatedAt:       time.Now(),
			}
			aliases := []Alias{
				{EntryID: entryID, Kind: AliasDesktopID, Value: desktopID, Normalized: NormalizeTitle(desktopID), Confidence: 90},
				{EntryID: entryID, Kind: AliasTitle, Value: name, Normalized: NormalizeTitle(name), Confidence: 80},
			}
			if execCmd != "" {
				exeBase := filepath.Base(strings.Fields(execCmd)[0])
				if exeBase != "" {
					aliases = append(aliases, Alias{EntryID: entryID, Kind: AliasExecutable, Value: exeBase, Normalized: NormalizeTitle(exeBase), Confidence: 70})
				}
			}
			if wmClass != "" {
				aliases = append(aliases, Alias{EntryID: entryID, Kind: AliasWindowTitle, Value: wmClass, Normalized: NormalizeTitle(wmClass), Confidence: 65})
			}

			if err := store.UpsertEntry(ctx, e, aliases); err != nil {
				return fmt.Errorf("upsert desktop entry %s: %w", desktopID, err)
			}
		}
	}
	return nil
}

func parseDesktopFile(path string) (name, execCmd, icon, wmClass string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inDesktopEntry := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[Desktop Entry]" {
			inDesktopEntry = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inDesktopEntry = false
			continue
		}
		if !inDesktopEntry {
			continue
		}
		if strings.HasPrefix(line, "Name=") {
			name = strings.TrimPrefix(line, "Name=")
		}
		if strings.HasPrefix(line, "Exec=") {
			execCmd = strings.TrimPrefix(line, "Exec=")
		}
		if strings.HasPrefix(line, "Icon=") {
			icon = strings.TrimPrefix(line, "Icon=")
		}
		if strings.HasPrefix(line, "StartupWMClass=") {
			wmClass = strings.TrimPrefix(line, "StartupWMClass=")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", "", err
	}
	return name, execCmd, icon, wmClass, nil
}
