package monitor

import (
	"regexp"
	"strings"
)

var (
	steamAppIDRe      = regexp.MustCompile(`(?i)(?:AppId=|steam://rungameid/|steam://run/)(\d+)`)
	steamAppIDArgsRe  = regexp.MustCompile(`(?i)^--appid$`)
	steamAppIDEnvKeys = []string{"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId", "SteamCompatAppId"}
)

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
