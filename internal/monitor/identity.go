package monitor

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/pecodigos/picord/internal/profile"
)

// isCarrierProcess returns true for Wine/Proton/Steam helper processes that
// don't identify the actual game by themselves.
func isCarrierProcess(name string) bool {
	lower := strings.ToLower(name)
	carriers := []string{
		"wine", "wine64", "wineserver", "wine-preloader", "wine64-preloader",
		"proton", "pressure-vessel-adverb", "pressure-vessel-wrap",
		"explorer.exe", "services.exe", "plugplay.exe", "winedevice.exe",
		"rpcss.exe", "svchost.exe", "conhost.exe",
		"steam.exe", "steamwebhelper.exe", "steamoverlayui.exe",
	}
	for _, c := range carriers {
		if lower == c {
			return true
		}
	}
	// Prefix match for pressure-vessel variants
	if strings.HasPrefix(lower, "pressure-vessel-") {
		return true
	}
	return false
}

// isNoisyProcess returns true for processes we should never use as aliases.
func isNoisyProcess(name string) bool {
	lower := strings.ToLower(name)
	noise := []string{
		"wine", "wine64", "wineserver", "wine-preloader", "wine64-preloader",
		"proton", "pressure-vessel-adverb", "pressure-vessel-wrap",
		"explorer.exe", "services.exe", "plugplay.exe", "winedevice.exe",
		"rpcss.exe", "svchost.exe", "conhost.exe", "cmd.exe",
		"steam.exe", "steamwebhelper.exe", "steamoverlayui.exe",
		"crasher64.exe", "isppcwow64.exe",
	}
	for _, n := range noise {
		if lower == n {
			return true
		}
	}
	return strings.HasPrefix(lower, "pressure-vessel-")
}

// isExcludedApp returns true for common desktop apps that should never be
// tracked as Rich Presence: browsers, Discord, file managers, etc.
func isExcludedApp(name string) bool {
	lower := strings.ToLower(name)

	// Exact process-name matches.
	exactExcludes := []string{
		// Discord
		"discord", "discordcanary", "discordptb", "discorddevelopment",
		// Firefox variants
		"firefox", "firefox-bin", "firefox-esr", "firefox-esr-bin",
		"firefox-developer-edition", "firefox-devedition",
		"firefox-nightly", "firefox-nightly-bin",
		"firefox-beta", "firefox-beta-bin",
		"librewolf", "waterfox", "floorp", "palemoon", "basilisk",
		"icecat", "iceape", "seamonkey",
		// Chromium / Chrome / Edge / Brave / Opera / Vivaldi
		"chrome", "chromium", "chromium-browser",
		"brave", "brave-browser",
		"opera", "opera-beta", "opera-developer",
		"microsoft-edge", "microsoft-edge-beta", "microsoft-edge-dev",
		"vivaldi", "vivaldi-stable",
		"thorium", "thorium-browser",
		"iridium", "iridium-browser",
		"ungoogled-chromium",
		"epiphany", "falkon", "midori", "qutebrowser",
		"konqueror", "rekonq", "otter-browser",
		"luakit", "surf", "nyxt", "lagrange", "badwolf",
		"netsurf", "netsurf-gtk3", "dooble",
		"tor-browser", "torbrowser",
		"zen", "zen-browser",
		// File managers
		"dolphin", "nautilus", "thunar", "nemo", "pcmanfm",
		"caja", "spacefm", "krusader", "doublecmd",
		// Common desktop noise
		"xfdesktop", "plasmashell", "gnome-shell", "cinnamon",
		"pamac", "pamac-tray", "octopi",
		"nm-applet", "blueman-applet", "pasystray",
		// Launchers (never show as the active game)
		"steam", "steamlinux", "steam-runtime", "steamwebhelper",
		"epicgameslauncher", "heroic", "lutris", "gog-galaxy",
		"itch", "bottles", "playnite",
		// Terminal emulators (never track as active game)
		"kitty", "alacritty", "wezterm", "foot", "gnome-terminal",
		"konsole", "xfce4-terminal", "lxterminal", "terminator",
		"tilix", "guake", "yakuake", "tilda", "qterminal",
		"st", "xterm", "urxvt", "rxvt", "eterm",
		"hyper", "tabby", "warp", "rio",
		// Shells (when detected as standalone processes)
		"bash", "zsh", "fish", "sh", "dash", "csh", "tcsh",
	}
	for _, e := range exactExcludes {
		if lower == e {
			return true
		}
	}

	// Flatpak / snap style IDs often contain dots.
	if strings.Contains(lower, ".") {
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
	}

	// Match helpers with known suffixes
	if strings.HasPrefix(lower, "xdg-") {
		return true
	}
	if strings.HasSuffix(lower, "-settings") || strings.HasSuffix(lower, "-config") {
		return true
	}
	return false
}

// ExtractAliases gathers candidate game identity strings from a ProcessInfo.
func ExtractAliases(info *ProcessInfo) []string {
	seen := make(map[string]bool)
	var aliases []string

	add := func(s string) {
		s = strings.TrimSpace(basenameAnyOS(stripQuotes(s)))
		if s == "" || isPureNumber(s) {
			return
		}
		lower := strings.ToLower(s)
		if isNoisyProcess(lower) {
			return
		}
		if seen[lower] {
			return
		}
		seen[lower] = true
		aliases = append(aliases, s)
	}

	// Process name
	add(info.Name)

	// Executable basename
	if info.ExePath != "" {
		add(info.ExePath)
	}

	// Args tokens ending in .exe
	for _, arg := range info.Args {
		arg = stripQuotes(arg)
		if strings.HasSuffix(strings.ToLower(arg), ".exe") {
			base := basenameAnyOS(arg)
			if base != "" {
				add(base)
				// Also add stripped version
				stripped := strings.TrimSuffix(base, filepath.Ext(base))
				if stripped != "" && stripped != base {
					add(stripped)
				}
			}
		}
	}

	// Steam app IDs are handled separately via ExtractSteamAppID;
	// do not include pure numeric strings as generic aliases.

	return aliases
}

// basenameAnyOS returns the last path component of p, treating both '/' and
// '\\' as separators. This is needed because Wine args often contain Windows
// paths even when picord is running on Linux.
func basenameAnyOS(p string) string {
	p = strings.TrimRight(p, "/\\")
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// stripQuotes removes a single pair of wrapping double or single quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ResolveProcessIdentities builds a full process table, enriches carrier
// processes with aliases from related processes, and returns DetectedProcesses.
func ResolveProcessIdentities() []profile.DetectedProcess {
	return resolveIdentitiesFromTable(BuildProcessTable())
}

// ResolveProcessIdentitiesLite builds a lite process table, enriches only
// processes related to the given candidate PIDs, and returns DetectedProcesses.
// This is used when scanAll=false to avoid reading expensive env/cmdline data
// for unrelated processes.
func ResolveProcessIdentitiesLite(candidatePIDs []int) []profile.DetectedProcess {
	pt := BuildProcessTableLite()

	// Phase 1: enrich candidates + descendants + ancestors.
	enrichSet := make(map[int]bool)
	for _, pid := range candidatePIDs {
		enrichSet[pid] = true
		for _, d := range pt.Descendants(pid) {
			enrichSet[d] = true
		}
		for _, a := range pt.Ancestors(pid) {
			enrichSet[a] = true
		}
	}
	var enrichList []int
	for pid := range enrichSet {
		enrichList = append(enrichList, pid)
	}
	EnrichProcessTable(pt, enrichList)

	// Phase 2: for carrier candidates with Wine/Proton clues, lightweight-read
	// same-PGID peers' env. If they share a clue, fully enrich them so the
	// carrier can inherit aliases/SteamAppID.
	var peerEnrich []int
	for _, pid := range candidatePIDs {
		info := pt.ByPID[pid]
		if info == nil || !isCarrierProcess(info.Name) {
			continue
		}
		// Only probe peers if the candidate itself has a gaming clue.
		if info.SteamAppID == "" && info.EnvHints["WINEPREFIX"] == "" &&
			info.EnvHints["STEAM_COMPAT_DATA_PATH"] == "" && info.EnvHints["PROTON_COMPAT_DATA_PATH"] == "" {
			continue
		}
		for _, peer := range pt.PgidPeers(pid) {
			if enrichSet[peer] {
				continue // already enriched
			}
			peerEnrich = append(peerEnrich, peer)
		}
	}
	if len(peerEnrich) > 0 {
		EnrichProcessEnvOnly(pt, peerEnrich)
		var fullEnrich []int
		for _, pid := range candidatePIDs {
			info := pt.ByPID[pid]
			if info == nil || !isCarrierProcess(info.Name) {
				continue
			}
			for _, peer := range pt.PgidPeers(pid) {
				if enrichSet[peer] {
					continue
				}
				if peerInfo := pt.ByPID[peer]; peerInfo != nil {
					if gate := determineSharedGate(info, peerInfo); gate != "" {
						fullEnrich = append(fullEnrich, peer)
						enrichSet[peer] = true
					}
				}
			}
		}
		if len(fullEnrich) > 0 {
			EnrichProcessTable(pt, fullEnrich)
		}
	}

	return resolveIdentitiesFromTable(pt)
}

func resolveIdentitiesFromTable(pt *ProcessTable) []profile.DetectedProcess {
	windowTitles, err := GetWindowTitles()
	if err != nil {
		log.Printf("[monitor] GetWindowTitles: %v", err)
	}

	// First pass: collect aliases and Steam AppIDs for every process
	pidAliases := make(map[int][]string)
	pidSteamAppID := make(map[int]string)
	for _, info := range pt.Procs {
		pidAliases[info.PID] = ExtractAliases(info)
		pidSteamAppID[info.PID] = info.SteamAppID
	}

	// Second pass: for carrier processes, enrich with aliases and SteamAppID
	// from related processes, tracking the relation source for observability.
	var identitySources = make(map[int][]monitorIdentitySource)
	for _, info := range pt.Procs {
		if !isCarrierProcess(info.Name) {
			continue
		}

		for _, rec := range collectRelatedRecords(pt, info.PID) {
			relInfo := pt.ByPID[rec.PID]
			if relInfo == nil {
				continue
			}

			// --- Alias propagation ---
			if relAliases, ok := pidAliases[rec.PID]; ok {
				for _, a := range relAliases {
					if isNoisyProcess(a) || containsAlias(pidAliases[info.PID], a) {
						continue
					}

					// Gate alias propagation by relation type.
					propagate := false
					switch rec.Type {
					case relDescendant:
						// Descendants are almost always the actual game.
						propagate = true
					case relAncestor:
						// Only propagate from gaming ancestors (Steam/Proton/Wine
						// launchers), never from common shells unless there is a
						// strong shared clue.
						if isGamingAncestor(relInfo) {
							propagate = true
						} else if isCommonShell(relInfo.Name) {
							// Shells only contribute if a very strong clue is shared.
							if rec.Gate == gateSharedSteamID || rec.Gate == gateSharedWinePref || rec.Gate == gateSharedCompat {
								propagate = true
							}
						}
					case relPgidPeer:
						// Already gated by determineSharedGate in collectRelatedRecords.
						propagate = true
					}

					if propagate {
						pidAliases[info.PID] = append(pidAliases[info.PID], a)
						identitySources[info.PID] = append(identitySources[info.PID], monitorIdentitySource{
							Alias:   a,
							FromPID: rec.PID,
							Type:    rec.Type,
							Gate:    rec.Gate,
						})
					}
				}
			}

			// --- SteamAppID propagation ---
			if pidSteamAppID[info.PID] == "" && pidSteamAppID[rec.PID] != "" {
				// Propagate SteamAppID from descendants or strong ancestors.
				propagateID := false
				switch rec.Type {
				case relDescendant:
					propagateID = true
				case relAncestor:
					if isGamingAncestor(relInfo) {
						propagateID = true
					}
				case relPgidPeer:
					if rec.Gate != gateDirect {
						propagateID = true
					}
				}
				if propagateID {
					pidSteamAppID[info.PID] = pidSteamAppID[rec.PID]
					identitySources[info.PID] = append(identitySources[info.PID], monitorIdentitySource{
						SteamAppID: pidSteamAppID[rec.PID],
						FromPID:    rec.PID,
						Type:       rec.Type,
						Gate:       rec.Gate,
					})
				}
			}
		}
	}

	// Third pass: build DetectedProcess output
	var processes []profile.DetectedProcess
	for _, info := range pt.Procs {
		// Skip purely internal helper processes with no meaningful identity
		if isInternalHelper(info.Name) && len(pidAliases[info.PID]) == 0 && pidSteamAppID[info.PID] == "" {
			continue
		}

		// Skip common desktop apps (browsers, Discord, file managers) that
		// should never be tracked as Rich Presence.
		if isExcludedApp(info.Name) {
			continue
		}

		// Extract DesktopID from env
		desktopID := ""
		if v := info.EnvHints["GIO_LAUNCHED_DESKTOP_FILE"]; v != "" {
			desktopID = filepath.Base(v)
			desktopID = strings.TrimSuffix(desktopID, ".desktop")
		}

		aliases := pidAliases[info.PID]
		// If this is a known emulator, try to extract the actual game title from
		// the window title and add it as an alias so the catalog can match it.
		if emuTitle := ExtractEmulatorGameTitle(info.Name, windowTitles[info.PID]); emuTitle != "" {
			aliases = append(aliases, emuTitle)
		}

		processes = append(processes, profile.DetectedProcess{
			PID:             info.PID,
			Name:            info.Name,
			WindowTitle:     windowTitles[info.PID],
			ExePath:         info.ExePath,
			Cwd:             info.Cwd,
			Args:            info.Args,
			SteamAppID:      pidSteamAppID[info.PID],
			DesktopID:       desktopID,
			Aliases:         aliases,
			IdentitySources: convertIdentitySources(identitySources[info.PID]),
		})
	}

	return processes
}

// collectRelatedRecords returns structured relation records for processes
// related to the given PID, including relation type and gate strength.
func collectRelatedRecords(pt *ProcessTable, pid int) []relationRecord {
	info := pt.ByPID[pid]
	if info == nil {
		return nil
	}

	seen := make(map[int]bool)
	seen[pid] = true // exclude self
	var result []relationRecord

	add := func(rec relationRecord) {
		if !seen[rec.PID] {
			seen[rec.PID] = true
			result = append(result, rec)
		}
	}

	// Descendants: always strong.
	for _, d := range pt.Descendants(pid) {
		add(relationRecord{PID: d, Type: relDescendant, Gate: gateDirect})
	}

	// Ancestors: mark as direct; alias propagation is gated later by
	// isGamingAncestor / isCommonShell.
	for _, a := range pt.Ancestors(pid) {
		add(relationRecord{PID: a, Type: relAncestor, Gate: gateDirect})
	}

	// PGID peers: only when they share a Wine/Proton/Steam clue.
	for _, peer := range pt.PgidPeers(pid) {
		if peerInfo := pt.ByPID[peer]; peerInfo != nil {
			if gate := determineSharedGate(info, peerInfo); gate != "" {
				add(relationRecord{PID: peer, Type: relPgidPeer, Gate: gate})
			}
		}
	}

	return result
}

// determineSharedGate returns the strongest shared Wine/Proton clue between
// two processes, or empty string if none.
func determineSharedGate(a, b *ProcessInfo) relationGate {
	if a == nil || b == nil {
		return ""
	}
	if a.SteamAppID != "" && a.SteamAppID == b.SteamAppID {
		return gateSharedSteamID
	}
	if a.EnvHints["WINEPREFIX"] != "" && a.EnvHints["WINEPREFIX"] == b.EnvHints["WINEPREFIX"] {
		return gateSharedWinePref
	}
	compatKeys := []string{"STEAM_COMPAT_DATA_PATH", "PROTON_COMPAT_DATA_PATH"}
	for _, key := range compatKeys {
		if a.EnvHints[key] != "" && a.EnvHints[key] == b.EnvHints[key] {
			return gateSharedCompat
		}
	}
	for _, key := range compatKeys {
		prefix := a.EnvHints[key]
		if prefix == "" {
			prefix = b.EnvHints[key]
		}
		if prefix == "" {
			continue
		}
		if isUnderPrefix(a.Cwd, prefix) && isUnderPrefix(b.Cwd, prefix) {
			return gateCompatSubtree
		}
		if isUnderPrefix(a.ExePath, prefix) && isUnderPrefix(b.ExePath, prefix) {
			return gateCompatSubtree
		}
	}
	return ""
}

// isUnderPrefix reports whether path is a descendant of prefix.
func isUnderPrefix(path, prefix string) bool {
	if path == "" || prefix == "" {
		return false
	}
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+string(filepath.Separator))
}

func containsAlias(aliases []string, target string) bool {
	lowerTarget := strings.ToLower(target)
	for _, a := range aliases {
		if strings.ToLower(a) == lowerTarget {
			return true
		}
	}
	return false
}

// isCommonShell returns true for terminal/shell processes that should not
// contribute aliases to carriers unless a strong shared clue is present.
func isCommonShell(name string) bool {
	lower := strings.ToLower(name)
	shells := []string{
		"bash", "zsh", "fish", "sh", "dash", "csh", "tcsh",
		"gnome-terminal-", "gnome-terminal-server",
		"konsole", "kitty", "alacritty", "terminator", "xfce4-terminal",
		"xterm", "urxvt", "rxvt",
	}
	for _, s := range shells {
		if lower == s || strings.HasPrefix(lower, s) {
			return true
		}
	}
	return false
}

// isGamingAncestor returns true if an ancestor process is likely part of the
// game launch chain (Steam, Proton, Wine launcher) rather than a generic shell.
func isGamingAncestor(info *ProcessInfo) bool {
	if info == nil {
		return false
	}
	if isCarrierProcess(info.Name) {
		return true
	}
	if isCommonShell(info.Name) {
		return false
	}
	// If the ancestor has Steam/Wine/Proton env hints, it's likely a launcher.
	for _, key := range []string{"SteamAppId", "SteamGameId", "SteamAppID", "SteamCompatAppId", "WINEPREFIX"} {
		if info.EnvHints[key] != "" {
			return true
		}
	}
	return false
}

// relationType describes how a related process is connected to the carrier.
type relationType string

const (
	relDescendant relationType = "descendant"
	relAncestor   relationType = "ancestor"
	relPgidPeer   relationType = "pgid_peer"
)

// relationGate describes the strength of the relationship clue.
type relationGate string

const (
	gateDirect         relationGate = "direct"
	gateSharedSteamID  relationGate = "shared_steam_app_id"
	gateSharedWinePref relationGate = "shared_wineprefix"
	gateSharedCompat   relationGate = "shared_compat_path"
	gateCompatSubtree  relationGate = "compat_subtree"
)

// relationRecord ties a related PID to its connection type and gate.
type relationRecord struct {
	PID  int
	Type relationType
	Gate relationGate
}

// monitorIdentitySource is the internal shape used during enrichment.
// It is converted to profile.IdentitySource at the boundary.
type monitorIdentitySource struct {
	Alias      string
	SteamAppID string
	FromPID    int
	Type       relationType
	Gate       relationGate
}

func convertIdentitySources(src []monitorIdentitySource) []profile.IdentitySource {
	out := make([]profile.IdentitySource, len(src))
	for i, s := range src {
		out[i] = profile.IdentitySource{
			Alias:      s.Alias,
			SteamAppID: s.SteamAppID,
			FromPID:    s.FromPID,
			Type:       string(s.Type),
			Gate:       string(s.Gate),
		}
	}
	return out
}

// isInternalHelper returns true for processes that are almost never the game itself.
func isInternalHelper(name string) bool {
	lower := strings.ToLower(name)
	helpers := []string{
		"wineserver", "wine-preloader", "wine64-preloader",
		"pressure-vessel-adverb", "pressure-vessel-wrap",
		"reaper", // Steam's process reaper
	}
	for _, h := range helpers {
		if lower == h {
			return true
		}
	}
	return strings.HasPrefix(lower, "pressure-vessel-")
}
