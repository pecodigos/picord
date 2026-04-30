package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	steamAppIDRe      = regexp.MustCompile(`(?i)(?:AppId=|steam://rungameid/|steam://run/)(\d+)`)
	steamAppIDArgsRe  = regexp.MustCompile(`(?i)^--appid$`)
	steamAppIDEnvKeys = []string{"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId", "SteamCompatAppId"}
)

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

	// Environment allowlist
	env := map[string]string{}
	envData, err := os.ReadFile(filepath.Join(procPath, "environ"))
	if err == nil {
		allowed := append([]string{}, steamAppIDEnvKeys...)
		allowed = append(allowed, "GIO_LAUNCHED_DESKTOP_FILE")
		env = parseEnvironAllowlist(envData, allowed)
		if v := env["GIO_LAUNCHED_DESKTOP_FILE"]; v != "" {
			desktopID = filepath.Base(v)
			if strings.HasSuffix(desktopID, ".desktop") {
				desktopID = strings.TrimSuffix(desktopID, ".desktop")
			}
		}
	}
	steamAppID = ExtractSteamAppID(args, env)

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
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ExtractSteamAppID searches args and env hints for a Steam application ID.
// It checks both command-line tokens and allowlisted environment variables.
func ExtractSteamAppID(args []string, envHints map[string]string) string {
	// 1. Search args for patterns like AppId=620, steam://rungameid/620, --appid 620
	for i, a := range args {
		if m := steamAppIDRe.FindStringSubmatch(a); m != nil {
			return m[1]
		}
		// Check --appid <next-arg>
		if steamAppIDArgsRe.MatchString(a) && i+1 < len(args) {
			if v := args[i+1]; isPureNumber(v) {
				return v
			}
		}
	}

	// 2. Search env hints in priority order
	for _, key := range steamAppIDEnvKeys {
		if v := envHints[key]; v != "" && isPureNumber(v) {
			return v
		}
	}

	return ""
}
