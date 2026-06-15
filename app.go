package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"minimin-sync/pkg/config"
	"minimin-sync/pkg/discovery"
	"minimin-sync/pkg/instance"
	"minimin-sync/pkg/sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	config         *config.Config
	syncService    *sync.Service
	autoCheckReset chan struct{}
	updateTmpPath  string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cfg, err := config.Load()
	if err != nil {
		wailsruntime.LogErrorf(ctx, "failed to load config: %v", err)
		cfg = &config.Config{}
	}
	a.config = cfg

	if a.config.InstancesDir != "" && a.config.Launcher == "" {
		a.config.Launcher = detectLauncherFromPath(a.config.InstancesDir)
		_ = a.config.Save()
	}

	a.syncService = sync.NewService(ctx, a.config.InstancesDir)
	a.autoCheckReset = make(chan struct{})
	go a.autoCheckLoop()
	go a.checkSelfUpdateOnStartup()
}

func (a *App) checkSelfUpdateOnStartup() {
	time.Sleep(3 * time.Second)
	info, err := a.CheckForUpdate()
	if err != nil {
		return
	}
	if info["available"].(bool) {
		wailsruntime.EventsEmit(a.ctx, "selfUpdate:available", info)
	}
}

// RunManualCheck triggers an immediate auto-check across all servers.
func (a *App) RunManualCheck() {
	go func() {
		wailsruntime.EventsEmit(a.ctx, "autoCheck:start")
		a.runAutoCheck()
		wailsruntime.EventsEmit(a.ctx, "autoCheck:done")
	}()
}

func (a *App) autoCheckLoop() {
	for {
		interval := a.config.AutoCheckIntervalMinutes
		if interval <= 0 {
			interval = 5
		}
		timer := time.NewTimer(time.Duration(interval) * time.Minute)

		if !a.syncService.IsOperationRunning() {
			wailsruntime.EventsEmit(a.ctx, "autoCheck:start")
			a.runAutoCheck()
			wailsruntime.EventsEmit(a.ctx, "autoCheck:done")
		}

		select {
		case <-timer.C:
		case <-a.autoCheckReset:
		case <-a.ctx.Done():
			timer.Stop()
			return
		}
		timer.Stop()
	}
}

// IsOperationRunning reports whether AddServer or ApplyUpdates is active.
func (a *App) IsOperationRunning() bool {
	return a.syncService.IsOperationRunning()
}

func (a *App) runAutoCheck() {
	if a.config.InstancesDir == "" {
		return
	}
	servers, err := instance.Scan(a.config.InstancesDir)
	if err != nil {
		return
	}

	type updateInfo struct {
		ServerID      string `json:"serverID"`
		Name          string `json:"name"`
		MissingCount  int    `json:"missingCount"`
		OutdatedCount int    `json:"outdatedCount"`
	}

	var updates []updateInfo
	for _, s := range servers {
		if s.Marker == nil {
			continue
		}
		res, err := a.syncService.CheckUpdates(s.Name)
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "checkUpdates:error", map[string]string{
				"serverID": s.Name,
				"error":    err.Error(),
			})
			continue
		}
		wailsruntime.EventsEmit(a.ctx, "checkUpdates:ok", map[string]string{
			"serverID": s.Name,
		})
		missingCnt, outdatedCnt := 0, 0
		if v, ok := res["missing"].([]sync.ManifestFile); ok {
			missingCnt = len(v)
		}
		if v, ok := res["outdated"].([]sync.ManifestFile); ok {
			outdatedCnt = len(v)
		}
		if missingCnt > 0 || outdatedCnt > 0 {
			updates = append(updates, updateInfo{
				ServerID:      s.Name,
				Name:          s.Name,
				MissingCount:  missingCnt,
				OutdatedCount: outdatedCnt,
			})
		}
	}
	if len(updates) > 0 {
		wailsruntime.EventsEmit(a.ctx, "updates:available", updates)
	}
	wailsruntime.EventsEmit(a.ctx, "servers:reload")
}

// GetVersion returns the current application version.
func (a *App) GetVersion() string {
	return version
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() config.Config {
	if a.config == nil {
		return config.Config{}
	}
	return *a.config
}

// SaveConfig persists the current configuration.
func (a *App) SaveConfig(cfg config.Config) error {
	oldInterval := a.config.AutoCheckIntervalMinutes
	a.config = &cfg
	if err := a.config.Save(); err != nil {
		return err
	}
	if a.syncService != nil {
		a.syncService = sync.NewService(a.ctx, cfg.InstancesDir)
	}
	if oldInterval != cfg.AutoCheckIntervalMinutes && a.autoCheckReset != nil {
		select {
		case a.autoCheckReset <- struct{}{}:
		default:
		}
	}
	return nil
}

// DiscoverAllLaunchers returns every valid launcher instances directory found.
func (a *App) DiscoverAllLaunchers() []string {
	return discovery.FindAllLaunchers()
}

// SelectInstancesDir opens an OS folder picker dialog.
func (a *App) SelectInstancesDir() (string, error) {
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Prism Launcher instances directory",
	})
}

// GetServers scans the instances directory and returns synced servers.
func (a *App) GetServers() ([]instance.ScannedInstance, error) {
	dir := a.config.InstancesDir
	if dir == "" {
		return nil, fmt.Errorf("instances directory not configured")
	}
	return instance.Scan(dir)
}

// RemoveServer deletes a synced server instance directory.
func (a *App) RemoveServer(serverID string) error {
	if a.config.InstancesDir == "" {
		return fmt.Errorf("instances directory not configured")
	}
	instanceDir := filepath.Join(a.config.InstancesDir, serverID)
	return os.RemoveAll(instanceDir)
}

// PreviewServer fetches archive info for a URL without installing.
func (a *App) PreviewServer(url string) (sync.InfoResponse, error) {
	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := sync.ParseArchiveURL(url)
	if err != nil {
		return sync.InfoResponse{}, err
	}
	client := sync.NewClient(baseURL, token)
	info, err := client.FetchInfo()
	if err != nil {
		return sync.InfoResponse{}, err
	}
	return *info, nil
}

// AddServer installs a new server from a prism archive URL asynchronously.
func (a *App) AddServer(url string) error {
	return a.syncService.AddServer(url)
}

// CheckUpdates compares local files with the remote manifest.
func (a *App) CheckUpdates(serverID string) (map[string]interface{}, error) {
	return a.syncService.CheckUpdates(serverID)
}

// ApplyUpdates downloads individual files and applies selected changes asynchronously.
func (a *App) ApplyUpdates(serverID string, selected []string) error {
	return a.syncService.ApplyUpdates(serverID, selected)
}

func detectLauncherFromPath(dir string) string {
	lower := strings.ToLower(dir)
	if strings.Contains(lower, "elyprismlauncher") {
		return "elyprismlauncher"
	}
	if strings.Contains(lower, "prismlauncher") {
		return "prismlauncher"
	}
	if strings.Contains(lower, "multimc") {
		return "multimc"
	}
	return "prismlauncher"
}

// OpenInstanceDir opens the instance directory in the OS file manager.
func (a *App) OpenInstanceDir(serverID string) error {
	dir := a.config.InstancesDir
	if dir == "" {
		return fmt.Errorf("instances directory not configured")
	}
	instanceDir := filepath.Join(dir, serverID)
	if _, err := os.Stat(instanceDir); err != nil {
		return fmt.Errorf("instance directory not found: %w", err)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", instanceDir)
	case "windows":
		cmd = exec.Command("explorer", instanceDir)
	default:
		cmd = exec.Command("xdg-open", instanceDir)
	}
	return cmd.Start()
}

// findLauncherBinary tries to locate the launcher executable.
// It first checks PATH, then falls back to standard install directories.
func findLauncherBinary(name string) (string, error) {
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(name, ".exe") {
			if p, err := exec.LookPath(name + ".exe"); err == nil {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		var candidates []string
		switch strings.ToLower(name) {
		case "prismlauncher", "prism-launcher":
			candidates = []string{
				filepath.Join(localAppData, "Programs", "PrismLauncher", "prismlauncher.exe"),
				filepath.Join(programFiles, "PrismLauncher", "prismlauncher.exe"),
				filepath.Join(programFilesX86, "PrismLauncher", "prismlauncher.exe"),
			}
		case "elyprismlauncher":
			candidates = []string{
				filepath.Join(localAppData, "Programs", "ElyPrismLauncher", "elyprismlauncher.exe"),
				filepath.Join(localAppData, "Programs", "ElyPrismLauncher", "prismlauncher.exe"),
				filepath.Join(programFiles, "ElyPrismLauncher", "elyprismlauncher.exe"),
				filepath.Join(programFiles, "ElyPrismLauncher", "prismlauncher.exe"),
				filepath.Join(programFilesX86, "ElyPrismLauncher", "elyprismlauncher.exe"),
				filepath.Join(programFilesX86, "ElyPrismLauncher", "prismlauncher.exe"),
			}
		case "multimc":
			candidates = []string{
				filepath.Join(localAppData, "Programs", "MultiMC", "MultiMC.exe"),
				filepath.Join(programFiles, "MultiMC", "MultiMC.exe"),
				filepath.Join(programFilesX86, "MultiMC", "MultiMC.exe"),
			}
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	} else if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		var candidates []string
		switch strings.ToLower(name) {
		case "prismlauncher", "prism-launcher":
			candidates = []string{
				"/Applications/Prism Launcher.app/Contents/MacOS/prismlauncher",
				filepath.Join(home, "Applications", "Prism Launcher.app", "Contents", "MacOS", "prismlauncher"),
			}
		case "elyprismlauncher":
			candidates = []string{
				"/Applications/ElyPrism Launcher.app/Contents/MacOS/elyprismlauncher",
				filepath.Join(home, "Applications", "ElyPrism Launcher.app", "Contents", "MacOS", "elyprismlauncher"),
			}
		case "multimc":
			candidates = []string{
				"/Applications/MultiMC.app/Contents/MacOS/MultiMC",
				filepath.Join(home, "Applications", "MultiMC.app", "Contents", "MacOS", "MultiMC"),
			}
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	}

	return "", fmt.Errorf("launcher %q not found", name)
}

// RunServer launches the given instance via the configured launcher binary.
func (a *App) RunServer(serverID string) error {
	binary := a.config.Launcher
	if binary == "" {
		binary = "prismlauncher"
	}

	launcher, err := findLauncherBinary(binary)
	if err != nil {
		var fallbacks []string
		switch binary {
		case "elyprismlauncher":
			fallbacks = []string{"prismlauncher", "prism-launcher"}
		case "prismlauncher":
			fallbacks = []string{"prism-launcher"}
		case "multimc":
			fallbacks = []string{"multimc-qt5"}
		}
		for _, fb := range fallbacks {
			launcher, err = findLauncherBinary(fb)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("launcher %q not found in PATH or standard directories", binary)
		}
	}
	cmd := exec.Command(launcher, "--launch", serverID)
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

// RefreshServerInfo fetches archive metadata and updates ExpiresAt in the marker.
func (a *App) RefreshServerInfo(serverID string) error {
	dir := a.config.InstancesDir
	if dir == "" {
		return fmt.Errorf("instances directory not configured")
	}
	instanceDir := filepath.Join(dir, serverID)
	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		return err
	}

	client := sync.NewClient(marker.BaseURL, marker.Token)
	info, err := client.FetchInfo()
	if err != nil {
		return err
	}

	marker.ExpiresAt = info.ExpiresAt
	marker.LastCheckAt = time.Now().UTC().Format(time.RFC3339)
	return instance.WriteMarker(instanceDir, marker)
}

// UpdateServerURL updates the archive link for an existing server.
func (a *App) UpdateServerURL(serverID, url string) error {
	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := sync.ParseArchiveURL(url)
	if err != nil {
		return err
	}
	dir := a.config.InstancesDir
	if dir == "" {
		return fmt.Errorf("instances directory not configured")
	}
	instanceDir := filepath.Join(dir, serverID)
	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		return err
	}

	client := sync.NewClient(baseURL, token)
	info, err := client.FetchInfo()
	if err != nil {
		return err
	}

	marker.Token = token
	marker.BaseURL = baseURL
	marker.ExpiresAt = info.ExpiresAt
	return instance.WriteMarker(instanceDir, marker)
}

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
