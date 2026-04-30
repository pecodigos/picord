package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DetectCompositor returns the current window compositor type.
func DetectCompositor() string {
	de := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	session := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))

	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		return "hyprland"
	}
	if os.Getenv("SWAYSOCK") != "" || strings.Contains(de, "sway") {
		return "sway"
	}
	if strings.Contains(de, "kde") || os.Getenv("KDE_FULL_SESSION") != "" {
		return "kde"
	}
	if os.Getenv("DISPLAY") != "" && session != "wayland" {
		return "x11"
	}
	if strings.Contains(de, "gnome") && session == "wayland" {
		return "gnome-wayland"
	}
	return "unknown"
}

// GetWindowTitles returns a map of PID -> window title for visible windows.
// It tries multiple backends based on the detected compositor.
func GetWindowTitles() (map[int]string, error) {
	compositor := DetectCompositor()

	switch compositor {
	case "hyprland":
		return getHyprlandWindows()
	case "sway":
		return getSwayWindows()
	case "x11":
		return getX11Windows()
	case "kde":
		return getKDEWindows()
	default:
		// Try all backends as fallback
		if titles, err := getHyprlandWindows(); err == nil && len(titles) > 0 {
			return titles, nil
		}
		if titles, err := getSwayWindows(); err == nil && len(titles) > 0 {
			return titles, nil
		}
		if titles, err := getX11Windows(); err == nil && len(titles) > 0 {
			return titles, nil
		}
		if titles, err := getKDEWindows(); err == nil && len(titles) > 0 {
			return titles, nil
		}
		return nil, fmt.Errorf("no window title backend available")
	}
}

// --- Hyprland ---

type hyprWindow struct {
	Address string `json:"address"`
	Mapped  bool   `json:"mapped"`
	Hidden  bool   `json:"hidden"`
	Title   string `json:"title"`
	Class   string `json:"class"`
	PID     int    `json:"pid"`
}

func getHyprlandWindows() (map[int]string, error) {
	out, err := exec.Command("hyprctl", "clients", "-j").Output()
	if err != nil {
		return nil, err
	}

	var windows []hyprWindow
	if err := json.Unmarshal(out, &windows); err != nil {
		return nil, err
	}

	titles := make(map[int]string)
	for _, w := range windows {
		if !w.Mapped || w.Hidden || w.PID == 0 {
			continue
		}
		title := w.Title
		if title == "" {
			title = w.Class
		}
		titles[w.PID] = title
	}
	return titles, nil
}

// --- Sway ---

type swayNode struct {
	Type     string     `json:"type"`
	Name     string     `json:"name"`
	PID      int        `json:"pid"`
	AppID    string     `json:"app_id"`
	Window   int        `json:"window"`
	Visible  bool       `json:"visible"`
	Nodes    []swayNode `json:"nodes"`
	Floating []swayNode `json:"floating_nodes"`
}

func getSwayWindows() (map[int]string, error) {
	out, err := exec.Command("swaymsg", "-t", "get_tree").Output()
	if err != nil {
		return nil, err
	}

	var root swayNode
	if err := json.Unmarshal(out, &root); err != nil {
		return nil, err
	}

	titles := make(map[int]string)
	walkSwayTree(&root, titles)
	return titles, nil
}

func walkSwayTree(node *swayNode, out map[int]string) {
	if node.Type == "con" || node.Type == "floating_con" {
		if node.PID > 0 && node.Name != "" {
			out[node.PID] = node.Name
		}
	}
	for i := range node.Nodes {
		walkSwayTree(&node.Nodes[i], out)
	}
	for i := range node.Floating {
		walkSwayTree(&node.Floating[i], out)
	}
}

// --- X11 ---

func getX11Windows() (map[int]string, error) {
	// Prefer wmctrl -l -p for simplicity
	out, err := exec.Command("wmctrl", "-l", "-p").Output()
	if err != nil {
		// Fallback to xdotool + xprop
		return getX11WindowsXdotool()
	}

	titles := make(map[int]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		pid := 0
		if _, err := fmt.Sscanf(parts[2], "%d", &pid); err != nil || pid == 0 {
			continue
		}
		// Title starts after the desktop column (index 3)
		title := strings.Join(parts[3:], " ")
		titles[pid] = title
	}
	return titles, nil
}

func getX11WindowsXdotool() (map[int]string, error) {
	out, err := exec.Command("xdotool", "search", "--onlyvisible", ".*").Output()
	if err != nil {
		return nil, err
	}

	titles := make(map[int]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		wid := strings.TrimSpace(line)
		if wid == "" {
			continue
		}
		titleOut, err := exec.Command("xdotool", "getwindowname", wid).Output()
		if err != nil {
			continue
		}
		pidOut, err := exec.Command("xdotool", "getwindowpid", wid).Output()
		if err != nil {
			continue
		}
		pid := 0
		fmt.Sscanf(string(pidOut), "%d", &pid)
		if pid > 0 {
			titles[pid] = strings.TrimSpace(string(titleOut))
		}
	}
	return titles, nil
}

// --- KDE ---

func getKDEWindows() (map[int]string, error) {
	// Try kdotool first (KDE 6+)
	out, err := exec.Command("kdotool", "search", ".*").Output()
	if err != nil {
		return getKDEDBusWindows()
	}

	titles := make(map[int]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		wid := strings.TrimSpace(line)
		if wid == "" {
			continue
		}
		titleOut, err := exec.Command("kdotool", "getwindowtitle", wid).Output()
		if err != nil {
			continue
		}
		pidOut, err := exec.Command("kdotool", "getwindowpid", wid).Output()
		if err != nil {
			continue
		}
		pid := 0
		fmt.Sscanf(string(pidOut), "%d", &pid)
		if pid > 0 {
			titles[pid] = strings.TrimSpace(string(titleOut))
		}
	}
	return titles, nil
}

func getKDEDBusWindows() (map[int]string, error) {
	// Query KWin's D-Bus interface for window list.
	// Each window is exposed as /org/kde/KWin/Client/<id> with caption and pid properties.
	out, err := exec.Command("qdbus", "org.kde.KWin", "/KWin", "org.kde.KWin.clientList").Output()
	if err != nil {
		return nil, err
	}

	titles := make(map[int]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		winID := line

		titleOut, err := exec.Command("qdbus", "org.kde.KWin", "/org/kde/KWin/Client/"+winID, "org.kde.KWin.Client.caption").Output()
		if err != nil {
			continue
		}
		pidOut, err := exec.Command("qdbus", "org.kde.KWin", "/org/kde/KWin/Client/"+winID, "org.kde.KWin.Client.pid").Output()
		if err != nil {
			continue
		}
		pid := 0
		if _, err := fmt.Sscanf(string(pidOut), "%d", &pid); err != nil || pid == 0 {
			continue
		}
		title := strings.TrimSpace(string(titleOut))
		if title != "" {
			titles[pid] = title
		}
	}

	if len(titles) == 0 {
		return nil, fmt.Errorf("no KWin clients found via dbus")
	}
	return titles, nil
}
