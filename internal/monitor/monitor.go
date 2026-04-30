package monitor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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

func New(intervalSec int, callback func([]profile.DetectedProcess)) *Monitor {
	return NewWithOptions(intervalSec, false, callback)
}

func NewWithOptions(intervalSec int, scanAll bool, callback func([]profile.DetectedProcess)) *Monitor {
	return &Monitor{
		interval: time.Duration(intervalSec) * time.Second,
		scanAll:  scanAll,
		stopCh:   make(chan struct{}),
		callback: callback,
	}
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

func scan() []profile.DetectedProcess {
	return scanProcesses(false)
}

func scanProcesses(scanAll bool) []profile.DetectedProcess {
	// Use the new identity resolver which builds a full /proc table,
	// enriches Wine/Proton carriers with aliases, and returns DetectedProcesses.
	processes := ResolveProcessIdentities()

	if !scanAll {
		// Legacy mode: only include processes with Discord IPC connections
		var filtered []profile.DetectedProcess
		for _, p := range processes {
			if processHasDiscordIPC(fmt.Sprintf("%d", p.PID)) {
				filtered = append(filtered, p)
			}
		}
		return filtered
	}

	return processes
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
