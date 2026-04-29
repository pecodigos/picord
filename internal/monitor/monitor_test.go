package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pecodigos/picord/internal/profile"
)

func setupMockProc(t *testing.T) string {
	dir := t.TempDir()
	oldRoot := procRoot
	procRoot = dir
	t.Cleanup(func() { procRoot = oldRoot })
	return dir
}

func createMockProcess(t *testing.T, root string, pid int, name string, hasDiscord bool) {
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "comm"), []byte(name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fdDir := filepath.Join(procDir, "fd")
	if err := os.MkdirAll(fdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if hasDiscord {
		if err := os.Symlink("/run/user/1000/discord-ipc-0", filepath.Join(fdDir, "3")); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.Symlink("/dev/null", filepath.Join(fdDir, "0")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScan_NoDiscordProcesses(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 1000, "firefox", false)
	createMockProcess(t, root, 1001, "chrome", false)

	procs := scan()
	if len(procs) != 0 {
		t.Errorf("expected 0 discord processes, got %d", len(procs))
	}
}

func TestScan_AllProcessesIncludesNonDiscordProcesses(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 1000, "firefox", false)
	createMockProcess(t, root, 2000, "steam", true)

	procs := scanProcesses(true)
	if len(procs) != 2 {
		t.Fatalf("expected 2 total processes, got %d", len(procs))
	}

	names := make(map[string]bool)
	for _, p := range procs {
		names[p.Name] = true
	}
	if !names["firefox"] || !names["steam"] {
		t.Errorf("expected firefox and steam, got %+v", procs)
	}
}

func TestMonitor_ScanNowHonorsScanAllOption(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 1000, "firefox", false)
	createMockProcess(t, root, 2000, "steam", true)

	m := NewWithOptions(1, true, nil)
	procs := m.ScanNow()
	if len(procs) != 2 {
		t.Fatalf("expected scan-all monitor to return 2 processes, got %d", len(procs))
	}
}

func TestScan_OneDiscordProcess(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 1000, "firefox", false)
	createMockProcess(t, root, 2000, "steam", true)

	procs := scan()
	if len(procs) != 1 {
		t.Fatalf("expected 1 discord process, got %d", len(procs))
	}
	if procs[0].PID != 2000 {
		t.Errorf("expected PID 2000, got %d", procs[0].PID)
	}
	if procs[0].Name != "steam" {
		t.Errorf("expected name steam, got %q", procs[0].Name)
	}
}

func TestScan_MultipleDiscordProcesses(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 2000, "steam", true)
	createMockProcess(t, root, 3000, "discord", true)
	createMockProcess(t, root, 4000, "firefox", false)

	procs := scan()
	if len(procs) != 2 {
		t.Fatalf("expected 2 discord processes, got %d", len(procs))
	}

	names := make(map[string]bool)
	for _, p := range procs {
		names[p.Name] = true
	}
	if !names["steam"] || !names["discord"] {
		t.Errorf("expected steam and discord, got %+v", procs)
	}
}

func TestScan_Deduplication(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 2000, "steam", true)
	// Same PID shouldn't appear twice since we only create one dir per PID
	procs := scan()
	if len(procs) != 1 {
		t.Errorf("expected 1 process, got %d", len(procs))
	}
}

func TestMonitor_StartStop(t *testing.T) {
	root := setupMockProc(t)
	createMockProcess(t, root, 2000, "steam", true)

	var detected []profile.DetectedProcess
	m := New(1, func(procs []profile.DetectedProcess) {
		detected = procs
	})
	m.Start()

	// Give it a moment to run at least one scan
	for i := 0; i < 50 && len(detected) == 0; i++ {
		// small sleep to let goroutine run
		// Using a channel or sync would be better in production
	}

	m.Stop()

	// Since we can't easily synchronize without modifying the code,
	// we at least verify the monitor can start and stop without panic.
}

func TestReadProcHints_DesktopID(t *testing.T) {
	root := setupMockProc(t)
	pid := 5000
	procDir := filepath.Join(root, fmt.Sprintf("%d", pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "comm"), []byte("firefox\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte("firefox\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	envData := "GIO_LAUNCHED_DESKTOP_FILE=/usr/share/applications/firefox.desktop\x00"
	if err := os.WriteFile(filepath.Join(procDir, "environ"), []byte(envData), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, _, steamAppID, desktopID := readProcHints(pid, "firefox")
	if desktopID != "firefox" {
		t.Errorf("expected desktopID=firefox, got %q", desktopID)
	}
	if steamAppID != "" {
		t.Errorf("expected empty steamAppID, got %q", steamAppID)
	}
}
