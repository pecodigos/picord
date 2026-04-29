package rpc

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type capturedFrame struct {
	Op      uint32
	Payload map[string]any
}

type mockDiscordServer struct {
	listener net.Listener
	frames   chan capturedFrame
	errs     chan error
}

func startMockDiscordServer(t *testing.T) (*mockDiscordServer, string) {
	t.Helper()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "discord-ipc-0")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	server := &mockDiscordServer{
		listener: listener,
		frames:   make(chan capturedFrame, 16),
		errs:     make(chan error, 16),
	}

	go server.acceptLoop()
	t.Cleanup(func() { server.close() })

	return server, socketPath
}

func (s *mockDiscordServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *mockDiscordServer) handleConn(conn net.Conn) {
	defer conn.Close()

	for {
		op, payload, err := readMockFrame(conn)
		if err != nil {
			if err != io.EOF {
				s.errs <- err
			}
			return
		}

		s.frames <- capturedFrame{Op: op, Payload: payload}

		switch op {
		case opHandshake:
			ready := map[string]any{"cmd": "DISPATCH", "evt": "READY", "data": map[string]any{}}
			if err := writeMockFrame(conn, opFrame, ready); err != nil {
				s.errs <- err
				return
			}
		case opFrame:
			resp := map[string]any{
				"cmd":   payload["cmd"],
				"nonce": payload["nonce"],
				"data":  map[string]any{"ok": true},
			}
			if err := writeMockFrame(conn, opFrame, resp); err != nil {
				s.errs <- err
				return
			}
		case opClose:
			return
		}
	}
}

func (s *mockDiscordServer) close() {
	_ = s.listener.Close()
}

func readMockFrame(conn net.Conn) (uint32, map[string]any, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}

	op := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])
	payloadData := make([]byte, length)
	if _, err := io.ReadFull(conn, payloadData); err != nil {
		return 0, nil, err
	}

	var payload map[string]any
	if len(payloadData) > 0 {
		if err := json.Unmarshal(payloadData, &payload); err != nil {
			return 0, nil, err
		}
	}
	return op, payload, nil
}

func writeMockFrame(conn net.Conn, op uint32, payload map[string]any) error {
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], op)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(payloadData)))
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err = conn.Write(payloadData)
	return err
}

func configureSocketDiscovery(t *testing.T, socketPath string) {
	t.Helper()

	t.Setenv("DISCORD_IPC_PATH", socketPath)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Dir(socketPath))
}

func waitForFrame(t *testing.T, server *mockDiscordServer) capturedFrame {
	t.Helper()

	select {
	case frame := <-server.frames:
		return frame
	case err := <-server.errs:
		t.Fatalf("mock server error: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame")
	}
	return capturedFrame{}
}

func TestNewClient_PerformsHandshake(t *testing.T) {
	server, socketPath := startMockDiscordServer(t)
	configureSocketDiscovery(t, socketPath)

	client, err := NewClient("test-app-id")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	frame := waitForFrame(t, server)
	if frame.Op != opHandshake {
		t.Fatalf("expected handshake op %d, got %d", opHandshake, frame.Op)
	}
	if frame.Payload["v"] != "1" {
		t.Errorf("expected protocol version 1, got %#v", frame.Payload["v"])
	}
	if frame.Payload["client_id"] != "test-app-id" {
		t.Errorf("expected client_id test-app-id, got %#v", frame.Payload["client_id"])
	}
}

func TestSetActivity_SendsExpectedFrame(t *testing.T) {
	server, socketPath := startMockDiscordServer(t)
	configureSocketDiscovery(t, socketPath)

	client, err := NewClient("test-app-id")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()
	_ = waitForFrame(t, server) // handshake

	err = client.SetActivity(&RichActivity{
		Details: "Playing Tests",
		State:   "In mock server",
		Assets:  &RichAssets{LargeImage: "test_art", LargeText: "Test Art"},
		Buttons: []RichButton{{Label: "Open", URL: "https://example.com"}},
	})
	if err != nil {
		t.Fatalf("SetActivity failed: %v", err)
	}

	frame := waitForFrame(t, server)
	if frame.Op != opFrame {
		t.Fatalf("expected frame op %d, got %d", opFrame, frame.Op)
	}
	if frame.Payload["cmd"] != "SET_ACTIVITY" {
		t.Fatalf("expected SET_ACTIVITY command, got %#v", frame.Payload["cmd"])
	}
	if frame.Payload["nonce"] == "" {
		t.Fatal("expected nonce to be populated")
	}

	args, ok := frame.Payload["args"].(map[string]any)
	if !ok {
		t.Fatalf("expected args object, got %#v", frame.Payload["args"])
	}
	if int(args["pid"].(float64)) != os.Getpid() {
		t.Errorf("expected pid %d, got %#v", os.Getpid(), args["pid"])
	}

	activity, ok := args["activity"].(map[string]any)
	if !ok {
		t.Fatalf("expected activity object, got %#v", args["activity"])
	}
	if activity["details"] != "Playing Tests" {
		t.Errorf("expected details to be sent, got %#v", activity["details"])
	}
	if activity["state"] != "In mock server" {
		t.Errorf("expected state to be sent, got %#v", activity["state"])
	}

	assets, ok := activity["assets"].(map[string]any)
	if !ok {
		t.Fatalf("expected assets object, got %#v", activity["assets"])
	}
	if assets["large_image"] != "test_art" {
		t.Errorf("expected large image key, got %#v", assets["large_image"])
	}

	buttons, ok := activity["buttons"].([]any)
	if !ok || len(buttons) != 1 {
		t.Fatalf("expected one button, got %#v", activity["buttons"])
	}
	button := buttons[0].(map[string]any)
	if button["label"] != "Open" || button["url"] != "https://example.com" {
		t.Errorf("unexpected button payload: %#v", button)
	}
}

func TestReconnect_DialsAgainAndHandshakes(t *testing.T) {
	server, socketPath := startMockDiscordServer(t)
	configureSocketDiscovery(t, socketPath)

	client, err := NewClient("test-app-id")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()
	_ = waitForFrame(t, server) // first handshake

	if err := client.Reconnect(); err != nil {
		t.Fatalf("Reconnect failed: %v", err)
	}

	frame := waitForFrame(t, server)
	if frame.Op != opHandshake {
		t.Fatalf("expected reconnect handshake op %d, got %d", opHandshake, frame.Op)
	}
	if frame.Payload["client_id"] != "test-app-id" {
		t.Errorf("expected reconnect client_id test-app-id, got %#v", frame.Payload["client_id"])
	}
	if !client.IsConnected() {
		t.Fatal("expected client to report connected after reconnect")
	}
}

func TestClose_SendsCloseFrameAndMarksDisconnected(t *testing.T) {
	server, socketPath := startMockDiscordServer(t)
	configureSocketDiscovery(t, socketPath)

	client, err := NewClient("test-app-id")
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	_ = waitForFrame(t, server) // handshake

	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	frame := waitForFrame(t, server)
	if frame.Op != opClose {
		t.Fatalf("expected close op %d, got %d", opClose, frame.Op)
	}
	if client.IsConnected() {
		t.Fatal("expected client to report disconnected after close")
	}
}
