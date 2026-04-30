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

			desktopID := strings.TrimSuffix(entry.Name(), ".desktop")
			if isExcludedDesktopApp(desktopID) {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			df, err := parseDesktopFile(path)
			if err != nil || df.Name == "" {
				continue
			}
			// Also skip terminal apps and NoDisplay apps
			if df.Terminal || df.NoDisplay {
				continue
			}

			entryID := fmt.Sprintf("desktop:%s", desktopID)
			imgURL := ""
			if strings.HasPrefix(df.Icon, "http://") || strings.HasPrefix(df.Icon, "https://") {
				imgURL = df.Icon
			}

			kind := EntryKindApplication
			if df.IsGame() {
				kind = EntryKindGame
			}

			e := Entry{
				ID:              entryID,
				Source:          "desktop",
				SourceID:        desktopID,
				Kind:            kind,
				Title:           df.Name,
				NormalizedTitle: NormalizeTitle(df.Name),
				ImageURL:        imgURL,
				ImageKind:       "desktop_icon",
				UpdatedAt:       time.Now(),
			}
			aliases := []Alias{
				{EntryID: entryID, Kind: AliasDesktopID, Value: desktopID, Normalized: NormalizeTitle(desktopID), Confidence: 90},
				{EntryID: entryID, Kind: AliasTitle, Value: df.Name, Normalized: NormalizeTitle(df.Name), Confidence: 80},
			}
			if df.Exec != "" {
				if exeBase := desktopExecBase(df.Exec); exeBase != "" {
					aliases = append(aliases, Alias{EntryID: entryID, Kind: AliasExecutable, Value: exeBase, Normalized: NormalizeTitle(exeBase), Confidence: 70})
				}
			}
			if df.WMClass != "" {
				aliases = append(aliases, Alias{EntryID: entryID, Kind: AliasWindowTitle, Value: df.WMClass, Normalized: NormalizeTitle(df.WMClass), Confidence: 65})
			}

			if err := store.UpsertEntry(ctx, e, aliases); err != nil {
				return fmt.Errorf("upsert desktop entry %s: %w", desktopID, err)
			}
		}
	}

	// Clean up any previously-added desktop entries for excluded apps.
	if err := deleteExcludedDesktopEntries(ctx, store); err != nil {
		return fmt.Errorf("cleanup excluded desktop entries: %w", err)
	}

	return nil
}

// desktopFile holds parsed fields from a .desktop file.
type desktopFile struct {
	Name       string
	Exec       string
	Icon       string
	WMClass    string
	Categories string
	Terminal   bool
	NoDisplay  bool
}

// IsGame returns true if the .desktop file appears to describe a game.
func (df *desktopFile) IsGame() bool {
	if df.Terminal {
		return false
	}
	cats := strings.ToLower(df.Categories)
	return strings.Contains(cats, "game")
}

func desktopExecBase(execLine string) string {
	argv0 := parseDesktopExecArgv0(execLine)
	if argv0 == "" {
		return ""
	}
	return filepath.Base(argv0)
}

func parseDesktopExecArgv0(execLine string) string {
	line := strings.TrimSpace(execLine)
	if line == "" {
		return ""
	}
	var b strings.Builder
	inQuote := false
	escaped := false
	started := false
	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' {
			escaped = true
			started = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			started = true
			continue
		}
		if !inQuote && (r == ' ' || r == '\t' || r == '\n') {
			if started {
				break
			}
			continue
		}
		b.WriteRune(r)
		started = true
	}
	arg := b.String()
	if strings.HasPrefix(arg, "%") {
		return ""
	}
	return arg
}

func parseDesktopFile(path string) (desktopFile, error) {
	var df desktopFile
	f, err := os.Open(path)
	if err != nil {
		return df, err
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
			df.Name = strings.TrimPrefix(line, "Name=")
		}
		if strings.HasPrefix(line, "Exec=") {
			df.Exec = strings.TrimPrefix(line, "Exec=")
		}
		if strings.HasPrefix(line, "Icon=") {
			df.Icon = strings.TrimPrefix(line, "Icon=")
		}
		if strings.HasPrefix(line, "StartupWMClass=") {
			df.WMClass = strings.TrimPrefix(line, "StartupWMClass=")
		}
		if strings.HasPrefix(line, "Categories=") {
			df.Categories = strings.TrimPrefix(line, "Categories=")
		}
		if strings.HasPrefix(line, "Terminal=") {
			v := strings.ToLower(strings.TrimPrefix(line, "Terminal="))
			df.Terminal = v == "true" || v == "1"
		}
		if strings.HasPrefix(line, "NoDisplay=") {
			v := strings.ToLower(strings.TrimPrefix(line, "NoDisplay="))
			df.NoDisplay = v == "true" || v == "1"
		}
	}
	if err := scanner.Err(); err != nil {
		return df, err
	}
	return df, nil
}

// isExcludedDesktopApp returns true for desktop IDs that should never be
// tracked as Rich Presence: browsers, Discord, file managers, terminals, etc.
func isExcludedDesktopApp(desktopID string) bool {
	lower := strings.ToLower(desktopID)

	exactExcludes := []string{
		// Discord
		"discord", "discord-canary", "discord-ptb", "discord-development",
		// Firefox variants
		"firefox", "firefox-esr", "firefox-developer-edition",
		"firefox-nightly", "firefox-beta", "firefox-devedition",
		"librewolf", "waterfox", "floorp", "palemoon", "basilisk",
		"icecat", "iceape", "seamonkey",
		// Chromium / Chrome / Edge / Brave / Opera / Vivaldi
		"google-chrome", "google-chrome-beta", "google-chrome-unstable",
		"chromium", "chromium-browser",
		"brave-browser", "brave-browser-beta", "brave-browser-nightly",
		"opera", "opera-beta", "opera-developer",
		"microsoft-edge", "microsoft-edge-beta", "microsoft-edge-dev",
		"vivaldi", "vivaldi-stable",
		"thorium-browser", "iridium-browser", "ungoogled-chromium",
		"epiphany", "falkon", "midori", "qutebrowser",
		"konqueror", "rekonq", "otter-browser",
		"luakit", "surf", "nyxt", "lagrange", "badwolf",
		"netsurf", "netsurf-gtk3", "dooble",
		"tor-browser", "torbrowser",
		"zen", "zen-browser",
		// File managers
		"dolphin", "nautilus", "nemo", "thunar", "pcmanfm",
		"caja", "spacefm", "krusader", "doublecmd",
		// Common desktop noise
		"xfdesktop", "plasmashell", "gnome-shell", "cinnamon",
		"pamac", "pamac-tray", "octopi",
		"nm-applet", "blueman-applet", "pasystray",
		// Launchers (never track as active game)
		"steam", "steamlinux", "steam-runtime", "steamwebhelper",
		"epicgameslauncher", "heroic", "lutris", "gog-galaxy",
		"itch", "bottles", "playnite",
		// Terminal emulators
		"kitty", "alacritty", "wezterm", "foot", "gnome-terminal",
		"konsole", "xfce4-terminal", "lxterminal", "terminator",
		"tilix", "guake", "yakuake", "tilda", "qterminal",
		"st", "xterm", "urxvt", "rxvt", "eterm",
		"hyper", "tabby", "warp", "rio",
	}
	for _, e := range exactExcludes {
		if lower == e {
			return true
		}
	}

	// Flatpak / snap style IDs.
	flatpakBrowserIDs := []string{
		"org.mozilla.firefox",
		"org.mozilla.firefox_beta",
		"org.mozilla.firefox_nightly",
		"org.mozilla.firefoxdevedition",
		"io.gitlab.librewolf",
		"io.github.zen_browser.zen",
		"com.google.Chrome",
		"com.google.Chrome.beta",
		"com.google.Chrome.dev",
		"com.google.Chrome.canary",
		"org.chromium.Chromium",
		"com.brave.Browser",
		"com.microsoft.Edge",
		"com.microsoft.Edge.dev",
		"com.microsoft.Edge.beta",
		"com.opera.Opera",
		"com.vivaldi.Vivaldi",
		"com.github.Eloston.UngoogledChromium",
		"org.gnome.Epiphany",
		"io.gitlab.pale_moon",
	}
	for _, f := range flatpakBrowserIDs {
		if lower == strings.ToLower(f) {
			return true
		}
	}

	// Prefix / suffix helpers.
	if strings.HasPrefix(lower, "xdg-") {
		return true
	}
	if strings.HasSuffix(lower, "-settings") || strings.HasSuffix(lower, "-config") {
		return true
	}
	return false
}

func deleteExcludedDesktopEntries(ctx context.Context, store *Store) error {
	// Build a list of exact excluded desktop IDs.
	var excludes []string
	for _, id := range []string{
		"discord", "discord-canary", "discord-ptb", "discord-development",
		"firefox", "firefox-esr", "firefox-developer-edition",
		"firefox-nightly", "firefox-beta", "firefox-devedition",
		"librewolf", "waterfox", "floorp", "palemoon", "basilisk",
		"icecat", "iceape", "seamonkey",
		"google-chrome", "google-chrome-beta", "google-chrome-unstable",
		"chromium", "chromium-browser",
		"brave-browser", "brave-browser-beta", "brave-browser-nightly",
		"opera", "opera-beta", "opera-developer",
		"microsoft-edge", "microsoft-edge-beta", "microsoft-edge-dev",
		"vivaldi", "vivaldi-stable",
		"thorium-browser", "iridium-browser", "ungoogled-chromium",
		"epiphany", "falkon", "midori", "qutebrowser",
		"konqueror", "rekonq", "otter-browser",
		"luakit", "surf", "nyxt", "lagrange", "badwolf",
		"netsurf", "netsurf-gtk3", "dooble",
		"tor-browser", "torbrowser",
		"zen", "zen-browser",
		"dolphin", "nautilus", "nemo", "thunar", "pcmanfm",
		"caja", "spacefm", "krusader", "doublecmd",
		"xfdesktop", "plasmashell", "gnome-shell", "cinnamon",
		"pamac", "pamac-tray", "octopi",
		"nm-applet", "blueman-applet", "pasystray",
		"org.mozilla.firefox", "org.mozilla.firefox_beta",
		"org.mozilla.firefox_nightly", "org.mozilla.firefoxdevedition",
		"io.gitlab.librewolf", "io.github.zen_browser.zen",
		"com.google.Chrome", "com.google.Chrome.beta", "com.google.Chrome.dev",
		"org.chromium.Chromium", "com.brave.Browser",
		"com.microsoft.Edge", "com.microsoft.Edge.dev", "com.microsoft.Edge.beta",
		"com.opera.Opera", "com.vivaldi.Vivaldi",
		"com.github.Eloston.UngoogledChromium",
		"org.gnome.Epiphany", "io.gitlab.pale_moon",
		// Launchers
		"steam", "steamlinux", "steam-runtime", "steamwebhelper",
		"epicgameslauncher", "heroic", "lutris", "gog-galaxy",
		"itch", "bottles", "playnite",
		"com.valvesoftware.Steam", "com.heroicgameslauncher.hgl",
		"net.lutris.Lutris", "com.usebottles.bottles",
		// Terminals
		"kitty", "alacritty", "wezterm", "foot", "gnome-terminal",
		"konsole", "xfce4-terminal", "lxterminal", "terminator",
		"tilix", "guake", "yakuake", "tilda", "qterminal",
		"st", "xterm", "urxvt", "rxvt", "eterm",
		"hyper", "tabby", "warp", "rio",
	} {
		excludes = append(excludes, "desktop:"+id)
	}

	if len(excludes) == 0 {
		return nil
	}

	placeholders := make([]string, len(excludes))
	args := make([]any, len(excludes))
	for i, id := range excludes {
		placeholders[i] = "?"
		args[i] = id
	}

	_, err := store.db.ExecContext(ctx,
		"DELETE FROM entries WHERE source = 'desktop' AND id IN ("+strings.Join(placeholders, ",")+")",
		args...,
	)
	return err
}
