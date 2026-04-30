package profile

import (
	"regexp"
	"sort"
	"strings"
)

type DetectedProcess struct {
	PID             int              `json:"pid"`
	Name            string           `json:"name"`
	WindowTitle     string           `json:"window_title,omitempty"`
	ExePath         string           `json:"exe_path,omitempty"`
	Cwd             string           `json:"cwd,omitempty"`
	Args            []string         `json:"args,omitempty"`
	SteamAppID      string           `json:"steam_app_id,omitempty"`
	LutrisSlug      string           `json:"lutris_slug,omitempty"`
	DesktopID       string           `json:"desktop_id,omitempty"`
	Aliases         []string         `json:"aliases,omitempty"`
	IdentitySources []IdentitySource `json:"identity_sources,omitempty"`
}

// IdentitySource records why an alias or SteamAppID was added to a process.
type IdentitySource struct {
	Alias      string `json:"alias,omitempty"`
	SteamAppID string `json:"steam_app_id,omitempty"`
	FromPID    int    `json:"from_pid"`
	Type       string `json:"type"`
	Gate       string `json:"gate"`
}

// MatchInfo holds diagnostics about why a particular presence was selected.
type MatchInfo struct {
	Source       string `json:"source"`                  // "profile", "catalog", "default", "none"
	ProfileName  string `json:"profile_name,omitempty"`  // matched profile name
	ProcessName  string `json:"process_name,omitempty"`  // process that matched
	Reason       string `json:"reason,omitempty"`        // e.g. "steam_app_id", "alias:Game.exe"
	Confidence   int    `json:"confidence,omitempty"`    // 0-100
	DiscordAppID string `json:"discord_app_id,omitempty"`// active Discord application ID
	RPConnected  bool   `json:"rpc_connected"`           // Discord IPC connected
}

func (p Profile) Matches(proc DetectedProcess) int {
	if !p.Enabled {
		return -1
	}

	// Reject blank match values to prevent overly broad matches.
	if strings.TrimSpace(p.Match.Value) == "" {
		return -1
	}

	matchVal := strings.ToLower(p.Match.Value)
	procName := strings.ToLower(proc.Name)

	switch p.Match.Type {
	case MatchProcessName:
		if procName == matchVal {
			return p.Priority
		}
		// Also check aliases for Wine/Proton carrier processes.
		for _, alias := range proc.Aliases {
			if strings.ToLower(alias) == matchVal {
				return p.Priority
			}
		}
		return -1

	case MatchWindowTitle:
		if strings.Contains(strings.ToLower(proc.WindowTitle), matchVal) {
			return p.Priority
		}
		return -1

	case MatchRegex:
		if p.regexCache == nil {
			re, err := regexp.Compile(p.Match.Value)
			if err != nil {
				return -1
			}
			p.regexCache = re
		}
		if p.regexCache.MatchString(proc.Name) {
			return p.Priority
		}
		// Also check aliases.
		for _, alias := range proc.Aliases {
			if p.regexCache.MatchString(alias) {
				return p.Priority
			}
		}
		return -1

	default:
		return -1
	}
}

func FindBestMatch(profiles []Profile, processes []DetectedProcess) (*Profile, *DetectedProcess) {
	type match struct {
		profile  *Profile
		proc     *DetectedProcess
		priority int
		idx      int // original profile index for stable tie-breaking
	}

	var matches []match
	for i := range profiles {
		for j := range processes {
			prio := profiles[i].Matches(processes[j])
			if prio >= 0 {
				matches = append(matches, match{
					profile:  &profiles[i],
					proc:     &processes[j],
					priority: prio,
					idx:      i,
				})
			}
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].priority != matches[j].priority {
			return matches[i].priority > matches[j].priority
		}
		// Match-type specificity: exact process name > window title > regex.
		ti, tj := matchTypePriority(matches[i].profile.Match.Type), matchTypePriority(matches[j].profile.Match.Type)
		if ti != tj {
			return ti > tj
		}
		// Longer match value wins (more specific).
		if len(matches[i].profile.Match.Value) != len(matches[j].profile.Match.Value) {
			return len(matches[i].profile.Match.Value) > len(matches[j].profile.Match.Value)
		}
		// Stable final tie-break: first configured profile wins.
		return matches[i].idx < matches[j].idx
	})

	return matches[0].profile, matches[0].proc
}

// matchTypePriority returns a numeric specificity for a match type.
// Higher values mean more specific / preferred.
func matchTypePriority(t MatchType) int {
	switch t {
	case MatchProcessName:
		return 100
	case MatchWindowTitle:
		return 80
	case MatchRegex:
		return 60
	default:
		return 0
	}
}
