package catalog

import (
	"context"
	"strings"

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

// candidate holds a match candidate with all scoring dimensions.
type candidate struct {
	MatchResult
	reasonRank int
}

// Match returns the best catalog match for a detected process, or nil if none.
// It evaluates all available candidates using a multi-dimensional score:
//   1. Effective confidence (method confidence clamped by alias confidence)
//   2. Reason priority rank
//   3. Source priority
//   4. Entry ID (stable deterministic tie-break)
func (m *Matcher) Match(ctx context.Context, proc profile.DetectedProcess) *MatchResult {
	if m.store == nil {
		return nil
	}

	var candidates []candidate

	add := func(entries []Entry, methodConfidence int, reason string) {
		rr := reasonPriority(reason)
		for _, entry := range entries {
			candidates = append(candidates, candidate{
				MatchResult: MatchResult{
					Entry:      entry,
					Confidence: methodConfidence,
					Reason:     reason,
				},
				reasonRank: rr,
			})
		}
	}

	addAliasMatches := func(matches []AliasMatch, methodConfidence int, reason string) {
		rr := reasonPriority(reason)
		for _, am := range matches {
			// Effective confidence is the lesser of the method confidence
			// and the alias confidence stored in the catalog.
			effective := methodConfidence
			if am.Confidence < effective {
				effective = am.Confidence
			}
			candidates = append(candidates, candidate{
				MatchResult: MatchResult{
					Entry:      am.Entry,
					Confidence: effective,
					Reason:     reason,
				},
				reasonRank: rr,
			})
		}
	}

	// 1. Direct Steam AppID
	if isPureNumber(proc.SteamAppID) {
		matches, _ := m.store.SearchByAliasWithConfidence(ctx, AliasSteamAppID, proc.SteamAppID)
		addAliasMatches(matches, 100, "steam_app_id")
	}

	// 2. Direct Lutris slug
	if proc.LutrisSlug != "" {
		matches, _ := m.store.SearchByAliasWithConfidence(ctx, AliasLutrisSlug, proc.LutrisSlug)
		addAliasMatches(matches, 95, "lutris_slug")
	}

	// 3. Direct Desktop ID
	if proc.DesktopID != "" {
		matches, _ := m.store.SearchByAliasWithConfidence(ctx, AliasDesktopID, proc.DesktopID)
		addAliasMatches(matches, 90, "desktop_id")
	}

	// 4. Executable name
	if proc.Name != "" {
		matches, _ := m.store.SearchByAliasWithConfidence(ctx, AliasExecutable, proc.Name)
		addAliasMatches(matches, 80, "executable")
	}

	// 5. Aliases (Wine/Proton enriched identities)
	for _, alias := range proc.Aliases {
		if isPureNumber(alias) {
			matches, _ := m.store.SearchByAliasWithConfidence(ctx, AliasSteamAppID, alias)
			addAliasMatches(matches, 95, "alias_steam_app_id:"+alias)
		}
		matches, _ := m.store.SearchByAliasWithConfidence(ctx, AliasExecutable, alias)
		addAliasMatches(matches, 85, "alias:"+alias)
		matches, _ = m.store.SearchByAliasWithConfidence(ctx, AliasTitle, alias)
		addAliasMatches(matches, 85, "alias_title:"+alias)
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

	best := candidates[0]
	for i := 1; i < len(candidates); i++ {
		c := candidates[i]
		if isBetterCandidate(c, best) {
			best = c
		}
	}

	return &best.MatchResult
}

// isBetterCandidate reports whether a should win over b using the full ranking.
func isBetterCandidate(a, b candidate) bool {
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	if a.reasonRank != b.reasonRank {
		return a.reasonRank > b.reasonRank
	}
	if spA, spB := sourcePriority(a.Entry.Source), sourcePriority(b.Entry.Source); spA != spB {
		return spA > spB
	}
	// Deterministic final tie-break: entry ID, then title.
	if a.Entry.ID != b.Entry.ID {
		return a.Entry.ID < b.Entry.ID
	}
	return a.Entry.Title < b.Entry.Title
}

// reasonPriority returns a numeric rank for a match reason.
// Higher values are stronger reasons.
func reasonPriority(reason string) int {
	switch {
	case reason == "steam_app_id":
		return 100
	case reason == "lutris_slug":
		return 95
	case reason == "desktop_id":
		return 90
	case strings.HasPrefix(reason, "alias_steam_app_id:"):
		return 85
	case strings.HasPrefix(reason, "alias:"):
		return 80
	case strings.HasPrefix(reason, "alias_title:"):
		return 75
	case reason == "executable":
		return 70
	case reason == "exact_title":
		return 65
	case reason == "exact_window_title":
		return 60
	case reason == "window_title_substring":
		return 50
	default:
		return 0
	}
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

// IsBetterThan reports whether this match result should win over another
// using the full ranking (confidence, reason, source, ID).
func (mr *MatchResult) IsBetterThan(other *MatchResult) bool {
	if other == nil {
		return true
	}
	a := candidate{MatchResult: *mr, reasonRank: reasonPriority(mr.Reason)}
	b := candidate{MatchResult: *other, reasonRank: reasonPriority(other.Reason)}
	return isBetterCandidate(a, b)
}

// sourceDisplayName returns a human-readable label for a catalog source.
func sourceDisplayName(src string) string {
	switch src {
	case "steam":
		return "Steam"
	case "steam_shortcut":
		return "Steam"
	case "lutris":
		return "Lutris"
	case "desktop":
		return "Desktop"
	default:
		return capitalize(src)
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
			Details:    mr.Entry.Title,
			State:      "Playing now",
			LargeImage: largeImage,
			LargeText:  mr.Entry.Title,
		},
		Enabled: true,
	}
}
