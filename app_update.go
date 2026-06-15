package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// CheckForUpdate queries GitHub for the latest release.
func (a *App) CheckForUpdate() (map[string]interface{}, error) {
	const owner = "quonaro"
	const repo = "minimin-sync"
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "minimin-sync")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	var assetName string
	switch {
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		assetName = "minimin-sync-windows-amd64.exe"
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		assetName = "minimin-sync-linux-amd64"
	case runtime.GOOS == "darwin":
		assetName = "minimin-sync-darwin-universal.zip"
	default:
		return nil, fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.URL
			break
		}
	}
	if downloadURL == "" {
		return nil, fmt.Errorf("no asset %s found in release %s", assetName, release.TagName)
	}

	available := release.TagName != version && version != "dev"
	return map[string]interface{}{
		"available": available,
		"version":   release.TagName,
		"url":       downloadURL,
		"current":   version,
	}, nil
}

type progressReader struct {
	r          io.Reader
	total      int64
	current    int64
	onProgress func(downloaded, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	pr.current += int64(n)
	if pr.onProgress != nil {
		pr.onProgress(pr.current, pr.total)
	}
	return n, err
}

// DownloadUpdate downloads the latest release asset to a temporary file.
func (a *App) DownloadUpdate() error {
	info, err := a.CheckForUpdate()
	if err != nil {
		return err
	}
	if !info["available"].(bool) && version != "dev" {
		return fmt.Errorf("already up to date")
	}

	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return err
	}

	tmpFile := currentExe + ".tmp"

	// If already downloaded, just emit done.
	if _, err := os.Stat(tmpFile); err == nil {
		a.updateTmpPath = tmpFile
		wailsruntime.EventsEmit(a.ctx, "updateSelf:done")
		return nil
	}

	downloadURL := info["url"].(string)
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "minimin-sync")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	out, err := os.Create(tmpFile)
	if err != nil {
		return err
	}

	pr := &progressReader{
		r:          resp.Body,
		total:      resp.ContentLength,
		onProgress: func(d, t int64) { wailsruntime.EventsEmit(a.ctx, "updateSelf:progress", d, t) },
	}
	_, err = io.Copy(out, pr)
	out.Close()
	if err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	a.updateTmpPath = tmpFile
	wailsruntime.EventsEmit(a.ctx, "updateSelf:done")
	return nil
}

// RestartApp replaces the running binary with the downloaded update and restarts.
func (a *App) RestartApp() error {
	if a.updateTmpPath == "" {
		return fmt.Errorf("no update downloaded")
	}

	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return err
	}
	tmpFile := a.updateTmpPath
	a.updateTmpPath = ""

	if runtime.GOOS == "windows" {
		scriptPath := filepath.Join(os.TempDir(), "minimin-update.bat")
		script := fmt.Sprintf("@echo off\r\ntimeout /t 1 /nobreak >nul\r\nmove /Y %q %q\r\nstart \"\" %q\r\ndel \"%%~f0\"\r\n", tmpFile, currentExe, currentExe)
		if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
			_ = os.Remove(tmpFile)
			return err
		}
		cmd := exec.Command("cmd", "/c", scriptPath)
		if err := cmd.Start(); err != nil {
			_ = os.Remove(scriptPath)
			_ = os.Remove(tmpFile)
			return err
		}
		wailsruntime.Quit(a.ctx)
		return nil
	}

	if runtime.GOOS == "darwin" {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("auto-update is not supported on macOS")
	}

	if err := os.Rename(tmpFile, currentExe); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}
	if err := os.Chmod(currentExe, 0755); err != nil {
		return err
	}
	cmd := exec.Command(currentExe, os.Args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	wailsruntime.Quit(a.ctx)
	return nil
}

// CancelUpdate removes the downloaded update file without restarting.
func (a *App) CancelUpdate() error {
	if a.updateTmpPath != "" {
		_ = os.Remove(a.updateTmpPath)
		a.updateTmpPath = ""
	}
	return nil
}

// HasPendingUpdate reports whether a downloaded update file is waiting to be applied.
func (a *App) HasPendingUpdate() bool {
	currentExe, err := os.Executable()
	if err != nil {
		return false
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return false
	}
	_, err = os.Stat(currentExe + ".tmp")
	return err == nil
}
