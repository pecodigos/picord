package catalog

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func makeTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadImage_AcceptsPNG(t *testing.T) {
	imgData := makeTestPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(imgData)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	cachePath, hash, width, height, mime, err := DownloadImage(nil, cacheDir, server.URL)
	if err != nil {
		t.Fatalf("DownloadImage failed: %v", err)
	}
	if cachePath == "" {
		t.Fatal("expected cache path")
	}
	if hash == "" {
		t.Fatal("expected hash")
	}
	if mime != "image/png" {
		t.Errorf("mime=%q, want image/png", mime)
	}
	if width != 2 || height != 3 {
		t.Errorf("dimensions=%dx%d, want 2x3", width, height)
	}

	// Verify file exists on disk.
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("cached file missing: %v", err)
	}

	// Second download should succeed and point to same path.
	cachePath2, hash2, _, _, _, err := DownloadImage(nil, cacheDir, server.URL)
	if err != nil {
		t.Fatalf("second DownloadImage failed: %v", err)
	}
	if hash2 != hash {
		t.Error("expected same hash")
	}
	if cachePath2 != cachePath {
		t.Error("expected same cache path")
	}
}

func TestDownloadImage_RejectsHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	_, _, _, _, _, err := DownloadImage(nil, t.TempDir(), server.URL)
	if err == nil {
		t.Fatal("expected error for HTML response")
	}
}

func TestDownloadImage_RejectsText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("not an image"))
	}))
	defer server.Close()

	_, _, _, _, _, err := DownloadImage(nil, t.TempDir(), server.URL)
	if err == nil {
		t.Fatal("expected error for text response")
	}
}

func TestImageResolver(t *testing.T) {
	tests := []struct {
		name       string
		mode       ImageMode
		genericKey string
		externalOn bool
		entry      Entry
		profileImg string
		want       string
	}{
		{
			name:       "asset_key uses profile image",
			mode:       ImageModeAssetKey,
			genericKey: "picord",
			entry:      Entry{DiscordAssetKey: "steam_620"},
			profileImg: "my_custom_art",
			want:       "my_custom_art",
		},
		{
			name:       "asset_key falls back to entry asset key",
			mode:       ImageModeAssetKey,
			genericKey: "picord",
			entry:      Entry{DiscordAssetKey: "steam_620"},
			want:       "steam_620",
		},
		{
			name:       "asset_key falls back to generic",
			mode:       ImageModeAssetKey,
			genericKey: "picord",
			entry:      Entry{},
			want:       "picord",
		},
		{
			name:       "generic mode uses valid external URL when available",
			mode:       ImageModeGeneric,
			genericKey: "picord",
			entry:      Entry{ImageURL: "http://example.com/img.jpg"},
			want:       "http://example.com/img.jpg",
		},
		{
			name:       "generic mode falls back to generic key for non-HTTP image",
			mode:       ImageModeGeneric,
			genericKey: "picord",
			entry:      Entry{ImageURL: "firefox"},
			want:       "picord",
		},
		{
			name:       "external_url disabled falls back",
			mode:       ImageModeExternalURL,
			genericKey: "picord",
			externalOn: false,
			entry:      Entry{ImageURL: "http://example.com/img.jpg"},
			want:       "picord",
		},
		{
			name:       "external_url enabled returns image URL",
			mode:       ImageModeExternalURL,
			genericKey: "picord",
			externalOn: true,
			entry:      Entry{ImageURL: "http://example.com/img.jpg"},
			want:       "http://example.com/img.jpg",
		},
		{
			name:       "external_url skips non-HTTP desktop icons",
			mode:       ImageModeExternalURL,
			genericKey: "picord",
			externalOn: true,
			entry:      Entry{ImageURL: "firefox"},
			want:       "picord",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ImageResolver{
				Mode:            tt.mode,
				GenericAssetKey: tt.genericKey,
				ExternalEnabled: tt.externalOn,
			}
			got := r.Resolve(tt.entry, tt.profileImg)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestImageCacheDir(t *testing.T) {
	// Just ensure it returns a non-empty path.
	dir := ImageCacheDir()
	if dir == "" {
		t.Fatal("expected non-empty cache dir")
	}
}
