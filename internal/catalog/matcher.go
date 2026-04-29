package catalog

import (
	"context"

	"github.com/pecodigos/picord/internal/profile"
)

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
func (m *Matcher) Match(ctx context.Context, proc profile.DetectedProcess) *MatchResult {
	if m.store == nil {
		return nil
	}

	// Highest confidence first.

	// 1. Steam AppID
	if proc.SteamAppID != "" {
		entries, err := m.store.SearchByAlias(ctx, AliasSteamAppID, proc.SteamAppID)
		if err == nil && len(entries) > 0 {
			return &MatchResult{Entry: entries[0], Confidence: 100, Reason: "steam_app_id"}
		}
	}

	// 2. Lutris slug
	if proc.LutrisSlug != "" {
		entries, err := m.store.SearchByAlias(ctx, AliasLutrisSlug, proc.LutrisSlug)
		if err == nil && len(entries) > 0 {
			return &MatchResult{Entry: entries[0], Confidence: 95, Reason: "lutris_slug"}
		}
	}

	// 3. Desktop ID
	if proc.DesktopID != "" {
		entries, err := m.store.SearchByAlias(ctx, AliasDesktopID, proc.DesktopID)
		if err == nil && len(entries) > 0 {
			return &MatchResult{Entry: entries[0], Confidence: 90, Reason: "desktop_id"}
		}
	}

	// 4. Executable name
	if proc.Name != "" {
		entries, err := m.store.SearchByAlias(ctx, AliasExecutable, proc.Name)
		if err == nil && len(entries) > 0 {
			return &MatchResult{Entry: entries[0], Confidence: 80, Reason: "executable"}
		}
	}

	// 5. Exact normalized title match against process name
	if proc.Name != "" {
		entries, err := m.store.ExactTitleMatch(ctx, proc.Name)
		if err == nil && len(entries) > 0 {
			return &MatchResult{Entry: entries[0], Confidence: 70, Reason: "exact_title"}
		}
	}

	// 6. Exact normalized title match against window title
	if proc.WindowTitle != "" {
		entries, err := m.store.ExactTitleMatch(ctx, proc.WindowTitle)
		if err == nil && len(entries) > 0 {
			return &MatchResult{Entry: entries[0], Confidence: 70, Reason: "exact_window_title"}
		}
	}

	// 7. Substring window title search (only if unique result)
	if proc.WindowTitle != "" {
		entries, err := m.store.SearchAll(ctx, proc.WindowTitle)
		if err == nil && len(entries) == 1 {
			return &MatchResult{Entry: entries[0], Confidence: 50, Reason: "window_title_substring"}
		}
	}

	return nil
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
