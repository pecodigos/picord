package monitor

import (
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
	if strings.HasPrefix(lower, "pressure-vessel-") {
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

	// Determine which PIDs need expensive enrichment:
	// candidates + their descendants + their ancestors.
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

	return resolveIdentitiesFromTable(pt)
}

func resolveIdentitiesFromTable(pt *ProcessTable) []profile.DetectedProcess {
	windowTitles, _ := GetWindowTitles()

	// First pass: collect aliases and Steam AppIDs for every process
	pidAliases := make(map[int][]string)
	pidSteamAppID := make(map[int]string)
	for _, info := range pt.Procs {
		pidAliases[info.PID] = ExtractAliases(info)
		pidSteamAppID[info.PID] = info.SteamAppID
	}

	// Second pass: for carrier processes, enrich with aliases and SteamAppID
	// from related processes.
	for _, info := range pt.Procs {
		if !isCarrierProcess(info.Name) {
			continue
		}

		related := collectRelatedPIDs(pt, info.PID)
		for _, relPID := range related {
			// Propagate aliases
			if relAliases, ok := pidAliases[relPID]; ok {
				for _, a := range relAliases {
					// Only add non-noisy aliases not already present
					if !isNoisyProcess(a) && !containsAlias(pidAliases[info.PID], a) {
						pidAliases[info.PID] = append(pidAliases[info.PID], a)
					}
				}
			}
			// Propagate SteamAppID if carrier doesn't have one
			if pidSteamAppID[info.PID] == "" && pidSteamAppID[relPID] != "" {
				pidSteamAppID[info.PID] = pidSteamAppID[relPID]
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

		// Extract DesktopID from env
		desktopID := ""
		if v := info.EnvHints["GIO_LAUNCHED_DESKTOP_FILE"]; v != "" {
			desktopID = filepath.Base(v)
			desktopID = strings.TrimSuffix(desktopID, ".desktop")
		}

		processes = append(processes, profile.DetectedProcess{
			PID:         info.PID,
			Name:        info.Name,
			WindowTitle: windowTitles[info.PID],
			ExePath:     info.ExePath,
			Cwd:         info.Cwd,
			Args:        info.Args,
			SteamAppID:  pidSteamAppID[info.PID],
			DesktopID:   desktopID,
			Aliases:     pidAliases[info.PID],
		})
	}

	return processes
}

// collectRelatedPIDs returns PIDs of processes related to the given PID.
// It always includes descendants and ancestors (strong tree relations).
// It only includes pgid/sid peers when they share a Wine/Proton/Steam clue
// with the target process, to avoid false positives from unrelated desktop apps.
func collectRelatedPIDs(pt *ProcessTable, pid int) []int {
	info := pt.ByPID[pid]
	if info == nil {
		return nil
	}

	seen := make(map[int]bool)
	seen[pid] = true // exclude self
	var result []int

	add := func(pids []int) {
		for _, p := range pids {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}

	// Strong relations: descendants and ancestors are always included.
	add(pt.Descendants(pid))
	add(pt.Ancestors(pid))

	// PGID peers: only when they share a Wine/Proton/Steam clue.
	for _, peer := range pt.PgidPeers(pid) {
		if peerInfo := pt.ByPID[peer]; peerInfo != nil {
			if sharesWineProtonClue(info, peerInfo) {
				add([]int{peer})
			}
		}
	}

	// SID peers: disabled by default to avoid broad false positives.
	// (If needed later, gate them the same way as PGID peers.)

	return result
}

// sharesWineProtonClue returns true if two processes share a gaming-platform
// hint that makes them likely part of the same Wine/Proton/Steam launch.
func sharesWineProtonClue(a, b *ProcessInfo) bool {
	if a == nil || b == nil {
		return false
	}

	// Same Steam app ID
	if a.SteamAppID != "" && a.SteamAppID == b.SteamAppID {
		return true
	}

	// Same WINEPREFIX
	if a.EnvHints["WINEPREFIX"] != "" && a.EnvHints["WINEPREFIX"] == b.EnvHints["WINEPREFIX"] {
		return true
	}

	// Same Steam compatibility data path
	compatKeys := []string{"STEAM_COMPAT_DATA_PATH", "PROTON_COMPAT_DATA_PATH"}
	for _, key := range compatKeys {
		if a.EnvHints[key] != "" && a.EnvHints[key] == b.EnvHints[key] {
			return true
		}
	}

	// Same executable/cwd subtree under a Steam compatibility prefix
	for _, key := range compatKeys {
		prefix := a.EnvHints[key]
		if prefix == "" {
			prefix = b.EnvHints[key]
		}
		if prefix == "" {
			continue
		}
		if isUnderPrefix(a.Cwd, prefix) && isUnderPrefix(b.Cwd, prefix) {
			return true
		}
		if isUnderPrefix(a.ExePath, prefix) && isUnderPrefix(b.ExePath, prefix) {
			return true
		}
	}

	return false
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

// isInternalHelper returns true for processes that are almost never the game itself.
func isInternalHelper(name string) bool {
	lower := strings.ToLower(name)
	helpers := []string{
		"wineserver", "wine-preloader", "wine64-preloader",
		"pressure-vessel-adverb", "pressure-vessel-wrap",
	}
	for _, h := range helpers {
		if lower == h {
			return true
		}
	}
	if strings.HasPrefix(lower, "pressure-vessel-") {
		return true
	}
	return false
}
