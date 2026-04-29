package profile

import (
	"regexp"
	"sort"
	"strings"
)

type DetectedProcess struct {
	PID  int
	Name string
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
		return -1

	case MatchWindowTitle:
		return -1

	case MatchRegex:
		re, err := regexp.Compile(p.Match.Value)
		if err != nil {
			return -1
		}
		if re.MatchString(proc.Name) {
			return p.Priority
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
