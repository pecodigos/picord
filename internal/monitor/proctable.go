package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo holds everything we read from a single /proc/<pid> entry.
type ProcessInfo struct {
	PID      int
	PPID     int
	Pgid     int
	Sid      int
	Name     string
	ExePath  string
	Cwd      string
	Args     []string
	EnvHints map[string]string // allowlisted env only
}

// ProcessTable is a snapshot of all processes from one scan.
type ProcessTable struct {
	ByPID  map[int]*ProcessInfo
	ByPPID map[int][]int // parent PID -> child PIDs
	ByPgid map[int][]int // process group -> member PIDs
	BySid  map[int][]int // session -> member PIDs
	Procs  []*ProcessInfo
}

// BuildProcessTable reads /proc once and returns a fully indexed table.
func BuildProcessTable() *ProcessTable {
	pt := &ProcessTable{
		ByPID:  make(map[int]*ProcessInfo),
		ByPPID: make(map[int][]int),
		ByPgid: make(map[int][]int),
		BySid:  make(map[int][]int),
	}

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return pt
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		info := readProcessInfo(pid)
		if info == nil {
			continue
		}
		pt.Procs = append(pt.Procs, info)
		pt.ByPID[pid] = info
	}

	// Build indexes
	for _, info := range pt.Procs {
		pt.ByPPID[info.PPID] = append(pt.ByPPID[info.PPID], info.PID)
		pt.ByPgid[info.Pgid] = append(pt.ByPgid[info.Pgid], info.PID)
		pt.BySid[info.Sid] = append(pt.BySid[info.Sid], info.PID)
	}

	return pt
}

func readProcessInfo(pid int) *ProcessInfo {
	procPath := filepath.Join(procRoot, fmt.Sprintf("%d", pid))

	// Verify the directory exists and is readable
	if _, err := os.Stat(procPath); err != nil {
		return nil
	}

	info := &ProcessInfo{PID: pid}

	// Read /proc/<pid>/status for PPID, Pgid, Sid
	info.PPID, info.Pgid, info.Sid = readProcStatus(procPath)

	// exe symlink
	if p, err := os.Readlink(filepath.Join(procPath, "exe")); err == nil {
		info.ExePath = p
	}

	// cwd symlink
	if p, err := os.Readlink(filepath.Join(procPath, "cwd")); err == nil {
		info.Cwd = p
	}

	// cmdline
	cmdline, err := os.ReadFile(filepath.Join(procPath, "cmdline"))
	if err == nil && len(cmdline) > 0 {
		info.Args = strings.Split(string(cmdline), "\x00")
		for len(info.Args) > 0 && info.Args[len(info.Args)-1] == "" {
			info.Args = info.Args[:len(info.Args)-1]
		}
	}

	// Name from cmdline first (full path, not truncated)
	if len(info.Args) > 0 && info.Args[0] != "" {
		info.Name = filepath.Base(info.Args[0])
	} else {
		// Fallback to comm
		data, _ := os.ReadFile(filepath.Join(procPath, "comm"))
		info.Name = strings.TrimSpace(string(data))
	}

	// Environment hints with extended Wine/Proton allowlist
	info.EnvHints = readProcEnvHints(procPath)

	return info
}

// readProcStatus extracts PPID, Pgid, and Sid from /proc/<pid>/status.
func readProcStatus(procPath string) (ppid, pgid, sid int) {
	data, err := os.ReadFile(filepath.Join(procPath, "status"))
	if err != nil {
		return 0, 0, 0
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "PPid:") {
			ppid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
		} else if strings.HasPrefix(line, "NSpgid:") {
			// Use the namespace-aware value if available.
			pgid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "NSpgid:")))
		} else if strings.HasPrefix(line, "NSsid:") {
			// Modern kernels expose namespace-aware session id as NSsid.
			sid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "NSsid:")))
		} else if strings.HasPrefix(line, "NSpid:") {
			// ignore, just the PID
		}
	}
	// Re-read for non-namespace values if namespace ones weren't found
	if pgid == 0 {
		for _, line := range lines {
			if strings.HasPrefix(line, "Pgid:") {
				pgid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pgid:")))
			}
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "Sid:") {
			sid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Sid:")))
		}
	}
	return ppid, pgid, sid
}

func readProcEnvHints(procPath string) map[string]string {
	allowed := []string{
		"SteamAppId", "SteamGameId", "SteamAppID", "SteamOverlayGameId",
		"PROTON_COMPAT_DATA_PATH", "STEAM_COMPAT_DATA_PATH", "SteamCompatAppId",
		"WINEPREFIX", "WINELOADER",
		"GIO_LAUNCHED_DESKTOP_FILE",
	}

	result := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(procPath, "environ"))
	if err != nil {
		return result
	}

	allowedSet := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allowedSet[k] = true
	}

	entries := strings.Split(string(data), "\x00")
	for _, entry := range entries {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if allowedSet[parts[0]] {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// Descendants returns all descendant PIDs (recursive children).
func (pt *ProcessTable) Descendants(pid int) []int {
	var result []int
	children := pt.ByPPID[pid]
	for _, child := range children {
		result = append(result, child)
		result = append(result, pt.Descendants(child)...)
	}
	return result
}

// Ancestors returns parent PIDs walking up the tree.
func (pt *ProcessTable) Ancestors(pid int) []int {
	var result []int
	info := pt.ByPID[pid]
	for info != nil && info.PPID != 0 && info.PPID != info.PID {
		result = append(result, info.PPID)
		info = pt.ByPID[info.PPID]
	}
	return result
}

// PgidPeers returns other PIDs in the same process group (excluding self).
func (pt *ProcessTable) PgidPeers(pid int) []int {
	info := pt.ByPID[pid]
	if info == nil || info.Pgid == 0 {
		return nil
	}
	var result []int
	for _, p := range pt.ByPgid[info.Pgid] {
		if p != pid {
			result = append(result, p)
		}
	}
	return result
}

// SidPeers returns other PIDs in the same session (excluding self).
func (pt *ProcessTable) SidPeers(pid int) []int {
	info := pt.ByPID[pid]
	if info == nil || info.Sid == 0 {
		return nil
	}
	var result []int
	for _, p := range pt.BySid[info.Sid] {
		if p != pid {
			result = append(result, p)
		}
	}
	return result
}
