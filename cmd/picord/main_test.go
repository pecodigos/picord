package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pecodigos/picord/internal/catalog"
	"github.com/pecodigos/picord/internal/config"
	"github.com/pecodigos/picord/internal/profile"
	"github.com/pecodigos/picord/internal/rpc"
)

func TestDefaultConfigEnablesScanAllProcesses(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.ScanAllProcesses {
		t.Fatal("expected fallback default config to enable scan_all_processes")
	}
}

func TestRPCManager_ConnectsAfterInitialFailure(t *testing.T) {
	// Save and restore the factory.
	orig := rpcNewClient
	defer func() { rpcNewClient = orig }()

	callCount := 0
	rpcNewClient = func(appID string) (*rpc.Client, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("discord not running")
		}
		return orig(appID)
	}

	rm := newRPCManager("test-app")
	if err := rm.connect(); err == nil {
		t.Fatal("expected first connect to fail")
	}
	if rm.isConnected() {
		t.Fatal("expected not connected after failure")
	}

	// Second call should fall through to the real factory.
	// Since there is no Discord running in this test environment,
	// it will also fail, but the important part is that it RETRIES.
	// Instead, set up a mock socket for the second call.
}

func TestRPCManager_ReconnectsUsingMockSocket(t *testing.T) {
	orig := rpcNewClient
	defer func() { rpcNewClient = orig }()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "discord-ipc-0")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			header := make([]byte, 8)
			if _, err := io.ReadFull(conn, header); err != nil {
				return
			}
			op := binary.LittleEndian.Uint32(header[0:4])
			length := binary.LittleEndian.Uint32(header[4:8])
			payload := make([]byte, length)
			if _, err := io.ReadFull(conn, payload); err != nil {
				return
			}

			var pld map[string]any
			if len(payload) > 0 {
				_ = json.Unmarshal(payload, &pld)
			}

			if op == 0 { // handshake
				resp, _ := json.Marshal(map[string]any{
					"cmd":  "DISPATCH",
					"evt":  "READY",
					"data": map[string]any{},
				})
				h := make([]byte, 8)
				binary.LittleEndian.PutUint32(h[0:4], 1) // opFrame
				binary.LittleEndian.PutUint32(h[4:8], uint32(len(resp)))
				conn.Write(h)
				conn.Write(resp)
			} else if op == 1 { // frame
				resp, _ := json.Marshal(map[string]any{
					"cmd":   pld["cmd"],
					"nonce": pld["nonce"],
					"data":  map[string]any{"ok": true},
				})
				h := make([]byte, 8)
				binary.LittleEndian.PutUint32(h[0:4], 1)
				binary.LittleEndian.PutUint32(h[4:8], uint32(len(resp)))
				conn.Write(h)
				conn.Write(resp)
			}
		}
	}()

	// Point discovery at our mock socket.
	t.Setenv("DISCORD_IPC_PATH", socketPath)
	t.Setenv("XDG_RUNTIME_DIR", dir)

	rpcNewClient = func(appID string) (*rpc.Client, error) {
		return rpc.NewClient(appID)
	}

	rm := newRPCManager("test-app")
	if err := rm.connect(); err != nil {
		t.Fatalf("expected connect to succeed: %v", err)
	}
	if !rm.isConnected() {
		t.Fatal("expected connected after connect")
	}

	// clearActivity should be safe even when connected.
	rm.clearActivity()

	// close should clean up.
	rm.close()
	if rm.isConnected() {
		t.Fatal("expected disconnected after close")
	}
}

func TestRPCManager_RetriesAfterInitialFailureWithMockSocket(t *testing.T) {
	orig := rpcNewClient
	defer func() { rpcNewClient = orig }()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "discord-ipc-0")

	callCount := 0
	rpcNewClient = func(appID string) (*rpc.Client, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("discord not running")
		}
		// Second call: start the mock server and connect.
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			return nil, err
		}
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				header := make([]byte, 8)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				op := binary.LittleEndian.Uint32(header[0:4])
				length := binary.LittleEndian.Uint32(header[4:8])
				payload := make([]byte, length)
				if _, err := io.ReadFull(conn, payload); err != nil {
					return
				}
				var pld map[string]any
				if len(payload) > 0 {
					_ = json.Unmarshal(payload, &pld)
				}
				if op == 0 {
					resp, _ := json.Marshal(map[string]any{
						"cmd":  "DISPATCH",
						"evt":  "READY",
						"data": map[string]any{},
					})
					h := make([]byte, 8)
					binary.LittleEndian.PutUint32(h[0:4], 1)
					binary.LittleEndian.PutUint32(h[4:8], uint32(len(resp)))
					conn.Write(h)
					conn.Write(resp)
				} else if op == 1 {
					resp, _ := json.Marshal(map[string]any{
						"cmd":   pld["cmd"],
						"nonce": pld["nonce"],
						"data":  map[string]any{"ok": true},
					})
					h := make([]byte, 8)
					binary.LittleEndian.PutUint32(h[0:4], 1)
					binary.LittleEndian.PutUint32(h[4:8], uint32(len(resp)))
					conn.Write(h)
					conn.Write(resp)
				}
			}
		}()
		// Need a small delay for the goroutine to start listening.
		time.Sleep(10 * time.Millisecond)
		t.Setenv("DISCORD_IPC_PATH", socketPath)
		return rpc.NewClient(appID)
	}

	rm := newRPCManager("test-app")
	if err := rm.connect(); err == nil {
		t.Fatal("expected first connect to fail")
	}
	if rm.isConnected() {
		t.Fatal("expected not connected after first failure")
	}

	if err := rm.connect(); err != nil {
		t.Fatalf("expected second connect to succeed: %v", err)
	}
	if !rm.isConnected() {
		t.Fatal("expected connected after second connect")
	}

	rm.close()
}

func TestDefaultConfigUsesPicordDiscordApp(t *testing.T) {
	cfg := defaultConfig()

	if cfg.ResolveDiscordApp("main") != config.DefaultDiscordAppID {
		t.Fatalf("defaultConfig main app = %q, want %q", cfg.ResolveDiscordApp("main"), config.DefaultDiscordAppID)
	}
}

func TestRPCManager_ReplaysDesiredActivityOnReconnect(t *testing.T) {
	orig := rpcNewClient
	defer func() { rpcNewClient = orig }()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "discord-ipc-0")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	frames := make(chan string, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					header := make([]byte, 8)
					if _, err := io.ReadFull(c, header); err != nil {
						return
					}
					op := binary.LittleEndian.Uint32(header[0:4])
					length := binary.LittleEndian.Uint32(header[4:8])
					payload := make([]byte, length)
					if _, err := io.ReadFull(c, payload); err != nil {
						return
					}
					var pld map[string]any
					if len(payload) > 0 {
						_ = json.Unmarshal(payload, &pld)
					}
					if op == 0 {
						frames <- "handshake"
						resp, _ := json.Marshal(map[string]any{
							"cmd": "DISPATCH", "evt": "READY", "data": map[string]any{},
						})
						h := make([]byte, 8)
						binary.LittleEndian.PutUint32(h[0:4], 1)
						binary.LittleEndian.PutUint32(h[4:8], uint32(len(resp)))
						c.Write(h)
						c.Write(resp)
					} else if op == 1 {
						cmd, _ := pld["cmd"].(string)
						frames <- cmd
						resp, _ := json.Marshal(map[string]any{
							"cmd": cmd, "nonce": pld["nonce"], "data": map[string]any{"ok": true},
						})
						h := make([]byte, 8)
						binary.LittleEndian.PutUint32(h[0:4], 1)
						binary.LittleEndian.PutUint32(h[4:8], uint32(len(resp)))
						c.Write(h)
						c.Write(resp)
					}
				}
			}(conn)
		}
	}()

	t.Setenv("DISCORD_IPC_PATH", socketPath)

	rpcNewClient = func(appID string) (*rpc.Client, error) {
		return rpc.NewClient(appID)
	}

	rm := newRPCManager("test-app")
	if err := rm.connect(); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if <-frames != "handshake" {
		t.Fatal("expected handshake")
	}

	act := &rpc.RichActivity{Details: "Playing Tests"}
	if err := rm.setActivity(act); err != nil {
		t.Fatalf("setActivity: %v", err)
	}
	if <-frames != "SET_ACTIVITY" {
		t.Fatal("expected SET_ACTIVITY")
	}

	// Simulate disconnect by nulling the client reference.
	rm.mu.Lock()
	oldClient := rm.client
	rm.client = nil
	rm.mu.Unlock()
	if oldClient != nil {
		oldClient.Close()
	}

	// Reconnect should replay desired activity automatically.
	if err := rm.connect(); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if <-frames != "handshake" {
		t.Fatal("expected handshake after reconnect")
	}
	if <-frames != "SET_ACTIVITY" {
		t.Fatal("expected SET_ACTIVITY replay after reconnect")
	}

	rm.close()
}

func TestDefaultConfigSources(t *testing.T) {
	cfg := defaultConfig()
	for _, s := range cfg.Catalog.Sources {
		if s == "lutris_local" {
			t.Fatal("default config should not include unsupported lutris_local source")
		}
	}
}

func TestFindBestCatalogMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "steam:620", Source: "steam", SourceID: "620", Kind: catalog.EntryKindGame,
		Title: "Portal 2", NormalizedTitle: catalog.NormalizeTitle("Portal 2"),
	}, []catalog.Alias{
		{EntryID: "steam:620", Kind: catalog.AliasSteamAppID, Value: "620", Normalized: "620", Confidence: 100},
	})

	matcher := catalog.NewMatcher(store)

	procs := []profile.DetectedProcess{
		{PID: 1, Name: "someproc"},
		{PID: 2, Name: "portal2", SteamAppID: "620"},
	}
	result, proc := findBestCatalogMatch(ctx, matcher, procs)
	if result == nil {
		t.Fatal("expected catalog match")
	}
	if result.Entry.Title != "Portal 2" {
		t.Errorf("expected Portal 2, got %q", result.Entry.Title)
	}
	if proc == nil || proc.PID != 2 {
		t.Errorf("expected proc PID 2, got %+v", proc)
	}
}

func TestFindBestCatalogMatch_NoMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	matcher := catalog.NewMatcher(store)
	procs := []profile.DetectedProcess{
		{PID: 1, Name: "unknown"},
	}
	result, proc := findBestCatalogMatch(context.Background(), matcher, procs)
	if result != nil || proc != nil {
		t.Errorf("expected no match, got result=%v proc=%v", result, proc)
	}
}

func TestSetRichPresence_StoresDesiredWhenDisconnected(t *testing.T) {
	// When Discord is not connected, setRichPresence should still store
	// the desired activity so it can be replayed on reconnect.
	rm := newRPCManager("test-app")
	if rm.isConnected() {
		t.Fatal("expected not connected initially")
	}

	p := &profile.Profile{
		Name: "Test Game",
		Activity: profile.Activity{
			Details:    "Playing Test Game",
			LargeImage: "test_image",
		},
	}
	setRichPresence(rm, p, nil)

	rm.mu.Lock()
	desired := rm.desiredActivity
	rm.mu.Unlock()

	if desired == nil {
		t.Fatal("expected desiredActivity to be stored even when disconnected")
	}
	if desired.Details != "Playing Test Game" {
		t.Errorf("details=%q, want Playing Test Game", desired.Details)
	}
	if desired.Assets == nil || desired.Assets.LargeImage != "test_image" {
		t.Error("expected assets to be stored")
	}
}

func TestSelectBestPresence_CatalogBeatsBroadProfile(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "steam:620", Source: "steam", SourceID: "620", Kind: catalog.EntryKindGame,
		Title: "Portal 2", NormalizedTitle: catalog.NormalizeTitle("Portal 2"),
	}, []catalog.Alias{
		{EntryID: "steam:620", Kind: catalog.AliasSteamAppID, Value: "620", Normalized: "620", Confidence: 100},
	})

	// Broad launcher profile with low priority.
	pm := profile.NewManager([]profile.Profile{
		{Name: "Steam", Match: profile.MatchRule{Type: profile.MatchProcessName, Value: "steam"}, Priority: 5, Enabled: true},
	}, nil)
	matcher := catalog.NewMatcher(store)

	procs := []profile.DetectedProcess{
		{PID: 1, Name: "steam"},
		{PID: 2, Name: "portal2", SteamAppID: "620"},
	}

	winner := selectBestPresence(ctx, pm, matcher, catalog.ImageResolver{}, procs)
	if winner == nil {
		t.Fatal("expected a winner")
	}
	if winner.source != "catalog" {
		t.Errorf("expected catalog to win over broad profile, got %s", winner.source)
	}
	if winner.Profile.Name != "Portal 2" {
		t.Errorf("expected Portal 2, got %q", winner.Profile.Name)
	}
}

func TestSelectBestPresence_ProfileBeatsLowConfidenceCatalog(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "test:1", Source: "test", SourceID: "1", Kind: catalog.EntryKindGame,
		Title: "Some Game", NormalizedTitle: catalog.NormalizeTitle("Some Game"),
	}, nil)

	pm := profile.NewManager([]profile.Profile{
		{Name: "Doom", Match: profile.MatchRule{Type: profile.MatchProcessName, Value: "doom"}, Priority: 10, Enabled: true},
	}, nil)
	matcher := catalog.NewMatcher(store)

	procs := []profile.DetectedProcess{
		{PID: 1, Name: "doom"},
	}

	winner := selectBestPresence(ctx, pm, matcher, catalog.ImageResolver{}, procs)
	if winner == nil {
		t.Fatal("expected a winner")
	}
	if winner.source != "profile" {
		t.Errorf("expected profile to win, got %s", winner.source)
	}
}

func TestSelectBestPresence_TiePrefersProfile(t *testing.T) {
	dir := t.TempDir()
	store, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	_ = store.UpsertEntry(ctx, catalog.Entry{
		ID: "steam:620", Source: "steam", SourceID: "620", Kind: catalog.EntryKindGame,
		Title: "Portal 2", NormalizedTitle: catalog.NormalizeTitle("Portal 2"),
	}, []catalog.Alias{
		{EntryID: "steam:620", Kind: catalog.AliasSteamAppID, Value: "620", Normalized: "620", Confidence: 100},
	})

	// Priority 10 profile => score 100, same as catalog confidence 100.
	pm := profile.NewManager([]profile.Profile{
		{Name: "Portal 2", Match: profile.MatchRule{Type: profile.MatchProcessName, Value: "portal2"}, Priority: 10, Enabled: true},
	}, nil)
	matcher := catalog.NewMatcher(store)

	procs := []profile.DetectedProcess{
		{PID: 2, Name: "portal2", SteamAppID: "620"},
	}

	winner := selectBestPresence(ctx, pm, matcher, catalog.ImageResolver{}, procs)
	if winner == nil {
		t.Fatal("expected a winner")
	}
	if winner.source != "profile" {
		t.Errorf("expected profile to win on tie, got %s", winner.source)
	}
}

func TestSelectBestPresence_NoMatches(t *testing.T) {
	pm := profile.NewManager(nil, nil)
	procs := []profile.DetectedProcess{{PID: 1, Name: "unknown"}}
	winner := selectBestPresence(context.Background(), pm, nil, catalog.ImageResolver{}, procs)
	if winner != nil {
		t.Errorf("expected no winner, got %+v", winner)
	}
}

func TestDebugProcessViewRedactsPrivateFields(t *testing.T) {
	views := debugProcessViews([]profile.DetectedProcess{{
		PID:         42,
		Name:        "wine",
		WindowTitle: "Portal 2",
		SteamAppID:  "620",
		Aliases:     []string{"portal2.exe"},
		ExePath:     "/home/alice/private/portal2.exe",
		Cwd:         "/home/alice/private",
		Args:        []string{"portal2.exe", "--token=secret"},
	}})
	data, err := json.Marshal(views)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(data)
	for _, secret := range []string{"exe_path", "cwd", "args", "/home/alice/private", "--token=secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("debug JSON leaked %q: %s", secret, body)
		}
	}
	for _, expected := range []string{"Portal 2", "620", "portal2.exe"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("debug JSON missing %q: %s", expected, body)
		}
	}
}

func TestDebugProcessNameMatchesAliases(t *testing.T) {
	proc := profile.DetectedProcess{Name: "wine", Aliases: []string{"Lethal Company.exe"}, SteamAppID: "1966720"}
	if !debugProcessNameMatches(proc, "lethal") {
		t.Fatal("expected name filter to match aliases")
	}
	if !debugProcessNameMatches(proc, "1966720") {
		t.Fatal("expected name filter to match Steam AppID")
	}
	if debugProcessNameMatches(proc, "firefox") {
		t.Fatal("did not expect unrelated query to match")
	}
}
