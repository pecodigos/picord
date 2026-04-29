# Picord

Universal Discord Rich Presence manager for Linux.

Runs in the background with a system tray icon, automatically detects games and apps, and sets your Discord Rich Presence. Fixes the broken, missing, or lingering Rich Presence from buggy game implementations.

## Features

- **Auto-Detect** — Scans running processes every 2 seconds, matches against built-in profiles
- **Rich Game Catalog** — Local SQLite database with titles, aliases, and image metadata from Steam, Lutris, and desktop entries
- **Smart Matching** — Detects Steam AppID from `/proc` environment and command line, Lutris slugs, and desktop IDs
- **System Tray** — D-Bus StatusNotifierItem, works on Wayland (Hyprland, Sway, KDE) and X11
- **Web GUI** — Settings dashboard at `http://localhost:17970` for managing profiles and searching the catalog
- **48 Built-in Profiles** — Emulators (RetroArch, Dolphin, PCSX2, RPCS3, Yuzu, etc.), games, editors, media players
- **Custom Profiles** — Add your own via the GUI or YAML config; save catalog results as profiles
- **Manual Override** — Set any custom presence from the tray or GUI
- **Config Auto-Reload** — Edit `~/.config/picord/config.yaml` and it picks up changes instantly
- **Linger Fix** — Clears presence when the app/game closes

## Installation

### From source (go install)

```bash
go install github.com/pecodigos/picord/cmd/picord@latest
```

The binary installs to `~/go/bin/picord` (or `$GOPATH/bin/picord`).

### From binary

Download the latest binary from [Releases](https://github.com/pecodigos/picord/releases) and place it in your `$PATH`:

```bash
chmod +x picord
mv picord ~/.local/bin/
```

### .deb package

```bash
make deb
sudo dpkg -i dist/picord_0.1.0_amd64.deb
```

### AppImage

```bash
make appimage
chmod +x dist/picord-*.AppImage
```

## Quick Start

### 1. Create a Discord Application

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications)
2. Click **New Application** → name it (e.g., "Picord")
3. Go to **OAuth2** → copy the **Client ID**
4. (Optional) Under **Rich Presence → Art Assets**, upload images for your games (the image keys in profiles reference these)

### 2. Configure

Edit `~/.config/picord/config.yaml`:

```yaml
app_id: "YOUR_DISCORD_CLIENT_ID"   # Required - your Discord application ID
poll_interval: 2                     # Seconds between process scans
web_port: 17970                      # Web GUI port
scan_all_processes: true             # Detect ordinary apps/games, not just Discord IPC clients
profiles: []                         # Your custom profiles (optional)
```

### 3. Run

```bash
picord
```

A tray icon appears. Right-click for the menu, left-click opens the settings GUI.

## Autostart

### Systemd (recommended)

```bash
mkdir -p ~/.config/systemd/user/
cp resources/picord.service ~/.config/systemd/user/
systemctl --user enable --now picord
```

### Desktop autostart

```bash
mkdir -p ~/.config/autostart/
cp resources/picord.desktop ~/.config/autostart/
```

Then copy the icon:

```bash
mkdir -p ~/.local/share/icons/hicolor/128x128/apps/
cp icons/picord_128.png ~/.local/share/icons/hicolor/128x128/apps/picord.png
```

## Configuration

### Full config with custom profiles

```yaml
app_id: "123456789012345678"
poll_interval: 2
web_port: 17970
scan_all_processes: true
catalog:
  enabled: true
  auto_refresh: true
  sources: [steam_local, desktop]
  refresh_hours: 24
images:
  mode: generic                # generic | asset_key | external_url
  cache_enabled: true
  max_cache_mb: 512
  generic_asset_key: "picord_game"
profiles:
  - name: "My Custom Game"
    match:
      type: process_name    # process_name | window_title | regex
      value: "mygame"
    activity:
      details: "Playing My Game"
      state: "Level 1"
      large_image: "mygame_art"    # Asset key from Discord Dev Portal
      large_text: "My Game"
      small_image: "controller"
      small_text: "Gaming"
    priority: 10
    enabled: true
```

### Match types

| Type | Description | Example |
|------|-------------|---------|
| `process_name` | Exact process name match (from `/proc/<pid>/cmdline`, falling back to `comm`) | `retroarch` |
| `window_title` | Case-insensitive substring match against the visible window title | `Stardew Valley` |
| `regex` | Go regular expression match against process name | `(?i).*craft.*` |

Set `scan_all_processes: false` to use the narrower legacy mode that only considers processes with open Discord IPC sockets.

### Catalog image modes

Picord distinguishes between **catalog images** (for the UI and metadata) and **Discord Rich Presence images** (what Discord actually displays).

| Mode | Behavior | Safety |
|------|----------|--------|
| `generic` | Always uses `generic_asset_key` (e.g., `picord_game`) | Safest |
| `asset_key` | Uses profile `large_image` or catalog `discord_asset_key`, falls back to generic | Safe if assets are uploaded |
| `external_url` | Uses catalog `image_url` directly in the RPC payload | **Unvalidated** — may not work with Discord |

**Important:** `external_url` mode has not been validated against a live Discord client. Use `picord debug-rpc-image --app-id <ID> --external-url <URL>` to test before enabling it.

### Catalog CLI

```bash
# Check catalog status
picord catalog status

# Search the local catalog
picord catalog search "Hollow Knight"

# Refresh local Steam metadata
picord catalog refresh --source steam_local

# Refresh desktop entries
picord catalog refresh --source desktop

# Refresh Lutris public API (opt-in, large download)
picord catalog refresh --source lutris_public --max-pages 3

# Save a catalog entry as a custom profile
picord profile from-catalog <entry-id>
```

## Built-in Profiles

48 profiles ship embedded in the binary covering:

**Emulators**: RetroArch, Dolphin, PCSX2, RPCS3, DuckStation, PPSSPP, Yuzu, Citra, Cemu, MAME, Xemu, Flycast

**Launchers**: Steam, Lutris, Heroic, Prism Launcher, MultiMC

**Source Ports**: GZDoom, OpenMW, OpenRA, OpenTTD, OpenRCT2

**Games**: Factorio, Dwarf Fortress, Stardew Valley, Terraria, Celeste, Hollow Knight, Dead Cells, Minecraft, RuneLite, osu!

**Editors**: VSCode, VSCodium, IntelliJ IDEA, PyCharm, GoLand, Blender, Krita, Godot, Unity

**Media**: Spotify, VLC, mpv, Firefox, Chromium, OBS Studio, Discord, Wine, Proton

## Storage Paths

| Data | Default Path |
|------|-------------|
| Config | `~/.config/picord/config.yaml` |
| Catalog database | `~/.local/share/picord/catalog.db` |
| Image cache | `~/.cache/picord/images` |
| Logs | `~/.local/state/picord/picord.log` |

## Privacy

- Picord reads only an **allowlisted subset** of `/proc/<pid>/environ` (SteamAppId, SteamGameId, etc.). Full environment is never exposed to the API or logs.
- Public catalog sources (e.g., Lutris API) are **opt-in** and rate-limited.
- No Discord account tokens or unofficial APIs are used.

## How It Works

1. **Scans** running processes under `/proc` (default) or only Discord IPC-connected processes when `scan_all_processes: false`
2. **Extracts** game identity hints: Steam AppID, Lutris slug, desktop ID, executable path, window title
3. **Matches** in order: user profiles → catalog by Steam AppID → catalog by Lutris slug → catalog by desktop ID → catalog by executable → catalog by title
4. **Sets** Rich Presence via Discord's local IPC socket (`$XDG_RUNTIME_DIR/discord-ipc-0`)
5. **Clears** presence when the matched process terminates

Picord connects to Discord's existing Rich Presence socket — it works alongside Discord, not instead of it.

## Building

```bash
git clone https://github.com/pecodigos/picord.git
cd picord
make build
```

Requires Go 1.21+.

## Contributing

To add new built-in profiles, edit `internal/profile/defaults.yaml` and open a PR. See existing entries for format reference.

## License

MIT
