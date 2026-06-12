package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"minimin-sync/pkg/config"
	"minimin-sync/pkg/discovery"
	"minimin-sync/pkg/instance"
	"minimin-sync/pkg/sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx    context.Context
	config *config.Config
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
		runtime.LogErrorf(ctx, "failed to load config: %v", err)
		cfg = &config.Config{}
	}
	a.config = cfg

	if a.config.InstancesDir != "" && a.config.Launcher == "" {
		a.config.Launcher = detectLauncherFromPath(a.config.InstancesDir)
		_ = a.config.Save()
	}

	go a.autoCheckLoop()
}

func (a *App) autoCheckLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	a.runAutoCheck()

	for {
		select {
		case <-ticker.C:
			a.runAutoCheck()
		case <-a.ctx.Done():
			return
		}
	}
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
		res, err := a.checkUpdatesInternal(s.Name)
		if err != nil {
			continue
		}
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
		runtime.EventsEmit(a.ctx, "updates:available", updates)
	}
	runtime.EventsEmit(a.ctx, "servers:reload")
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
	a.config = &cfg
	return a.config.Save()
}

// DiscoverAllLaunchers returns every valid launcher instances directory found.
func (a *App) DiscoverAllLaunchers() []string {
	return discovery.FindAllLaunchers()
}

// SelectInstancesDir opens an OS folder picker dialog.
func (a *App) SelectInstancesDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
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
	token, baseURL, err := parseArchiveURL(url)
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
	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := parseArchiveURL(url)
	if err != nil {
		return err
	}

	go func() {
		client := sync.NewClient(baseURL, token)

		runtime.EventsEmit(a.ctx, "addServer:status", "fetching info")
		info, err := client.FetchInfo()
		if err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		name := sanitizeName(info.ServerName)
		if name == "" {
			runtime.EventsEmit(a.ctx, "addServer:error", "server name is empty")
			return
		}

		runtime.EventsEmit(a.ctx, "addServer:status", "downloading archive")
		tmpPath, err := client.DownloadArchive("prism", func(d, t int64) {
			runtime.EventsEmit(a.ctx, "addServer:progress", d, t)
		})
		if err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}
		defer func() { _ = os.Remove(tmpPath) }()

		instanceDir := filepath.Join(a.config.InstancesDir, name)
		if err := os.MkdirAll(instanceDir, 0o755); err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		runtime.EventsEmit(a.ctx, "addServer:status", "extracting")
		if err := sync.ExtractAll(tmpPath, instanceDir); err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		marker := &instance.Marker{
			ServerID:   name,
			Token:      token,
			BaseURL:    baseURL,
			LastSyncAt: time.Now().UTC().Format(time.RFC3339),
			ExpiresAt:  info.ExpiresAt,
		}
		runtime.LogInfof(a.ctx, "writing marker to %s", filepath.Join(instanceDir, instance.MarkerFile))
		if err := instance.WriteMarker(instanceDir, marker); err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		runtime.EventsEmit(a.ctx, "addServer:done", info.ServerName)
	}()

	return nil
}

func (a *App) checkUpdatesInternal(serverID string) (map[string]interface{}, error) {
	dir := a.config.InstancesDir
	if dir == "" {
		return nil, fmt.Errorf("instances directory not configured")
	}

	instanceDir := filepath.Join(dir, serverID)
	runtime.LogInfof(a.ctx, "checking updates for %s in %s", serverID, instanceDir)

	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		runtime.LogErrorf(a.ctx, "read marker failed: %v", err)
		return nil, err
	}
	runtime.LogInfof(a.ctx, "marker read: baseURL=%s token=%s...", marker.BaseURL, marker.Token[:8])

	client := sync.NewClient(marker.BaseURL, marker.Token)

	info, err := client.FetchInfo()
	if err != nil {
		runtime.LogErrorf(a.ctx, "fetch info failed for %s: %v", serverID, err)
	} else {
		marker.ExpiresAt = info.ExpiresAt
	}

	manifest, err := client.FetchManifest()
	if err != nil {
		runtime.LogErrorf(a.ctx, "fetch manifest failed: %v", err)
		return nil, err
	}
	runtime.LogInfof(a.ctx, "manifest fetched: %d files", len(manifest.Files))

	localDir := filepath.Join(instanceDir, ".minecraft")
	var missing []sync.ManifestFile
	var outdated []sync.ManifestFile
	localFiles := make(map[string]bool)

	for _, mf := range manifest.Files {
		localPath := filepath.Join(instanceDir, filepath.FromSlash(mf.Path))
		info, err := os.Stat(localPath)
		if err != nil {
			missing = append(missing, mf)
			continue
		}
		localFiles[mf.Path] = true
		if info.Size() != mf.Size {
			outdated = append(outdated, mf)
			continue
		}
		hash, err := sync.ComputeSHA256(localPath)
		if err != nil || hash != mf.SHA256 {
			outdated = append(outdated, mf)
		}
	}

	var orphan []string
	for _, sub := range []string{"mods", "resourcepacks", "shaderpacks"} {
		subDir := filepath.Join(localDir, sub)
		entries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			relPath := filepath.ToSlash(filepath.Join(".minecraft", sub, e.Name()))
			if !localFiles[relPath] {
				orphan = append(orphan, relPath)
			}
		}
	}

	result := map[string]interface{}{
		"missing":  missing,
		"outdated": outdated,
		"orphan":   orphan,
	}
	runtime.LogInfof(a.ctx, "check complete: missing=%d outdated=%d orphan=%d", len(missing), len(outdated), len(orphan))

	marker.LastCheckAt = time.Now().UTC().Format(time.RFC3339)
	_ = instance.WriteMarker(instanceDir, marker)

	return result, nil
}

// CheckUpdates compares local files with the remote manifest.
func (a *App) CheckUpdates(serverID string) (map[string]interface{}, error) {
	return a.checkUpdatesInternal(serverID)
}

// ApplyUpdates downloads the zip archive and applies selected files asynchronously.
func (a *App) ApplyUpdates(serverID string, selected []string) error {
	dir := a.config.InstancesDir
	if dir == "" {
		return fmt.Errorf("instances directory not configured")
	}

	instanceDir := filepath.Join(dir, serverID)
	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		return err
	}

	go func() {
		client := sync.NewClient(marker.BaseURL, marker.Token)

		runtime.EventsEmit(a.ctx, "applyUpdates:status", "downloading archive")
		tmpPath, err := client.DownloadArchive("zip", func(d, t int64) {
			runtime.EventsEmit(a.ctx, "applyUpdates:progress", d, t)
		})
		if err != nil {
			runtime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
			return
		}
		defer func() { _ = os.Remove(tmpPath) }()

		selectedMap := make(map[string]bool)
		for _, p := range selected {
			zipPath := strings.TrimPrefix(p, ".minecraft/")
			selectedMap[zipPath] = true
		}

		backupDir := filepath.Join(instanceDir, ".minimin-backup")
		_ = os.RemoveAll(backupDir)

		for _, p := range selected {
			src := filepath.Join(instanceDir, filepath.FromSlash(p))
			if _, err := os.Stat(src); err == nil {
				dst := filepath.Join(backupDir, filepath.FromSlash(p))
				_ = os.MkdirAll(filepath.Dir(dst), 0o755)
				_ = os.Rename(src, dst)
			}
		}

		runtime.EventsEmit(a.ctx, "applyUpdates:status", "extracting files")
		zr, err := zip.OpenReader(tmpPath)
		if err != nil {
			runtime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
			return
		}
		defer func() { _ = zr.Close() }()

		for _, f := range zr.File {
			if !selectedMap[f.Name] {
				continue
			}
			target := filepath.Join(instanceDir, ".minecraft", filepath.FromSlash(f.Name))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				runtime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
				return
			}
			rc, err := f.Open()
			if err != nil {
				runtime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
				return
			}
			out, err := os.Create(target)
			if err != nil {
				_ = rc.Close()
				runtime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
				return
			}
			_, err = io.Copy(out, rc)
			_ = out.Close()
			_ = rc.Close()
			if err != nil {
				runtime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
				return
			}
		}

		marker.LastSyncAt = time.Now().UTC().Format(time.RFC3339)
		_ = instance.WriteMarker(instanceDir, marker)
		_ = os.RemoveAll(backupDir)

		runtime.EventsEmit(a.ctx, "applyUpdates:done", serverID)
	}()

	return nil
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

// RunServer launches the given instance via the configured launcher binary.
func (a *App) RunServer(serverID string) error {
	binary := a.config.Launcher
	if binary == "" {
		binary = "prismlauncher"
	}

	launcher, err := exec.LookPath(binary)
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
			launcher, err = exec.LookPath(fb)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("launcher %q not found in PATH", binary)
		}
	}
	cmd := exec.Command(launcher, "--launch", serverID)
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

// UpdateServerURL updates the archive link for an existing server.
func (a *App) UpdateServerURL(serverID, url string) error {
	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := parseArchiveURL(url)
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

func sanitizeName(name string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
		" ", "_",
	)
	return strings.TrimSpace(replacer.Replace(name))
}

func parseArchiveURL(rawURL string) (token, baseURL string, err error) {
	for _, sep := range []string{"/api/client-archive/", "/client-archive/"} {
		parts := strings.Split(rawURL, sep)
		if len(parts) == 2 {
			baseURL = strings.TrimSuffix(parts[0], "/")
			tokenPart := parts[1]
			if idx := strings.Index(tokenPart, "?"); idx != -1 {
				tokenPart = tokenPart[:idx]
			}
			token = strings.Trim(tokenPart, "/")
			if token != "" {
				return token, baseURL, nil
			}
		}
	}
	return "", "", fmt.Errorf("invalid archive URL")
}
