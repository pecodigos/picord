package profile

import (
	"testing"
)

func TestRenderActivity_NoTemplates(t *testing.T) {
	act := Activity{
		Details:    "Playing a game",
		State:      "In menu",
		LargeImage: "game_icon",
	}
	proc := DetectedProcess{Name: "doom", WindowTitle: "DOOM Eternal"}

	rendered := RenderActivity(act, proc)
	if rendered.Details != "Playing a game" {
		t.Errorf("details mismatch: got %q", rendered.Details)
	}
	if rendered.State != "In menu" {
		t.Errorf("state mismatch: got %q", rendered.State)
	}
}

func TestRenderActivity_ProcessName(t *testing.T) {
	act := Activity{
		Details: "Playing {process_name}",
		State:   "via {process_name}",
	}
	proc := DetectedProcess{Name: "doom"}

	rendered := RenderActivity(act, proc)
	if rendered.Details != "Playing doom" {
		t.Errorf("details mismatch: got %q", rendered.Details)
	}
	if rendered.State != "via doom" {
		t.Errorf("state mismatch: got %q", rendered.State)
	}
}

func TestRenderActivity_WindowTitle(t *testing.T) {
	act := Activity{
		Details: "Watching {window_title}",
		State:   "on {window_title}",
	}
	proc := DetectedProcess{Name: "firefox", WindowTitle: "YouTube"}

	rendered := RenderActivity(act, proc)
	if rendered.Details != "Watching YouTube" {
		t.Errorf("details mismatch: got %q", rendered.Details)
	}
	if rendered.State != "on YouTube" {
		t.Errorf("state mismatch: got %q", rendered.State)
	}
}

func TestRenderActivity_Mixed(t *testing.T) {
	act := Activity{
		Details:    "{process_name}: {window_title}",
		LargeText:  "Playing {window_title}",
	}
	proc := DetectedProcess{Name: "steam", WindowTitle: "Half-Life 3"}

	rendered := RenderActivity(act, proc)
	if rendered.Details != "steam: Half-Life 3" {
		t.Errorf("details mismatch: got %q", rendered.Details)
	}
	if rendered.LargeText != "Playing Half-Life 3" {
		t.Errorf("large_text mismatch: got %q", rendered.LargeText)
	}
}

func TestRenderActivity_EmptyWindowTitle(t *testing.T) {
	act := Activity{
		Details: "Playing {window_title}",
	}
	proc := DetectedProcess{Name: "terminal", WindowTitle: ""}

	rendered := RenderActivity(act, proc)
	if rendered.Details != "Playing " {
		t.Errorf("details mismatch: got %q", rendered.Details)
	}
}
