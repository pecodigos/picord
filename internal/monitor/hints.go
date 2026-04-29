package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var steamAppIDRe = regexp.MustCompile(`(?i)(?:AppId=|steam://rungameid/)(\d+)`)

// readProcHints collects game-identity hints from /proc/<pid> without exposing
// the full environment. Only allowlisted env keys are returned.
func readProcHints(pid int, name string) (exePath, cwd string, args []string, steamAppID, desktopID string) {
	procPath := filepath.Join(procRoot, fmt.Sprintf("%d", pid))

	// exe symlink
	if p, err := os.Readlink(filepath.Join(procPath, "exe")); err == nil {
		exePath = p
	}

	// cwd symlink
	if p, err := os.Readlink(filepath.Join(procPath, "cwd")); err == nil {
		cwd = p
	}

	// cmdline
	cmdline, err := os.ReadFile(filepath.Join(procPath, "cmdline"))
	if err == nil && len(cmdline) > 0 {
		args = strings.Split(string(cmdline), "\x00")
		// cmdline ends with a trailing null, so last element may be empty.
		for len(args) > 0 && args[len(args)-1] == "" {
			args = args[:len(args)-1]
		}
	}

	// Steam AppID from cmdline tokens
	for _, a := range args {
		if m := steamAppIDRe.FindStringSubmatch(a); m != nil {
			steamAppID = m[1]
			break
		}
	}

	// Environment allowlist
	envData, err := os.ReadFile(filepath.Join(procPath, "environ"))
	if err == nil {
		env := parseEnvironAllowlist(envData, []string{
			"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId",
			"GIO_LAUNCHED_DESKTOP_FILE",
		})
		for _, key := range []string{"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId"} {
			if v := env[key]; v != "" {
				steamAppID = v
				break
			}
		}
		if v := env["GIO_LAUNCHED_DESKTOP_FILE"]; v != "" {
			desktopID = filepath.Base(v)
			if strings.HasSuffix(desktopID, ".desktop") {
				desktopID = strings.TrimSuffix(desktopID, ".desktop")
			}
		}
	}

	return exePath, cwd, args, steamAppID, desktopID
}

func parseEnvironAllowlist(data []byte, allowed []string) map[string]string {
	result := make(map[string]string)
	allowedSet := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allowedSet[k] = true
	}

	entries := strings.Split(string(data), "\x00")
	for _, entry := range entries {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		if allowedSet[key] {
			result[key] = parts[1]
		}
	}
	return result
}

// isPureNumber returns true if the string is all digits.
func isPureNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
