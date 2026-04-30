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
