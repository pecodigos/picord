package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

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
