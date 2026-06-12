package sync

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to a minimin backend.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient creates a sync client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

// ManifestFile is a single entry in the server manifest.
type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// InfoResponse is the JSON returned by /api/client-archive/{token}/info.
type InfoResponse struct {
	Token      string   `json:"token"`
	ServerName string   `json:"serverName"`
	ExpiresAt  string   `json:"expiresAt"`
	CreatedAt  string   `json:"createdAt"`
	Formats    []string `json:"formats"`
}

// ManifestResponse is the JSON returned by /api/client-archive/{token}/manifest.
type ManifestResponse struct {
	ServerName string         `json:"serverName"`
	Files      []ManifestFile `json:"files"`
}

// FetchInfo retrieves archive metadata from the minimin backend.
func (c *Client) FetchInfo() (*InfoResponse, error) {
	url := fmt.Sprintf("%s/api/client-archive/%s/info", c.BaseURL, c.Token)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var i InfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&i); err != nil {
		return nil, err
	}
	return &i, nil
}

// FetchManifest retrieves the file manifest from the minimin backend.
func (c *Client) FetchManifest() (*ManifestResponse, error) {
	url := fmt.Sprintf("%s/api/client-archive/%s/manifest", c.BaseURL, c.Token)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	var m ManifestResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DownloadArchive downloads the archive to a temporary file and reports progress.
func (c *Client) DownloadArchive(format string, progress func(downloaded, total int64)) (string, error) {
	url := fmt.Sprintf("%s/api/client-archive/%s?format=%s", c.BaseURL, c.Token, format)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}

	total := resp.ContentLength
	tmpFile, err := os.CreateTemp("", "minimin-sync-*.zip")
	if err != nil {
		return "", err
	}
	defer func() { _ = tmpFile.Close() }()

	written := int64(0)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, werr := tmpFile.Write(buf[:n])
			if werr != nil {
				_ = os.Remove(tmpFile.Name())
				return "", werr
			}
			written += int64(n)
			if progress != nil {
				progress(written, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = os.Remove(tmpFile.Name())
			return "", err
		}
	}

	return tmpFile.Name(), nil
}

// ExtractAll extracts every entry from a zip archive into destDir.
func ExtractAll(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		target := filepath.Join(destDir, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		_ = out.Close()
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ComputeSHA256 computes the sha256 hash of a file.
func ComputeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
