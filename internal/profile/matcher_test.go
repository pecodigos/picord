package profile

import (
	"testing"
)

func TestMatches_ProcessName(t *testing.T) {
	p := Profile{
		Name:     "test",
		Enabled:  true,
		Priority: 5,
		Match:    MatchRule{Type: MatchProcessName, Value: "firefox"},
	}

	if p.Matches(DetectedProcess{Name: "firefox"}) < 0 {
		t.Error("expected match for exact process name")
	}
	if p.Matches(DetectedProcess{Name: "Firefox"}) < 0 {
		t.Error("expected case-insensitive match")
	}
	if p.Matches(DetectedProcess{Name: "firefox-bin"}) >= 0 {
		t.Error("expected no match for partial name")
	}
}

func TestMatches_Disabled(t *testing.T) {
	p := Profile{
		Name:     "test",
		Enabled:  false,
		Priority: 5,
		Match:    MatchRule{Type: MatchProcessName, Value: "firefox"},
	}

	if p.Matches(DetectedProcess{Name: "firefox"}) >= 0 {
		t.Error("expected no match for disabled profile")
	}
}

func TestMatches_Regex(t *testing.T) {
	p := Profile{
		Name:     "test",
		Enabled:  true,
		Priority: 5,
		Match:    MatchRule{Type: MatchRegex, Value: `^steam.*`},
	}

	if p.Matches(DetectedProcess{Name: "steam"}) < 0 {
		t.Error("expected match for steam")
	}
	if p.Matches(DetectedProcess{Name: "steamwebhelper"}) < 0 {
		t.Error("expected match for steamwebhelper")
	}
	if p.Matches(DetectedProcess{Name: "firefox"}) >= 0 {
		t.Error("expected no match for firefox")
	}
}

func TestMatches_InvalidRegex(t *testing.T) {
	p := Profile{
		Name:     "test",
		Enabled:  true,
		Priority: 5,
		Match:    MatchRule{Type: MatchRegex, Value: `[invalid`},
	}

	if p.Matches(DetectedProcess{Name: "anything"}) >= 0 {
		t.Error("expected no match for invalid regex")
	}
}

func TestMatches_WindowTitle(t *testing.T) {
	p := Profile{
		Name:     "test",
		Enabled:  true,
		Priority: 5,
		Match:    MatchRule{Type: MatchWindowTitle, Value: "youtube"},
	}

	if p.Matches(DetectedProcess{Name: "firefox", WindowTitle: "YouTube - Mozilla Firefox"}) < 0 {
		t.Error("expected match for window title containing youtube")
	}
	if p.Matches(DetectedProcess{Name: "firefox", WindowTitle: "GitHub"}) >= 0 {
		t.Error("expected no match for unrelated window title")
	}
}

func TestFindBestMatch(t *testing.T) {
	profiles := []Profile{
		{Name: "low", Enabled: true, Priority: 1, Match: MatchRule{Type: MatchProcessName, Value: "a"}},
		{Name: "high", Enabled: true, Priority: 10, Match: MatchRule{Type: MatchProcessName, Value: "a"}},
		{Name: "mid", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "b"}},
	}
	processes := []DetectedProcess{
		{Name: "a"},
		{Name: "b"},
	}

	match, proc := FindBestMatch(profiles, processes)
	if match == nil || match.Name != "high" {
		t.Errorf("expected 'high' profile, got %v", match)
	}
	if proc == nil || proc.Name != "a" {
		t.Errorf("expected process 'a', got %v", proc)
	}
}

func TestFindBestMatch_NoMatch(t *testing.T) {
	profiles := []Profile{
		{Name: "test", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "x"}},
	}
	processes := []DetectedProcess{{Name: "y"}}

	match, proc := FindBestMatch(profiles, processes)
	if match != nil || proc != nil {
		t.Error("expected no match")
	}
}

func TestFindBestMatch_PrioritySort(t *testing.T) {
	// Same priority, longer match value should win
	profiles := []Profile{
		{Name: "short", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "steam"}},
		{Name: "long", Enabled: true, Priority: 5, Match: MatchRule{Type: MatchProcessName, Value: "steamdeck"}},
	}
	processes := []DetectedProcess{{Name: "steamdeck"}}

	match, _ := FindBestMatch(profiles, processes)
	if match == nil || match.Name != "long" {
		t.Errorf("expected 'long' profile due to longer match value, got %v", match)
	}
}
