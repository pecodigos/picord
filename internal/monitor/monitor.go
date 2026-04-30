package monitor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pecodigos/picord/internal/profile"
)

var procRoot = "/proc"

type Monitor struct {
	interval time.Duration
	scanAll  bool
	debug    bool
	stopCh   chan struct{}
	callback func([]profile.DetectedProcess)
}

func NewWithOptions(intervalSec int, scanAll bool, callback func([]profile.DetectedProcess)) *Monitor {
	return &Monitor{
		interval: time.Duration(intervalSec) * time.Second,
		scanAll:  scanAll,
		stopCh:   make(chan struct{}),
		callback: callback,
	}
}

func New(intervalSec int, callback func([]profile.DetectedProcess)) *Monitor {
	return NewWithOptions(intervalSec, false, callback)
}

func (m *Monitor) SetDebug(enabled bool) {
	m.debug = enabled
}

func (m *Monitor) Start() {
	go m.loop()
}

func (m *Monitor) Stop() {
	close(m.stopCh)
}

func (m *Monitor) loop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			procs := m.ScanNow()
			if m.debug && len(procs) > 0 {
				if m.scanAll {
					log.Printf("[monitor] detected %d process(es)", len(procs))
				} else {
					log.Printf("[monitor] detected %d process(es) with Discord IPC", len(procs))
				}
				for _, p := range procs {
					log.Printf("[monitor]   PID=%d Name=%q WindowTitle=%q", p.PID, p.Name, p.WindowTitle)
				}
			}
			if m.callback != nil {
				m.callback(procs)
			}
		}
	}
}

func (m *Monitor) ScanNow() []profile.DetectedProcess {
	return scanProcesses(m.scanAll)
}

func scanProcesses(scanAll bool) []profile.DetectedProcess {
	if scanAll {
		// Full resolver path: read expensive data for all processes.
		processes := ResolveProcessIdentities()
		return processes
	}

	// Candidate-first path: find Discord IPC processes, then only read
	// expensive env/cmdline/exe/cwd data for those candidates and their
	// related processes (ancestors/descendants for Wine/Proton enrichment).
	ipcPIDs := findDiscordIPCPIDs()
	if len(ipcPIDs) == 0 {
		return nil
	}
	processes := ResolveProcessIdentitiesLite(ipcPIDs)

	// Filter to only IPC candidates in the final output.
	ipcSet := make(map[int]bool, len(ipcPIDs))
	for _, pid := range ipcPIDs {
		ipcSet[pid] = true
	}
	var filtered []profile.DetectedProcess
	for _, p := range processes {
		if ipcSet[p.PID] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// findDiscordIPCPIDs returns PIDs that have an open discord-ipc fd,
// excluding known desktop noise like Discord itself and browsers.
func findDiscordIPCPIDs() []int {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}

	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		// Skip excluded apps (Discord opens discord-ipc itself)
		name := readProcName(pid)
		if isExcludedApp(name) {
			continue
		}
		if processHasDiscordIPC(entry.Name()) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func processHasDiscordIPC(entry string) bool {
	fdDir, err := os.Open(filepath.Join(procRoot, entry, "fd"))
	if err != nil {
		return false
	}

	fdEntries, err := fdDir.Readdirnames(-1)
	fdDir.Close()
	if err != nil {
		return false
	}

	for _, fd := range fdEntries {
		link, err := os.Readlink(filepath.Join(procRoot, entry, "fd", fd))
		if err != nil {
			continue
		}
		if strings.Contains(link, "discord-ipc") {
			return true
		}
	}

	return false
}

func readProcName(pid int) string {
	procPath := filepath.Join(procRoot, fmt.Sprintf("%d", pid))

	// Try cmdline first (full path, not truncated)
	cmdline, err := os.ReadFile(filepath.Join(procPath, "cmdline"))
	if err == nil && len(cmdline) > 0 {
		// cmdline is null-separated; first element is the executable
		parts := strings.Split(string(cmdline), "\x00")
		if len(parts) > 0 && parts[0] != "" {
			base := filepath.Base(parts[0])
			if base != "" {
				return base
			}
		}
	}

	// Fallback to comm (kernel thread name, max 15 chars)
	data, err := os.ReadFile(filepath.Join(procPath, "comm"))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}
