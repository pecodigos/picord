package catalog

import (
	"context"

	"github.com/pecodigos/picord/internal/profile"
)

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

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

// Matcher looks up catalog entries from detected process hints.
type Matcher struct {
	store *Store
}

func NewMatcher(store *Store) *Matcher {
	return &Matcher{store: store}
}

// Match returns the best catalog match for a detected process, or nil if none.
// It evaluates all available candidates and returns the highest-confidence match.
func (m *Matcher) Match(ctx context.Context, proc profile.DetectedProcess) *MatchResult {
	if m.store == nil {
		return nil
	}

	var candidates []MatchResult

	// Helper to append every entry from a search as a candidate. Alias and exact
	// title searches can legally return multiple entries; tie-breaking below only
	// works if all candidates are present.
	add := func(entries []Entry, confidence int, reason string) {
		for _, entry := range entries {
			candidates = append(candidates, MatchResult{
				Entry:      entry,
				Confidence: confidence,
				Reason:     reason,
			})
		}
	}

	// 1. Steam AppID
	if isPureNumber(proc.SteamAppID) {
		entries, _ := m.store.SearchByAlias(ctx, AliasSteamAppID, proc.SteamAppID)
		add(entries, 100, "steam_app_id")
	}

	// 2. Lutris slug
	if proc.LutrisSlug != "" {
		entries, _ := m.store.SearchByAlias(ctx, AliasLutrisSlug, proc.LutrisSlug)
		add(entries, 95, "lutris_slug")
	}

	// 3. Desktop ID
	if proc.DesktopID != "" {
		entries, _ := m.store.SearchByAlias(ctx, AliasDesktopID, proc.DesktopID)
		add(entries, 90, "desktop_id")
	}

	// 4. Executable name
	if proc.Name != "" {
		entries, _ := m.store.SearchByAlias(ctx, AliasExecutable, proc.Name)
		add(entries, 80, "executable")
	}

	// 5. Aliases (Wine/Proton enriched identities)
	for _, alias := range proc.Aliases {
		// Numeric aliases may be Steam AppIDs from related processes
		if isPureNumber(alias) {
			entries, _ := m.store.SearchByAlias(ctx, AliasSteamAppID, alias)
			add(entries, 95, "alias_steam_app_id:"+alias)
		}
		entries, _ := m.store.SearchByAlias(ctx, AliasExecutable, alias)
		add(entries, 85, "alias:"+alias)
		entries, _ = m.store.SearchByAlias(ctx, AliasTitle, alias)
		add(entries, 85, "alias_title:"+alias)
	}

	// 6. Exact normalized title match against process name
	if proc.Name != "" {
		entries, _ := m.store.ExactTitleMatch(ctx, proc.Name)
		add(entries, 70, "exact_title")
	}

	// 7. Exact normalized title match against window title
	if proc.WindowTitle != "" {
		entries, _ := m.store.ExactTitleMatch(ctx, proc.WindowTitle)
		add(entries, 70, "exact_window_title")
	}

	// 8. Substring window title search (only if unique result)
	if proc.WindowTitle != "" {
		entries, _ := m.store.SearchAll(ctx, proc.WindowTitle)
		if len(entries) == 1 {
			add(entries, 50, "window_title_substring")
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Select highest confidence; tie-break by source priority and specificity.
	best := &candidates[0]
	for i := 1; i < len(candidates); i++ {
		c := &candidates[i]
		if c.Confidence > best.Confidence {
			best = c
			continue
		}
		if c.Confidence == best.Confidence {
			// Tie-break: prefer Steam over Lutris over Desktop over others.
			if sourcePriority(c.Entry.Source) > sourcePriority(best.Entry.Source) {
				best = c
			}
		}
	}

	return best
}

// sourcePriority returns a numeric priority for catalog sources.
// Higher values win ties.
func sourcePriority(src string) int {
	switch src {
	case "steam":
		return 100
	case "steam_shortcut":
		return 95
	case "lutris":
		return 90
	case "desktop":
		return 80
	default:
		return 50
	}
}

// ToProfile converts a MatchResult into an ephemeral Profile for Rich Presence.
func (mr *MatchResult) ToProfile(imgResolver ImageResolver) profile.Profile {
	if mr == nil {
		return profile.Profile{}
	}

	largeImage := imgResolver.Resolve(mr.Entry, "")

	return profile.Profile{
		Name: mr.Entry.Title,
		Activity: profile.Activity{
			Details:    "Playing " + mr.Entry.Title,
			State:      capitalize(string(mr.Entry.Source)),
			LargeImage: largeImage,
			LargeText:  mr.Entry.Title,
		},
		Enabled: true,
	}
}
