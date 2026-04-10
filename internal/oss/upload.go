// Package oss provides utilities for uploading files to the platform's OSS service.
//
// OSS uploads follow a 2-step flow:
//  1. POST to the sign endpoint to obtain a pre-signed upload URL and headers.
//  2. PUT the file binary directly to the pre-signed URL (no platform auth needed here).
//
// The resulting public URL is the pre-signed URL stripped of its query string.
package oss

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keman-ai/a2hmarket-cli/internal/api"
	"github.com/keman-ai/a2hmarket-cli/internal/config"
)

const (
	// OSSBaseURL is the OSS sign service base URL (independent from the main API host).
	OSSBaseURL   = "https://api.a2hmarket.ai/findu-oss"
	ossSignPath  = "/api/v1/oss_signurl/upload/sign"
	ossSignSignPath = "/findu-oss/api/v1/oss_signurl/upload/sign"

	DefaultExpiresHours = 24
)

// MIME maps file extensions to MIME types.
var mimeMap = map[string]string{
	// images
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp",
	// video
	".mp4": "video/mp4", ".mov": "video/quicktime", ".avi": "video/x-msvideo",
	".mpeg": "video/mpeg", ".mpg": "video/mpeg",
	// documents
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	// text
	".txt": "text/plain", ".csv": "text/csv", ".md": "text/markdown",
	// archives
	".zip": "application/zip", ".tar": "application/x-tar", ".gz": "application/gzip",
	// audio
	".mp3": "audio/mpeg",
}

// ProfileQRCodeMIME lists MIME types accepted for payment QR codes.
var ProfileQRCodeMIME = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".png": "image/png", ".webp": "image/webp",
}

// FileInfo holds the result of a successful OSS upload.
type FileInfo struct {
	URL          string `json:"url"`
	ObjectKey    string `json:"object_key,omitempty"`
	FileName     string `json:"file_name"`
	FileSize     int64  `json:"file_size"`
	MIMEType     string `json:"mime_type"`
	UploadSubtype string `json:"upload_subtype"`
	ExpiresAt    string `json:"expires_at"`
	ExpiresHours int    `json:"expires_hours"`
	Source       string `json:"source"`
}

// ossSignResponse is the response from the sign endpoint.
type ossSignResponse struct {
	UploadURL     string            `json:"upload_url"`
	ObjectKey     string            `json:"object_key"`
	SignedHeaders map[string]string `json:"signed_headers"`
}

// MIMEFromPath returns the MIME type for the given file path, or "" if unsupported.
func MIMEFromPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	return mimeMap[ext]
}

// SubtypeFromMIME maps a MIME type to an OSS upload_subtype.
func SubtypeFromMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	default:
		return "document"
	}
}

// PublicURL strips the query string from a pre-signed URL to get the public object URL.
func PublicURL(signedURL string) string {
	u, err := url.Parse(signedURL)
	if err != nil {
		if idx := strings.Index(signedURL, "?"); idx >= 0 {
			return signedURL[:idx]
		}
		return signedURL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// Upload uploads the file at localPath to OSS using the provided upload type and MIME override list.
// uploadType is "chatfile" (for A2A attachments) or "profile" (for profile images).
// allowedMIME is the subset of mimeMap to accept; nil means accept all.
func Upload(creds *config.Credentials, localPath, uploadType string, allowedMIME map[string]string) (*FileInfo, error) {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, fmt.Errorf("oss: resolve path: %w", err)
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("oss: file not found: %s", absPath)
		}
		return nil, fmt.Errorf("oss: stat file: %w", err)
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("oss: not a regular file: %s", absPath)
	}
	if stat.Size() == 0 {
		return nil, fmt.Errorf("oss: file is empty: %s", absPath)
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	mimeType := mimeMap[ext]

	if allowedMIME != nil {
		if m, ok := allowedMIME[ext]; ok {
			mimeType = m
		} else {
			supported := make([]string, 0, len(allowedMIME))
			for k := range allowedMIME {
				supported = append(supported, k)
			}
			return nil, fmt.Errorf("oss: unsupported file type %q (allowed: %s)", ext, strings.Join(supported, " "))
		}
	} else if mimeType == "" {
		return nil, fmt.Errorf("oss: unsupported file type %q", ext)
	}

	fileName := filepath.Base(absPath)
	uploadSubtype := SubtypeFromMIME(mimeType)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("oss: read file: %w", err)
	}

	// Step 1: get pre-signed upload URL
	apiCreds := api.Credentials{
		AgentID:  creds.AgentID,
		AgentKey: creds.AgentKey,
		BaseURL:  strings.TrimRight(creds.APIURL, "/"),
	}
	client := api.NewClient(apiCreds, 30*time.Second)

	signBody := map[string]interface{}{
		"upload_type":    uploadType,
		"upload_subtype": uploadSubtype,
		"file_name":      fileName,
		"file_size":      stat.Size(),
		"file_type":      mimeType,
	}

	var signResp ossSignResponse
	if err := client.PostJSONToHost(OSSBaseURL, ossSignPath, ossSignSignPath, signBody, &signResp); err != nil {
		return nil, fmt.Errorf("oss: sign request: %w", err)
	}
	if signResp.UploadURL == "" {
		return nil, fmt.Errorf("oss: sign endpoint returned no upload_url")
	}

	// Step 2: PUT binary to OSS
	if err := client.PutBinary(signResp.UploadURL, signResp.SignedHeaders, data); err != nil {
		return nil, fmt.Errorf("oss: upload binary: %w", err)
	}

	publicURL := PublicURL(signResp.UploadURL)
	expiresAt := time.Now().UTC().Add(DefaultExpiresHours * time.Hour).Format(time.RFC3339)

	return &FileInfo{
		URL:           publicURL,
		ObjectKey:     signResp.ObjectKey,
		FileName:      fileName,
		FileSize:      stat.Size(),
		MIMEType:      mimeType,
		UploadSubtype: uploadSubtype,
		ExpiresAt:     expiresAt,
		ExpiresHours:  DefaultExpiresHours,
		Source:        "oss",
	}, nil
}
