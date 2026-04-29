package rpc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
)

func DiscoverSocket() (string, error) {
	// 1. Explicit env override takes highest priority.
	if p := os.Getenv("DISCORD_IPC_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Build candidate directories.
	var dirs []string
	if rd := os.Getenv("XDG_RUNTIME_DIR"); rd != "" {
		dirs = append(dirs, rd)
	}
	uid := os.Getuid()
	dirs = append(dirs, fmt.Sprintf("/run/user/%d", uid))
	dirs = append(dirs, os.TempDir())

	// 2. Standard indexed sockets in each directory.
	for _, dir := range dirs {
		for i := 0; i < 10; i++ {
			candidate := filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i))
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	// 3. Flatpak-specific paths.
	if rd := os.Getenv("XDG_RUNTIME_DIR"); rd != "" {
		for i := 0; i < 10; i++ {
			candidate := filepath.Join(rd, "app", "com.discordapp.Discord", fmt.Sprintf("discord-ipc-%d", i))
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	return "", errors.New("no Discord IPC socket found; is Discord running?")
}

type Client struct {
	conn   net.Conn
	appID  string
	mu     sync.Mutex
	closed bool
}

type handshakePayload struct {
	V        string `json:"v"`
	ClientID string `json:"client_id"`
}

type handshakeResponse struct {
	Cmd  string         `json:"cmd"`
	Data map[string]any `json:"data"`
	Evt  *string        `json:"evt"`
}

type rpcFrame struct {
	Nonce string         `json:"nonce"`
	Cmd   string         `json:"cmd"`
	Args  map[string]any `json:"args"`
	Evt   *string        `json:"evt"`
	Data  map[string]any `json:"data"`
}

func NewClient(appID string) (*Client, error) {
	socket, err := DiscoverSocket()
	if err != nil {
		return nil, fmt.Errorf("socket discovery: %w", err)
	}

	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial socket %s: %w", socket, err)
	}

	c := &Client{
		conn:  conn,
		appID: appID,
	}

	if err := c.handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	return c, nil
}

func (c *Client) handshake() error {
	payload := handshakePayload{V: "1", ClientID: c.appID}
	data, _ := json.Marshal(payload)
	if err := c.writeFrame(opHandshake, data); err != nil {
		return err
	}

	frame, err := c.readFrame()
	if err != nil {
		return err
	}

	var resp handshakeResponse
	if err := json.Unmarshal(frame, &resp); err != nil {
		return fmt.Errorf("parse handshake response: %w", err)
	}

	if resp.Cmd == "DISPATCH" && resp.Evt != nil && *resp.Evt == "READY" {
		return nil
	}

	return fmt.Errorf("unexpected handshake response: cmd=%s evt=%v", resp.Cmd, resp.Evt)
}

func (c *Client) writeFrame(op int32, data []byte) error {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], uint32(op))
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(data)))

	if _, err := c.conn.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := c.conn.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	return nil
}

func (c *Client) readFrame() ([]byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	op := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])

	if op == opClose {
		return nil, errors.New("received close frame from Discord")
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	return payload, nil
}

func (c *Client) sendCommand(cmd string, args map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, errors.New("client is closed")
	}

	frame := rpcFrame{
		Cmd:   cmd,
		Args:  args,
		Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("marshal command: %w", err)
	}

	if err := c.writeFrame(opFrame, data); err != nil {
		return nil, fmt.Errorf("send command: %w", err)
	}

	respData, err := c.readFrame()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp rpcFrame
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return resp.Data, nil
}

type RichActivity struct {
	Details    string       `json:"details,omitempty"`
	State      string       `json:"state,omitempty"`
	Assets     *RichAssets  `json:"assets,omitempty"`
	Timestamps *RichTimes   `json:"timestamps,omitempty"`
	Buttons    []RichButton `json:"buttons,omitempty"`
	Party      *RichParty   `json:"party,omitempty"`
	Instance   bool         `json:"instance,omitempty"`
}

type RichAssets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type RichTimes struct {
	Start *int64 `json:"start,omitempty"`
	End   *int64 `json:"end,omitempty"`
}

type RichButton struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type RichParty struct {
	ID   string  `json:"id,omitempty"`
	Size *[2]int `json:"size,omitempty"`
}

func (c *Client) SetActivity(activity *RichActivity) error {
	args := map[string]any{
		"pid": os.Getpid(),
	}
	if activity != nil {
		args["activity"] = activity
	}
	_, err := c.sendCommand("SET_ACTIVITY", args)
	return err
}

func (c *Client) ClearActivity() error {
	return c.SetActivity(nil)
}

func (c *Client) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	socket, err := DiscoverSocket()
	if err != nil {
		return fmt.Errorf("socket discovery: %w", err)
	}

	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial socket %s: %w", socket, err)
	}

	c.conn = conn
	c.closed = false
	return c.handshake()
}

func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil && !c.closed
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true

	if c.conn != nil {
		data, _ := json.Marshal(map[string]string{})
		_ = c.writeFrame(opClose, data)
		return c.conn.Close()
	}
	return nil
}
