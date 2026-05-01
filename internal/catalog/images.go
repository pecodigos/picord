package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ImageCacheDir returns the directory for cached images.
func ImageCacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "picord", "images")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, ".cache", "picord", "images")
	}
	return "picord-images"
}

const maxImageBytes = 10 << 20 // 10 MiB

// DownloadImage fetches an image from url, validates it is an image, computes
// SHA256, decodes dimensions, and writes it to cacheDir if not already present.
// It returns the cached file path, SHA256, width, height, MIME type, and any error.
func DownloadImage(client *http.Client, cacheDir, url string) (cachePath, hash string, width, height int, mime string, err error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", 0, 0, "", fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, 0, "", fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", 0, 0, "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !isImageMIME(contentType) {
		return "", "", 0, 0, "", fmt.Errorf("unexpected content-type %q", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return "", "", 0, 0, "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxImageBytes {
		return "", "", 0, 0, "", fmt.Errorf("image exceeds max size %d", maxImageBytes)
	}

	sum := sha256.Sum256(body)
	hash = hex.EncodeToString(sum[:])

	// Decode dimensions from bytes.
	cfg, imgFormat, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return "", "", 0, 0, "", fmt.Errorf("decode image config: %w", err)
	}
	width = cfg.Width
	height = cfg.Height
	if imgFormat == "jpeg" {
		mime = "image/jpeg"
	} else if imgFormat == "png" {
		mime = "image/png"
	} else {
		mime = contentType
	}

	// Write to cache if not already present.
	if cacheDir != "" {
		_ = os.MkdirAll(cacheDir, 0755)
		cachePath = filepath.Join(cacheDir, hash[:2], hash[2:])
		if _, statErr := os.Stat(cachePath); os.IsNotExist(statErr) {
			_ = os.MkdirAll(filepath.Dir(cachePath), 0755)
			if writeErr := os.WriteFile(cachePath, body, 0644); writeErr != nil {
				return "", "", 0, 0, "", fmt.Errorf("write cache: %w", writeErr)
			}
		}
	}

	return cachePath, hash, width, height, mime, nil
}

func isImageMIME(ct string) bool {
	return ct == "image/jpeg" || ct == "image/png" || ct == "image/webp" || ct == "image/gif"
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isLocalIconURL(s string) bool {
	return strings.HasPrefix(s, "localicon:")
}

type ImageMode string

const (
	ImageModeAssetKey    ImageMode = "asset_key"
	ImageModeExternalURL ImageMode = "external_url"
	ImageModeGeneric     ImageMode = "generic"
)

// ImageResolver resolves the Discord Rich Presence image reference for a catalog entry.
type ImageResolver struct {
	Mode            ImageMode
	GenericAssetKey string
	ExternalEnabled bool // set only after live validation
	LocalAssetBase  string // base URL for serving local icons (e.g. "http://127.0.0.1:17970")
}

// Resolve returns the image reference to send to Discord.
func (r *ImageResolver) Resolve(entry Entry, profileActivityLargeImage string) string {
	switch r.Mode {
	case ImageModeAssetKey:
		if profileActivityLargeImage != "" {
			return profileActivityLargeImage
		}
		if entry.DiscordAssetKey != "" {
			return entry.DiscordAssetKey
		}
		return r.GenericAssetKey
	case ImageModeExternalURL:
		if r.ExternalEnabled && entry.ImageURL != "" && isHTTPURL(entry.ImageURL) {
			return entry.ImageURL
		}
		if r.ExternalEnabled {
			if pub := publicIconURL(entry.Title); pub != "" {
				return pub
			}
		}
		if r.ExternalEnabled && entry.ImageURL != "" && isLocalIconURL(entry.ImageURL) {
			return r.localIconHTTPURL(entry.ImageURL, entry.Title)
		}
		if profileActivityLargeImage != "" {
			return profileActivityLargeImage
		}
		if entry.DiscordAssetKey != "" {
			return entry.DiscordAssetKey
		}
		return r.GenericAssetKey
	case ImageModeGeneric:
		if entry.ImageURL != "" && isHTTPURL(entry.ImageURL) {
			return entry.ImageURL
		}
		if entry.ImageURL != "" && isLocalIconURL(entry.ImageURL) {
			return r.localIconHTTPURL(entry.ImageURL, entry.Title)
		}
		return r.GenericAssetKey
	default:
		return r.GenericAssetKey
	}
}

func (r *ImageResolver) localIconHTTPURL(localiconURL string, entryTitle string) string {
	// Try icon.horse CDN — returns PNG, HTTPS, works with Discord.
	if url := appIconURL(entryTitle); url != "" {
		return url
	}
	return r.GenericAssetKey
}

// appIconURL returns a public HTTPS PNG icon URL for the given app title.
// Uses icon.horse to fetch the website favicon/logo for known domains.
func appIconURL(title string) string {
	domain := appDomain(strings.ToLower(title))
	if domain == "" {
		return ""
	}
	return "https://icon.horse/icon/" + domain
}

var appDomainMap = map[string]string{
	"gimp":                              "gimp.org",
	"gnu image manipulation program":    "gimp.org",
	"blender":                           "blender.org",
	"obs studio":                        "obsproject.com",
	"krita":                             "krita.org",
	"inkscape":                          "inkscape.org",
	"audacity":                          "audacityteam.org",
	"kdenlive":                          "kdenlive.org",
	"davinci resolve":                   "blackmagicdesign.com",
	"visual studio code":                "code.visualstudio.com",
	"code - oss":                        "code.visualstudio.com",
	"vscodium":                          "vscodium.com",
	"intellij idea":                     "jetbrains.com",
	"pycharm":                           "jetbrains.com",
	"goland":                            "jetbrains.com",
	"godot engine":                      "godotengine.org",
	"godot":                             "godotengine.org",
	"unity":                             "unity.com",
	"unity hub":                         "unity.com",
	"unreal editor":                     "unrealengine.com",
	"libreoffice writer":                "libreoffice.org",
	"libreoffice calc":                  "libreoffice.org",
	"libreoffice impress":               "libreoffice.org",
	"libreoffice":                       "libreoffice.org",
	"freecad":                           "freecad.org",
	"vlc":                               "videolan.org",
	"vlc media player":                  "videolan.org",
	"mpv":                               "mpv.io",
	"handbrake":                         "handbrake.fr",
	"thunderbird":                       "thunderbird.net",
	"vscode":                            "code.visualstudio.com",
	"ardour":                            "ardour.org",
	"darktable":                         "darktable.org",
	"digikam":                           "digikam.org",
	"openshot":                          "openshot.org",
	"shotcut":                           "shotcut.org",
	"calibre":                           "calibre-ebook.com",
	"musescore":                         "musescore.org",
	"scribus":                           "scribus.net",
	"kicad":                             "kicad.org",
	"arduino":                           "arduino.cc",
	"processing":                        "processing.org",
	"qgis":                              "qgis.org",
	"hexchat":                           "hexchat.github.io",
	"pidgin":                            "pidgin.im",
	"signal":                            "signal.org",
	"telegram":                          "telegram.org",
	"element":                           "element.io",
	"discord":                           "discord.com",
	"spotify":                           "spotify.com",
	"slack":                             "slack.com",
	"zoom":                              "zoom.us",
	"1password":                         "1password.com",
	"bitwarden":                         "bitwarden.com",
	"nextcloud":                         "nextcloud.com",
	"firefox":                           "mozilla.org",
	"chromium":                          "chromium.org",
	"gnome builder":                     "apps.gnome.org",
	"gnome boxes":                       "apps.gnome.org",
}

func appDomain(lowerTitle string) string {
	if d, ok := appDomainMap[lowerTitle]; ok {
		return d
	}
	// Try single-word titles as .org domains.
	if !strings.Contains(lowerTitle, " ") && !strings.Contains(lowerTitle, ".") {
		return lowerTitle + ".org"
	}
	return ""
}

// publicIconURL returns a public HTTPS icon URL for well-known applications.
// Returns "" if no icon is available.
func publicIconURL(title string) string {
	lower := strings.ToLower(title)

	if url, ok := customIconURLs[lower]; ok {
		return url
	}

	name, ok := simpleIconsMap[lower]
	if !ok {
		return ""
	}
	// Use wsrv.nl proxy to convert simpleicons SVG to PNG, since Discord RPC ignores SVG.
	return "https://wsrv.nl/?url=https://cdn.simpleicons.org/" + name + "&output=png"
}

var simpleIconsMap = map[string]string{
	"gimp":                           "gimp",
	"gnu image manipulation program": "gimp",
	"blender":                        "blender",
	"obs studio":                     "obsstudio",
	"krita":                          "krita",
	"inkscape":                       "inkscape",
	"audacity":                       "audacity",
	"kdenlive":                       "kdenlive",
	"davinci resolve":                "davinciresolve",
	"visual studio code":             "visualstudiocode",
	"code - oss":                     "visualstudiocode",
	"vscodium":                       "vscodium",
	"intellij idea":                  "intellijidea",
	"pycharm":                        "pycharm",
	"goland":                         "goland",
	"godot engine":                   "godotengine",
	"godot":                          "godotengine",
	"unity":                          "unity",
	"unity hub":                      "unity",
	"unreal editor":                  "unrealengine",
	"libreoffice writer":             "libreofficewriter",
	"libreoffice calc":               "libreofficecalc",
	"libreoffice impress":            "libreofficeimpress",
	"freecad":                        "freecad",
	"vlc":                            "vlc",
	"vlc media player":               "vlc",
	"mpv":                            "mpv",
	"handbrake":                      "handbrake",
	"thunderbird":                    "thunderbird",
	"vscode":                         "visualstudiocode",
}

// customIconURLs provides HTTPS icon URLs for apps not on the Simple Icons CDN.
var customIconURLs = map[string]string{
	"duckstation":  "https://raw.githubusercontent.com/stenzek/duckstation/master/data/resources/images/duck.png",
	"pcsx2":        "https://raw.githubusercontent.com/PCSX2/pcsx2/master/bin/Resources/icons/AppIcon.png",
	"rpcs3":        "https://raw.githubusercontent.com/RPCS3/rpcs3/master/rpcs3/rpcs3qt/resources/rpcs3.ico",
	"dolphin":      "https://raw.githubusercontent.com/dolphin-emu/dolphin/master/Data/dolphin.png",
	"retroarch":    "https://raw.githubusercontent.com/libretro/RetroArch/master/pkg/msvc-uwp/RetroArch-uwp/Assets/Logo.png",
	"cemu":         "https://raw.githubusercontent.com/cemu-project/Cemu/main/dist/linux/cemu.svg",
	"ppsspp":       "https://raw.githubusercontent.com/hrydgard/ppsspp/master/assets/icon_regular_72.png",
	"yuzu":         "https://raw.githubusercontent.com/yuzu-emu/yuzu/main/dist/72-yuzu.png",
	"citra":        "https://raw.githubusercontent.com/citra-emu/citra/master/dist/citra.png",
	"xemu":         "https://raw.githubusercontent.com/xemu-project/xemu/master/ui/data/xemu.png",
}
