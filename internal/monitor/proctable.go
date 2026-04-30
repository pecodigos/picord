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
	PID        int
	PPID       int
	Pgid       int
	Sid        int
	Name       string
	ExePath    string
	Cwd        string
	Args       []string
	EnvHints   map[string]string // allowlisted env only
	SteamAppID string            // extracted Steam application ID
}

// ProcessTable is a snapshot of all processes from one scan.
type ProcessTable struct {
	ByPID  map[int]*ProcessInfo
	ByPPID map[int][]int // parent PID -> child PIDs
	ByPgid map[int][]int // process group -> member PIDs
	BySid  map[int][]int // session -> member PIDs
	Procs  []*ProcessInfo
}

// BuildProcessTable reads /proc once and returns a fully indexed table
// with expensive data (exe, cwd, cmdline, env) for every process.
func BuildProcessTable() *ProcessTable {
	return buildProcessTableInternal(true)
}

// BuildProcessTableLite reads /proc once and returns a fully indexed table
// with only cheap data (status, comm). Use EnrichProcessTable to add
// expensive data for selected PIDs later.
func BuildProcessTableLite() *ProcessTable {
	return buildProcessTableInternal(false)
}

func buildProcessTableInternal(full bool) *ProcessTable {
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
		var info *ProcessInfo
		if full {
			info = readProcessInfo(pid)
		} else {
			info = readProcessInfoLite(pid)
		}
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

// EnrichProcessTable reads expensive data for the given PIDs and their
// related processes in the table.
func EnrichProcessTable(pt *ProcessTable, pids []int) {
	seen := make(map[int]bool)
	for _, pid := range pids {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if info := pt.ByPID[pid]; info != nil {
			enrichProcessInfo(info)
		}
	}
}

// EnrichProcessEnvOnly reads only the allowlisted environment for the given
// PIDs and populates EnvHints and SteamAppID. This is cheaper than a full
// enrichment and is used for same-PGID peer gating in scanAll=false mode.
func EnrichProcessEnvOnly(pt *ProcessTable, pids []int) {
	for _, pid := range pids {
		if info := pt.ByPID[pid]; info != nil {
			procPath := filepath.Join(procRoot, fmt.Sprintf("%d", pid))
			info.EnvHints = readProcEnvHints(procPath)
			info.SteamAppID = ExtractSteamAppID(nil, info.EnvHints)
		}
	}
}

func readProcessInfo(pid int) *ProcessInfo {
	info := readProcessInfoLite(pid)
	if info == nil {
		return nil
	}
	enrichProcessInfo(info)
	return info
}

// readProcessInfoLite reads only cheap /proc data: status and comm.
func readProcessInfoLite(pid int) *ProcessInfo {
	procPath := filepath.Join(procRoot, fmt.Sprintf("%d", pid))

	if _, err := os.Stat(procPath); err != nil {
		return nil
	}

	info := &ProcessInfo{PID: pid}
	info.PPID, info.Pgid, info.Sid = readProcStatus(procPath)

	data, _ := os.ReadFile(filepath.Join(procPath, "comm"))
	info.Name = strings.TrimSpace(string(data))

	return info
}

// enrichProcessInfo reads expensive /proc data (exe, cwd, cmdline, environ)
// and computes derived fields like SteamAppID.
func enrichProcessInfo(info *ProcessInfo) {
	procPath := filepath.Join(procRoot, fmt.Sprintf("%d", info.PID))

	// exe symlink
	if p, err := os.Readlink(filepath.Join(procPath, "exe")); err == nil {
		// Linux appends " (deleted)" when the executable is updated/replaced.
		info.ExePath = strings.TrimSuffix(p, " (deleted)")
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

	// Prefer name from cmdline over comm
	if len(info.Args) > 0 && info.Args[0] != "" {
		info.Name = basenameAnyOS(stripQuotes(info.Args[0]))
	}

	// Environment hints
	info.EnvHints = readProcEnvHints(procPath)

	// Steam AppID
	info.SteamAppID = ExtractSteamAppID(info.Args, info.EnvHints)
}

// readProcStatus extracts PPID, Pgid, and Sid from /proc/<pid>/status.
// It handles multi-value namespace fields (NSpgid, NSsid) by using the
// procfs-visible namespace value. Linux reports these namespace lists from the
// procfs/root namespace on the left toward nested inner namespaces on the right.
func readProcStatus(procPath string) (ppid, pgid, sid int) {
	data, err := os.ReadFile(filepath.Join(procPath, "status"))
	if err != nil {
		return readProcStatFallback(procPath)
	}
	lines := strings.Split(string(data), "\n")

	var hasNSpgid, hasNSsid bool
	for _, line := range lines {
		if strings.HasPrefix(line, "PPid:") {
			ppid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
		} else if strings.HasPrefix(line, "NSpgid:") {
			// Multi-value format: "NSpgid:\t5000\t7" — use leftmost procfs-visible value.
			pgid = parseFirstIntField(line)
			hasNSpgid = pgid != 0
		} else if strings.HasPrefix(line, "NSsid:") {
			// Multi-value format: "NSsid:\t4000\t7" — use leftmost procfs-visible value.
			sid = parseFirstIntField(line)
			hasNSsid = sid != 0
		}
	}

	// Only fall back to non-namespace values if namespace ones weren't found.
	if !hasNSpgid {
		for _, line := range lines {
			if strings.HasPrefix(line, "Pgid:") {
				pgid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pgid:")))
			}
		}
	}
	if !hasNSsid {
		for _, line := range lines {
			if strings.HasPrefix(line, "Sid:") {
				sid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Sid:")))
			}
		}
	}

	// If we still lack pgid or sid, try /proc/<pid>/stat as a last resort.
	if pgid == 0 || sid == 0 {
		p, g, s := readProcStatFallback(procPath)
		if pgid == 0 {
			pgid = g
		}
		if sid == 0 {
			sid = s
		}
		if ppid == 0 {
			ppid = p
		}
	}

	return ppid, pgid, sid
}

// parseFirstIntField extracts the first integer value after the key from a
// /proc status line, e.g. "NSpgid:\t5000\t7" returns 5000.
func parseFirstIntField(line string) int {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.Atoi(fields[1])
	return v
}

// readProcStatFallback parses pgid and sid from /proc/<pid>/stat as a fallback
// when /proc/<pid>/status is unavailable or incomplete.
// The stat format is: pid (comm) state ppid pgrp session ...
// After grouping the comm field, we need indices 2 (ppid), 3 (pgrp), 4 (session).
func readProcStatFallback(procPath string) (ppid, pgid, sid int) {
	data, err := os.ReadFile(filepath.Join(procPath, "stat"))
	if err != nil {
		return 0, 0, 0
	}
	// The comm field may contain spaces and parentheses, so we need to find
	// the matching ')' first.
	fields := parseStatFields(string(data))
	if len(fields) < 5 {
		return 0, 0, 0
	}
	ppid, _ = strconv.Atoi(fields[2])
	pgid, _ = strconv.Atoi(fields[3])
	sid, _ = strconv.Atoi(fields[4])
	return ppid, pgid, sid
}

// parseStatFields splits a /proc/<pid>/stat line into fields, handling the
// comm field which is wrapped in parentheses and may contain spaces.
func parseStatFields(data string) []string {
	// Find the closing parenthesis of the comm field.
	closeIdx := strings.LastIndex(data, ")")
	if closeIdx < 0 {
		// Malformed; fall back to simple split.
		return strings.Fields(data)
	}
	prefix := data[:closeIdx+1]
	rest := strings.TrimSpace(data[closeIdx+1:])
	result := []string{prefix}
	result = append(result, strings.Fields(rest)...)
	return result
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
// It includes cycle protection to handle malformed process tables.
func (pt *ProcessTable) Descendants(pid int) []int {
	return pt.descendantsRec(pid, make(map[int]bool))
}

func (pt *ProcessTable) descendantsRec(pid int, visited map[int]bool) []int {
	if visited[pid] {
		return nil
	}
	visited[pid] = true

	var result []int
	children := pt.ByPPID[pid]
	for _, child := range children {
		if !visited[child] {
			result = append(result, child)
			result = append(result, pt.descendantsRec(child, visited)...)
		}
	}
	return result
}

// Ancestors returns parent PIDs walking up the tree.
func (pt *ProcessTable) Ancestors(pid int) []int {
	var result []int
	visited := map[int]bool{pid: true}
	info := pt.ByPID[pid]
	for info != nil && info.PPID != 0 && info.PPID != info.PID {
		if visited[info.PPID] {
			break
		}
		visited[info.PPID] = true
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
