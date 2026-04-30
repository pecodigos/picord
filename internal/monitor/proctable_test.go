package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func createMockProcDir(t *testing.T, root string, pid int, name string, statusExtra string) string {
	t.Helper()
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "comm"), []byte(name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	status := "Name:\t" + name + "\n"
	status += "PPid:\t0\n"
	status += "Pgid:\t" + fmt.Sprintf("%d", pid) + "\n"
	status += "Sid:\t" + fmt.Sprintf("%d", pid) + "\n"
	if statusExtra != "" {
		status += statusExtra
	}
	if err := os.WriteFile(filepath.Join(procDir, "status"), []byte(status), 0644); err != nil {
		t.Fatal(err)
	}
	return procDir
}

func TestBuildProcessTable(t *testing.T) {
	root := setupMockProc(t)

	// Parent
	createMockProcDir(t, root, 1000, "steam", "")
	_ = os.WriteFile(filepath.Join(root, "1000", "cmdline"), []byte("steam\x00"), 0644)

	// Child wine process
	createMockProcDir(t, root, 1001, "wine", "PPid:\t1000\nPgid:\t1001\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(root, "1001", "cmdline"), []byte("wine\x00"), 0644)

	// Grandchild game
	createMockProcDir(t, root, 1002, "game", "PPid:\t1001\nPgid:\t1001\nSid:\t1000\n")
	_ = os.WriteFile(filepath.Join(root, "1002", "cmdline"), []byte("game\x00"), 0644)

	pt := BuildProcessTable()
	if len(pt.Procs) != 3 {
		t.Fatalf("expected 3 procs, got %d", len(pt.Procs))
	}

	// Check parent/child index
	children := pt.ByPPID[1000]
	if len(children) != 1 || children[0] != 1001 {
		t.Errorf("expected child of 1000 to be 1001, got %v", children)
	}

	// Check pgid peers
	peers := pt.PgidPeers(1001)
	if len(peers) != 1 || peers[0] != 1002 {
		t.Errorf("expected pgid peer of 1001 to be 1002, got %v", peers)
	}

	// Check descendants
	desc := pt.Descendants(1000)
	if len(desc) != 2 {
		t.Errorf("expected 2 descendants of 1000, got %v", desc)
	}

	// Check ancestors
	anc := pt.Ancestors(1002)
	if len(anc) != 2 {
		t.Errorf("expected 2 ancestors of 1002, got %v", anc)
	}
}

func TestReadProcStatus(t *testing.T) {
	root := setupMockProc(t)
	pid := 5000
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	_ = os.MkdirAll(procDir, 0755)
	status := "Name:\tsteam\nPPid:\t1\nPgid:\t5000\nSid:\t5000\n"
	_ = os.WriteFile(filepath.Join(procDir, "status"), []byte(status), 0644)

	ppid, pgid, sid := readProcStatus(procDir)
	if ppid != 1 {
		t.Errorf("expected ppid 1, got %d", ppid)
	}
	if pgid != 5000 {
		t.Errorf("expected pgid 5000, got %d", pgid)
	}
	if sid != 5000 {
		t.Errorf("expected sid 5000, got %d", sid)
	}
}

func TestReadProcStatusNamespaceFields(t *testing.T) {
	root := setupMockProc(t)
	pid := 5001
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	_ = os.MkdirAll(procDir, 0755)
	status := "Name:\tsteam\nPid:\t5001\nPPid:\t1\nNSpid:\t5001\nNSpgid:\t5000\nNSsid:\t4000\n"
	_ = os.WriteFile(filepath.Join(procDir, "status"), []byte(status), 0644)

	ppid, pgid, sid := readProcStatus(procDir)
	if ppid != 1 {
		t.Errorf("expected ppid 1, got %d", ppid)
	}
	if pgid != 5000 {
		t.Errorf("expected pgid from NSpgid 5000, got %d", pgid)
	}
	if sid != 4000 {
		t.Errorf("expected sid from NSsid 4000, got %d", sid)
	}
}

func TestProcessTableSidPeersIgnoreUnknownSession(t *testing.T) {
	pt := &ProcessTable{
		ByPID: map[int]*ProcessInfo{
			1000: {PID: 1000, Sid: 0},
			1001: {PID: 1001, Sid: 0},
		},
		BySid: map[int][]int{0: {1000, 1001}},
	}

	if peers := pt.SidPeers(1000); len(peers) != 0 {
		t.Fatalf("expected no session peers for unknown sid 0, got %v", peers)
	}
}

func TestProcessTablePgidPeersIgnoreUnknownGroup(t *testing.T) {
	pt := &ProcessTable{
		ByPID: map[int]*ProcessInfo{
			1000: {PID: 1000, Pgid: 0},
			1001: {PID: 1001, Pgid: 0},
		},
		ByPgid: map[int][]int{0: {1000, 1001}},
	}

	if peers := pt.PgidPeers(1000); len(peers) != 0 {
		t.Fatalf("expected no process-group peers for unknown pgid 0, got %v", peers)
	}
}

func TestReadProcStatusMultiValueNamespace(t *testing.T) {
	root := setupMockProc(t)
	pid := 5002
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	_ = os.MkdirAll(procDir, 0755)
	// Real nested namespace format is ordered as for NStgid: the procfs-visible
	// namespace is leftmost, followed by successively nested inner namespaces.
	status := "Name:\tgame\nPid:\t5002\nPPid:\t1\nNSpid:\t5002\t7\nNSpgid:\t5000\t7\nNSsid:\t4000\t7\n"
	_ = os.WriteFile(filepath.Join(procDir, "status"), []byte(status), 0644)

	_, pgid, sid := readProcStatus(procDir)
	if pgid != 5000 {
		t.Errorf("expected pgid=5000 (procfs-visible namespace), got %d", pgid)
	}
	if sid != 4000 {
		t.Errorf("expected sid=4000 (procfs-visible namespace), got %d", sid)
	}
}

func TestReadProcStatusNamespaceNotOverwritten(t *testing.T) {
	root := setupMockProc(t)
	pid := 5003
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	_ = os.MkdirAll(procDir, 0755)
	// Both NSpgid and Pgid present — NSpgid should win
	status := "Name:\tgame\nPPid:\t1\nNSpgid:\t9999\nPgid:\t1111\nNSsid:\t8888\nSid:\t2222\n"
	_ = os.WriteFile(filepath.Join(procDir, "status"), []byte(status), 0644)

	_, pgid, sid := readProcStatus(procDir)
	if pgid != 9999 {
		t.Errorf("expected NSpgid=9999 to win over Pgid=1111, got %d", pgid)
	}
	if sid != 8888 {
		t.Errorf("expected NSsid=8888 to win over Sid=2222, got %d", sid)
	}
}

func TestReadProcStatFallback(t *testing.T) {
	root := setupMockProc(t)
	pid := 5004
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	_ = os.MkdirAll(procDir, 0755)

	// No status file, but provide a stat file
	// Format: pid (comm) state ppid pgrp session tty_nr tpgid flags ...
	stat := "5004 (game) S 1 5004 5004 0 -1 4194560 123 0 0 0 0 0 0 0 20 0 1 0 12345 12345678 256 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	_ = os.WriteFile(filepath.Join(procDir, "stat"), []byte(stat), 0644)

	ppid, pgid, sid := readProcStatus(procDir)
	if ppid != 1 {
		t.Errorf("expected ppid=1 from stat fallback, got %d", ppid)
	}
	if pgid != 5004 {
		t.Errorf("expected pgid=5004 from stat fallback, got %d", pgid)
	}
	if sid != 5004 {
		t.Errorf("expected sid=5004 from stat fallback, got %d", sid)
	}
}

func TestDescendantsCycleProtection(t *testing.T) {
	pt := &ProcessTable{
		ByPID: map[int]*ProcessInfo{
			1000: {PID: 1000, PPID: 1001},
			1001: {PID: 1001, PPID: 1000},
		},
		ByPPID: map[int][]int{
			1000: {1001},
			1001: {1000},
		},
	}

	desc := pt.Descendants(1000)
	if len(desc) != 1 || desc[0] != 1001 {
		t.Errorf("expected 1 descendant [1001] with cycle protection, got %v", desc)
	}
}

func TestAncestorsCycleProtection(t *testing.T) {
	pt := &ProcessTable{
		ByPID: map[int]*ProcessInfo{
			1000: {PID: 1000, PPID: 1001},
			1001: {PID: 1001, PPID: 1000},
		},
	}

	anc := pt.Ancestors(1000)
	if len(anc) != 1 || anc[0] != 1001 {
		t.Errorf("expected ancestors [1001] with cycle protection, got %v", anc)
	}
}

func TestReadProcEnvHints(t *testing.T) {
	root := setupMockProc(t)
	pid := 5000
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	_ = os.MkdirAll(procDir, 0755)

	env := "SteamAppId=12345\x00PATH=/usr/bin\x00SteamGameId=67890\x00HOME=/home\x00"
	_ = os.WriteFile(filepath.Join(procDir, "environ"), []byte(env), 0644)

	hints := readProcEnvHints(procDir)
	if hints["SteamAppId"] != "12345" {
		t.Errorf("expected SteamAppId=12345, got %q", hints["SteamAppId"])
	}
	if hints["SteamGameId"] != "67890" {
		t.Errorf("expected SteamGameId=67890, got %q", hints["SteamGameId"])
	}
	if _, ok := hints["PATH"]; ok {
		t.Error("PATH should not be in allowlisted hints")
	}
}
