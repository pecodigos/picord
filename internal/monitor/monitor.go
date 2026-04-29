package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pecodigos/picord/internal/profile"
)

type Monitor struct {
	interval time.Duration
	stopCh   chan struct{}
	callback func([]profile.DetectedProcess)
}

func New(intervalSec int, callback func([]profile.DetectedProcess)) *Monitor {
	return &Monitor{
		interval: time.Duration(intervalSec) * time.Second,
		stopCh:   make(chan struct{}),
		callback: callback,
	}
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
			procs := scan()
			if m.callback != nil {
				m.callback(procs)
			}
		}
	}
}

func (m *Monitor) ScanNow() []profile.DetectedProcess {
	return scan()
}

func scan() []profile.DetectedProcess {
	seen := make(map[int]bool)
	var processes []profile.DetectedProcess

	procDir, err := os.Open("/proc")
	if err != nil {
		return processes
	}
	defer procDir.Close()

	entries, err := procDir.Readdirnames(-1)
	if err != nil {
		return processes
	}

	for _, entry := range entries {
		pid := 0
		if _, err := fmt.Sscanf(entry, "%d", &pid); err != nil {
			continue
		}

		fdDir, err := os.Open(fmt.Sprintf("/proc/%d/fd", pid))
		if err != nil {
			continue
		}

		fdEntries, err := fdDir.Readdirnames(-1)
		fdDir.Close()
		if err != nil {
			continue
		}

		connected := false
		for _, fd := range fdEntries {
			link, err := os.Readlink(filepath.Join("/proc", entry, "fd", fd))
			if err != nil {
				continue
			}
			if strings.Contains(link, "discord-ipc") {
				connected = true
				break
			}
		}

		if !connected {
			continue
		}

		if seen[pid] {
			continue
		}
		seen[pid] = true

		name := readProcName(pid)
		processes = append(processes, profile.DetectedProcess{
			PID:  pid,
			Name: name,
		})
	}

	return processes
}

func readProcName(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}
