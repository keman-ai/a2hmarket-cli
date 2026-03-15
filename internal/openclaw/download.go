package openclaw

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true, ".svg": true, ".ico": true,
}

func IsImageFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return imageExtensions[ext]
}

func IsImageMIME(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "image/")
}

// DownloadDir returns the media download directory, creating it if needed.
// Uses ~/.openclaw/workspace/a2hmarket/ so that openclaw's feishu plugin
// can read the files for native media display.
// Falls back to ~/.a2hmarket/download/ if the openclaw workspace doesn't exist.
func DownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Prefer openclaw workspace for feishu media compatibility
	openclawDir := filepath.Join(home, ".openclaw", "workspace", "a2hmarket")
	if err := os.MkdirAll(openclawDir, 0755); err == nil {
		return openclawDir, nil
	}

	// Fallback
	dir := filepath.Join(home, ".a2hmarket", "download")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// DownloadFile downloads a URL to ~/.a2hmarket/download/<filename>.
// Returns the local path on success.
func DownloadFile(url, filename string) (string, error) {
	dir, err := DownloadDir()
	if err != nil {
		return "", fmt.Errorf("download dir: %w", err)
	}

	if filename == "" {
		filename = filenameFromURL(url)
	}
	// Prefix with timestamp to avoid collisions
	ts := time.Now().Format("20060102_150405")
	filename = ts + "_" + filename

	localPath := filepath.Join(dir, filename)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	// Reject files larger than 50MB
	const maxSize = 50 * 1024 * 1024
	if resp.ContentLength > maxSize {
		return "", fmt.Errorf("download %s: file too large (%d bytes, max %d)", url, resp.ContentLength, maxSize)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	written, err := io.Copy(f, io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		os.Remove(localPath)
		return "", fmt.Errorf("download %s: write: %w", url, err)
	}
	if written > maxSize {
		os.Remove(localPath)
		return "", fmt.Errorf("download %s: file too large (exceeded %d bytes)", url, maxSize)
	}

	return localPath, nil
}

func filenameFromURL(rawURL string) string {
	parts := strings.Split(rawURL, "/")
	last := parts[len(parts)-1]
	if idx := strings.Index(last, "?"); idx >= 0 {
		last = last[:idx]
	}
	if last == "" {
		return "attachment"
	}
	return last
}
