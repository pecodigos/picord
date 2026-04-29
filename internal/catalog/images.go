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
		if r.ExternalEnabled && entry.ImageURL != "" {
			return entry.ImageURL
		}
		if profileActivityLargeImage != "" {
			return profileActivityLargeImage
		}
		if entry.DiscordAssetKey != "" {
			return entry.DiscordAssetKey
		}
		return r.GenericAssetKey
	case ImageModeGeneric:
		return r.GenericAssetKey
	default:
		return r.GenericAssetKey
	}
}
