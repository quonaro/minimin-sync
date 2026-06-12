package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
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

// AddServer installs a new server from a prism archive URL asynchronously.
func (a *App) AddServer(url string) error {
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

		runtime.EventsEmit(a.ctx, "addServer:status", "downloading archive")
		tmpPath, err := client.DownloadArchive("prism", func(d, t int64) {
			runtime.EventsEmit(a.ctx, "addServer:progress", d, t)
		})
		if err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}
		defer func() { _ = os.Remove(tmpPath) }()

		instanceDir := filepath.Join(a.config.InstancesDir, info.ServerName)
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
			ServerID:   info.ServerName,
			Token:      token,
			BaseURL:    baseURL,
			LastSyncAt: info.CreatedAt,
		}
		if err := instance.WriteMarker(instanceDir, marker); err != nil {
			runtime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		a.config.Servers = append(a.config.Servers, config.Server{
			ID:      info.ServerName,
			Name:    info.ServerName,
			Token:   token,
			BaseURL: baseURL,
		})
		_ = a.config.Save()

		runtime.EventsEmit(a.ctx, "addServer:done", info.ServerName)
	}()

	return nil
}

// CheckUpdates compares local files with the remote manifest.
func (a *App) CheckUpdates(serverID string) (map[string]interface{}, error) {
	dir := a.config.InstancesDir
	if dir == "" {
		return nil, fmt.Errorf("instances directory not configured")
	}

	instanceDir := filepath.Join(dir, serverID)
	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		return nil, err
	}

	client := sync.NewClient(marker.BaseURL, marker.Token)
	manifest, err := client.FetchManifest()
	if err != nil {
		return nil, err
	}

	localDir := filepath.Join(instanceDir, ".minecraft")
	var missing []sync.ManifestFile
	var outdated []sync.ManifestFile
	localFiles := make(map[string]bool)

	for _, mf := range manifest.Files {
		localPath := filepath.Join(localDir, filepath.FromSlash(mf.Path))
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

	return map[string]interface{}{
		"missing":  missing,
		"outdated": outdated,
		"orphan":   orphan,
	}, nil
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

func parseArchiveURL(url string) (token, baseURL string, err error) {
	parts := strings.Split(url, "/api/client-archive/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid archive URL")
	}
	baseURL = strings.TrimSuffix(parts[0], "/")
	tokenPart := parts[1]
	if idx := strings.Index(tokenPart, "?"); idx != -1 {
		tokenPart = tokenPart[:idx]
	}
	token = strings.Trim(tokenPart, "/")
	if token == "" {
		return "", "", fmt.Errorf("token not found in URL")
	}
	return token, baseURL, nil
}
