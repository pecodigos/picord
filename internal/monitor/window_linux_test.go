package monitor

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestGetHyprlandWindows(t *testing.T) {
	data := []byte(`[
		{"address":"0x1","mapped":true,"hidden":false,"title":"YouTube","class":"firefox","pid":1234},
		{"address":"0x2","mapped":true,"hidden":true,"title":"Hidden","class":"app","pid":5678},
		{"address":"0x3","mapped":false,"hidden":false,"title":"Unmapped","class":"app","pid":9012}
	]`)

	var windows []hyprWindow
	if err := json.Unmarshal(data, &windows); err != nil {
		t.Fatal(err)
	}

	if len(windows) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(windows))
	}
	if windows[0].Title != "YouTube" || windows[0].PID != 1234 {
		t.Errorf("unexpected first window: %+v", windows[0])
	}
}

func TestWalkSwayTree(t *testing.T) {
	root := swayNode{
		Type: "root",
		Nodes: []swayNode{
			{
				Type:  "con",
				Name:  "Terminal",
				PID:   1000,
				Nodes: []swayNode{},
				Floating: []swayNode{
					{Type: "floating_con", Name: "Popup", PID: 1001},
				},
			},
		},
	}

	titles := make(map[int]string)
	walkSwayTree(&root, titles)

	if len(titles) != 2 {
		t.Errorf("expected 2 titles, got %d", len(titles))
	}
	if titles[1000] != "Terminal" {
		t.Errorf("expected PID 1000 = Terminal, got %q", titles[1000])
	}
	if titles[1001] != "Popup" {
		t.Errorf("expected PID 1001 = Popup, got %q", titles[1001])
	}
}

func TestDetectCompositor(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "hyprland",
			env:  map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc"},
			want: "hyprland",
		},
		{
			name: "sway",
			env:  map[string]string{"SWAYSOCK": "/run/sway.sock"},
			want: "sway",
		},
		{
			name: "kde",
			env:  map[string]string{"KDE_FULL_SESSION": "true"},
			want: "kde",
		},
		{
			name: "x11",
			env:  map[string]string{"DISPLAY": ":0", "XDG_SESSION_TYPE": "x11"},
			want: "x11",
		},
		{
			name: "unknown",
			env:  map[string]string{},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Unset all compositor-related env vars first
			for _, k := range []string{"HYPRLAND_INSTANCE_SIGNATURE", "SWAYSOCK", "KDE_FULL_SESSION", "DISPLAY", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE"} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got := DetectCompositor()
			if got != tt.want {
				t.Errorf("DetectCompositor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWindowTitleCache(t *testing.T) {
	callCount := 0
	originalFetcher := windowTitleFetcher
	windowTitleFetcher = func() (map[int]string, error) {
		callCount++
		return map[int]string{1234: "TestApp"}, nil
	}
	defer func() { windowTitleFetcher = originalFetcher }()

	windowTitleCacheMu.Lock()
	cachedWindowTitlesAt = time.Time{}
	windowTitleCacheMu.Unlock()

	titles, err := GetWindowTitles()
	if err != nil {
		t.Fatalf("GetWindowTitles() error: %v", err)
	}
	if titles[1234] != "TestApp" {
		t.Fatalf("expected PID 1234 = TestApp, got %q", titles[1234])
	}
	if callCount != 1 {
		t.Fatalf("expected 1 fetch call, got %d", callCount)
	}

	titles, err = GetWindowTitles()
	if err != nil {
		t.Fatalf("GetWindowTitles() error on second call: %v", err)
	}
	if titles[1234] != "TestApp" {
		t.Fatalf("expected PID 1234 = TestApp on second call, got %q", titles[1234])
	}
	if callCount != 1 {
		t.Fatalf("expected cache hit, but fetch was called %d times", callCount)
	}
}

func TestWindowTitleCacheExpiry(t *testing.T) {
	callCount := 0
	originalFetcher := windowTitleFetcher
	windowTitleFetcher = func() (map[int]string, error) {
		callCount++
		return map[int]string{callCount * 1000: "AppFromCall"}, nil
	}
	defer func() { windowTitleFetcher = originalFetcher }()

	windowTitleCacheMu.Lock()
	cachedWindowTitlesAt = time.Now().Add(-windowTitleCacheTTL - time.Second)
	windowTitleCacheMu.Unlock()

	titles, err := GetWindowTitles()
	if err != nil {
		t.Fatalf("GetWindowTitles() error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected fresh fetch after TTL expiry, got %d calls", callCount)
	}
	_ = titles

	titles2, err := GetWindowTitles()
	if err != nil {
		t.Fatalf("GetWindowTitles() error on second call: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected cache hit after fresh fetch, got %d calls", callCount)
	}
	_ = titles2
}

func TestWindowTitleCacheConcurrency(t *testing.T) {
	callCount := 0
	var mu sync.Mutex
	originalFetcher := windowTitleFetcher
	windowTitleFetcher = func() (map[int]string, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		return map[int]string{1: "Concurrent"}, nil
	}
	defer func() { windowTitleFetcher = originalFetcher }()

	windowTitleCacheMu.Lock()
	cachedWindowTitlesAt = time.Time{}
	windowTitleCacheMu.Unlock()

	var wg sync.WaitGroup
	const goroutines = 10
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			titles, err := GetWindowTitles()
			if err != nil {
				t.Errorf("GetWindowTitles() error: %v", err)
			}
			if titles[1] != "Concurrent" {
				t.Errorf("unexpected title: %v", titles)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	count := callCount
	mu.Unlock()
	if count != 1 {
		t.Errorf("expected exactly 1 fetch from %d concurrent calls, got %d", goroutines, count)
	}
}
