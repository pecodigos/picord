package profile

import (
	"regexp"
	"sort"
	"strings"
)

type DetectedProcess struct {
	PID         int      `json:"pid"`
	Name        string   `json:"name"`
	WindowTitle string   `json:"window_title,omitempty"`
	ExePath     string   `json:"exe_path,omitempty"`
	Cwd         string   `json:"cwd,omitempty"`
	Args        []string `json:"args,omitempty"`
	SteamAppID  string   `json:"steam_app_id,omitempty"`
	LutrisSlug  string   `json:"lutris_slug,omitempty"`
	DesktopID   string   `json:"desktop_id,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
}

func (p Profile) Matches(proc DetectedProcess) int {
	if !p.Enabled {
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
		re, err := regexp.Compile(p.Match.Value)
		if err != nil {
			return -1
		}
		if re.MatchString(proc.Name) {
			return p.Priority
		}
		// Also check aliases.
		for _, alias := range proc.Aliases {
			if re.MatchString(alias) {
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
				})
			}
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].priority != matches[j].priority {
			return matches[i].priority > matches[j].priority
		}
		return len(matches[i].profile.Match.Value) > len(matches[j].profile.Match.Value)
	})

	return matches[0].profile, matches[0].proc
}
