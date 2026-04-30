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
		s = strings.TrimSpace(s)
		if s == "" {
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
		add(filepath.Base(info.ExePath))
	}

	// Args tokens ending in .exe
	for _, arg := range info.Args {
		if strings.HasSuffix(strings.ToLower(arg), ".exe") {
			base := filepath.Base(arg)
			add(base)
			// Also add stripped version
			stripped := strings.TrimSuffix(base, filepath.Ext(base))
			add(stripped)
		}
	}

	// Steam app id from env
	for _, key := range []string{"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId", "SteamCompatAppId"} {
		if v := info.EnvHints[key]; v != "" {
			add(v)
		}
	}

	return aliases
}

// ResolveProcessIdentities builds a full process table, enriches carrier
// processes with aliases from related processes, and returns DetectedProcesses.
func ResolveProcessIdentities() []profile.DetectedProcess {
	pt := BuildProcessTable()
	windowTitles, _ := GetWindowTitles()

	// First pass: collect aliases for every process
	pidAliases := make(map[int][]string)
	for _, info := range pt.Procs {
		pidAliases[info.PID] = ExtractAliases(info)
	}

	// Second pass: for carrier processes, enrich with aliases from related processes
	for _, info := range pt.Procs {
		if !isCarrierProcess(info.Name) {
			continue
		}

		related := collectRelatedPIDs(pt, info.PID)
		for _, relPID := range related {
			if relAliases, ok := pidAliases[relPID]; ok {
				for _, a := range relAliases {
					// Only add non-noisy aliases not already present
					if !isNoisyProcess(a) && !containsAlias(pidAliases[info.PID], a) {
						pidAliases[info.PID] = append(pidAliases[info.PID], a)
					}
				}
			}
		}
	}

	// Third pass: build DetectedProcess output
	var processes []profile.DetectedProcess
	for _, info := range pt.Procs {
		// Skip purely internal helper processes with no meaningful identity
		if isInternalHelper(info.Name) && len(pidAliases[info.PID]) == 0 {
			continue
		}

		// Extract Steam AppID from env
		steamAppID := ""
		for _, key := range []string{"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId"} {
			if v := info.EnvHints[key]; v != "" {
				steamAppID = v
				break
			}
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
			SteamAppID:  steamAppID,
			DesktopID:   desktopID,
			Aliases:     pidAliases[info.PID],
		})
	}

	return processes
}

// collectRelatedPIDs returns PIDs of processes related to the given PID:
// descendants, ancestors, pgid peers, and sid peers.
func collectRelatedPIDs(pt *ProcessTable, pid int) []int {
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

	add(pt.Descendants(pid))
	add(pt.Ancestors(pid))
	add(pt.PgidPeers(pid))
	add(pt.SidPeers(pid))

	return result
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
