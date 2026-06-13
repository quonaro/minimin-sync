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
	"sync/atomic"
	"time"

	"minimin-sync/pkg/config"
	"minimin-sync/pkg/discovery"
	"minimin-sync/pkg/disk"
	"minimin-sync/pkg/instance"
	"minimin-sync/pkg/sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	config         *config.Config
	opRunning      atomic.Bool
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

	a.autoCheckReset = make(chan struct{})
	go a.autoCheckLoop()
}

func (a *App) autoCheckLoop() {
	for {
		interval := a.config.AutoCheckIntervalMinutes
		if interval <= 0 {
			interval = 5
		}
		timer := time.NewTimer(time.Duration(interval) * time.Minute)

		a.runAutoCheck()

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
	return a.opRunning.Load()
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

// uniqueInstanceName returns a directory name that does not already exist.
func (a *App) uniqueInstanceName(base string) string {
	name := base
	for i := 1; ; i++ {
		candidate := filepath.Join(a.config.InstancesDir, name)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// AddServer installs a new server from a prism archive URL asynchronously.
func (a *App) AddServer(url string) error {
	if !a.opRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("another operation is already in progress")
	}

	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := parseArchiveURL(url)
	if err != nil {
		a.opRunning.Store(false)
		return err
	}

	go func() {
		defer a.opRunning.Store(false)
		client := sync.NewClient(baseURL, token)

		wailsruntime.EventsEmit(a.ctx, "addServer:status", "fetching info")
		info, err := client.FetchInfo()
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		name := sanitizeName(info.ServerName)
		if name == "" {
			wailsruntime.EventsEmit(a.ctx, "addServer:error", "server name is empty")
			return
		}
		name = a.uniqueInstanceName(name)

		wailsruntime.EventsEmit(a.ctx, "addServer:status", "downloading archive")
		size, err := client.ArchiveSize("prism")
		if err == nil && size > 0 {
			free, derr := disk.FreeBytes(a.config.InstancesDir)
			if derr == nil && uint64(size) > free {
				wailsruntime.EventsEmit(a.ctx, "addServer:error", fmt.Sprintf("not enough disk space (need %d MB, have %d MB)", size/1024/1024, int64(free)/1024/1024))
				return
			}
		}

		tmpPath, err := client.DownloadArchive("prism", func(d, t int64) {
			wailsruntime.EventsEmit(a.ctx, "addServer:progress", d, t)
		})
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}
		defer func() { _ = os.Remove(tmpPath) }()

		instanceDir := filepath.Join(a.config.InstancesDir, name)
		if err := os.MkdirAll(instanceDir, 0o755); err != nil {
			wailsruntime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}
		cleanOnFail := true
		defer func() {
			if cleanOnFail {
				_ = os.RemoveAll(instanceDir)
			}
		}()

		wailsruntime.EventsEmit(a.ctx, "addServer:status", "extracting")
		if err := sync.ExtractAll(tmpPath, instanceDir); err != nil {
			wailsruntime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		marker := &instance.Marker{
			ServerID:   name,
			Token:      token,
			BaseURL:    baseURL,
			LastSyncAt: time.Now().UTC().Format(time.RFC3339),
			ExpiresAt:  info.ExpiresAt,
		}
		wailsruntime.LogInfof(a.ctx, "writing marker to %s", filepath.Join(instanceDir, instance.MarkerFile))
		if err := instance.WriteMarker(instanceDir, marker); err != nil {
			wailsruntime.EventsEmit(a.ctx, "addServer:error", err.Error())
			return
		}

		cleanOnFail = false
		wailsruntime.EventsEmit(a.ctx, "addServer:done", info.ServerName)
	}()

	return nil
}

func (a *App) checkUpdatesInternal(serverID string) (map[string]interface{}, error) {
	dir := a.config.InstancesDir
	if dir == "" {
		return nil, fmt.Errorf("instances directory not configured")
	}

	instanceDir := filepath.Join(dir, serverID)
	wailsruntime.LogInfof(a.ctx, "checking updates for %s in %s", serverID, instanceDir)
	wailsruntime.EventsEmit(a.ctx, "checkUpdates:status", "connecting")

	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "read marker failed: %v", err)
		return nil, err
	}
	wailsruntime.LogInfof(a.ctx, "marker read: baseURL=%s token=%s...", marker.BaseURL, marker.Token[:8])

	client := sync.NewClient(marker.BaseURL, marker.Token)

	wailsruntime.EventsEmit(a.ctx, "checkUpdates:status", "fetching_info")
	info, err := client.FetchInfo()
	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "fetch info failed for %s: %v", serverID, err)
	} else {
		marker.ExpiresAt = info.ExpiresAt
		_ = instance.WriteMarker(instanceDir, marker)
	}

	wailsruntime.EventsEmit(a.ctx, "checkUpdates:status", "fetching_manifest")
	manifest, err := client.FetchManifest()
	if err != nil {
		wailsruntime.LogErrorf(a.ctx, "fetch manifest failed: %v", err)
		return nil, err
	}
	wailsruntime.LogInfof(a.ctx, "manifest fetched: %d files", len(manifest.Files))

	wailsruntime.EventsEmit(a.ctx, "checkUpdates:status", "scanning_files")
	mcDir := "minecraft"
	localDir := filepath.Join(instanceDir, mcDir)
	var missing []sync.ManifestFile
	var outdated []sync.ManifestFile
	localFiles := make(map[string]bool)

	for _, mf := range manifest.Files {
		localPath := resolveMcPath(instanceDir, mf.Path)
		info, err := os.Stat(localPath)
		if err != nil {
			missing = append(missing, mf)
			continue
		}
		localFiles[normalizeMcPath(mf.Path)] = true
		if info.Size() != mf.Size {
			outdated = append(outdated, mf)
			continue
		}
		hash, err := sync.ComputeSHA256(localPath)
		if err != nil || hash != mf.SHA256 {
			outdated = append(outdated, mf)
		}
	}

	wailsruntime.EventsEmit(a.ctx, "checkUpdates:status", "scanning_orphan")
	var orphan []string
	for _, sub := range []string{"mods", "resourcepacks", "shaderpacks", "scripts", "kubejs", "defaultconfigs"} {
		subDir := filepath.Join(localDir, sub)
		entries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			relPath := filepath.ToSlash(filepath.Join(mcDir, sub, e.Name()))
			if !localFiles[relPath] {
				orphan = append(orphan, relPath)
			}
		}
	}

	wailsruntime.EventsEmit(a.ctx, "checkUpdates:status", "complete")
	result := map[string]interface{}{
		"missing":  missing,
		"outdated": outdated,
		"orphan":   orphan,
	}
	wailsruntime.LogInfof(a.ctx, "check complete: missing=%d outdated=%d orphan=%d", len(missing), len(outdated), len(orphan))

	marker.LastCheckAt = time.Now().UTC().Format(time.RFC3339)
	_ = instance.WriteMarker(instanceDir, marker)

	return result, nil
}

// CheckUpdates compares local files with the remote manifest.
func (a *App) CheckUpdates(serverID string) (map[string]interface{}, error) {
	return a.checkUpdatesInternal(serverID)
}

// ApplyUpdates downloads individual files and applies selected changes asynchronously.
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

	if !a.opRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("another operation is already in progress")
	}

	go func() {
		defer a.opRunning.Store(false)

		client := sync.NewClient(marker.BaseURL, marker.Token)

		wailsruntime.EventsEmit(a.ctx, "applyUpdates:status", "fetching manifest")
		manifest, err := client.FetchManifest()
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "applyUpdates:error", err.Error())
			return
		}

		manifestMap := make(map[string]sync.ManifestFile)
		for _, mf := range manifest.Files {
			manifestMap[mf.Path] = mf
		}

		wailsruntime.LogInfof(a.ctx, "ApplyUpdates selected=%d", len(selected))
		var toDownload []sync.ManifestFile
		var toDelete []string
		for _, p := range selected {
			if mf, ok := manifestMap[p]; ok {
				toDownload = append(toDownload, mf)
				wailsruntime.LogInfof(a.ctx, "toDownload: %s (%d bytes)", resolveMcPath(instanceDir, p), mf.Size)
			} else {
				toDelete = append(toDelete, p)
				wailsruntime.LogInfof(a.ctx, "toDelete: %s", resolveMcPath(instanceDir, p))
			}
		}

		backupDir := filepath.Join(instanceDir, ".minimin-backup")
		_ = os.RemoveAll(backupDir)

		for _, p := range selected {
			src := resolveMcPath(instanceDir, p)
			if _, err := os.Stat(src); err == nil {
				rel, _ := filepath.Rel(instanceDir, src)
				dst := filepath.Join(backupDir, rel)
				_ = os.MkdirAll(filepath.Dir(dst), 0o755)
				_ = os.Rename(src, dst)
			}
		}

		for _, p := range toDelete {
			target := resolveMcPath(instanceDir, p)
			_ = os.Remove(target)
		}

		var totalBytes int64
		for _, mf := range toDownload {
			totalBytes += mf.Size
		}

		if totalBytes > 0 {
			free, derr := disk.FreeBytes(instanceDir)
			if derr == nil && uint64(totalBytes) > free {
				_ = sync.RestoreBackup(backupDir, instanceDir)
				wailsruntime.EventsEmit(a.ctx, "applyUpdates:error", fmt.Sprintf("not enough disk space (need %d MB, have %d MB)", totalBytes/1024/1024, int64(free)/1024/1024))
				return
			}
		}

		const workers = 4
		type job struct {
			mf   sync.ManifestFile
			dest string
		}
		jobs := make(chan job, len(toDownload))
		for _, mf := range toDownload {
			dest := resolveMcPath(instanceDir, mf.Path)
			jobs <- job{mf: mf, dest: dest}
		}
		close(jobs)

		var downloaded atomic.Int64
		errChan := make(chan error, workers)

		for i := 0; i < workers; i++ {
			go func() {
				for j := range jobs {
					wailsruntime.LogInfof(a.ctx, "downloading %s", j.dest)
					wailsruntime.EventsEmit(a.ctx, "applyUpdates:status", fmt.Sprintf("downloading %s", filepath.Base(j.dest)))
					if err := client.DownloadFile(j.mf.Path, j.dest, nil); err != nil {
						wailsruntime.LogErrorf(a.ctx, "download failed %s: %v", j.dest, err)
						errChan <- err
						return
					}
					wailsruntime.LogInfof(a.ctx, "download ok %s", j.dest)
					d := downloaded.Add(j.mf.Size)
					wailsruntime.EventsEmit(a.ctx, "applyUpdates:progress", d, totalBytes)
				}
				errChan <- nil
			}()
		}

		var downloadErr error
		for i := 0; i < workers; i++ {
			if err := <-errChan; err != nil && downloadErr == nil {
				downloadErr = err
			}
		}

		if downloadErr != nil {
			_ = sync.RestoreBackup(backupDir, instanceDir)
			wailsruntime.EventsEmit(a.ctx, "applyUpdates:error", downloadErr.Error())
			return
		}

		wailsruntime.EventsEmit(a.ctx, "applyUpdates:status", "verifying files")
		for _, mf := range toDownload {
			dest := resolveMcPath(instanceDir, mf.Path)
			hash, err := sync.ComputeSHA256(dest)
			if err != nil || hash != mf.SHA256 {
				_ = sync.RestoreBackup(backupDir, instanceDir)
				wailsruntime.EventsEmit(a.ctx, "applyUpdates:error", fmt.Sprintf("hash mismatch for %s", dest))
				return
			}
		}

		marker.LastSyncAt = time.Now().UTC().Format(time.RFC3339)
		_ = instance.WriteMarker(instanceDir, marker)
		_ = os.RemoveAll(backupDir)

		wailsruntime.EventsEmit(a.ctx, "applyUpdates:done", serverID)
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

// resolveMcPath turns a manifest path into an absolute filesystem path.
// The server manifest uses ".minecraft/..." but Prism Launcher stores files in "minecraft/...".
func resolveMcPath(instanceDir string, manifestPath string) string {
	if strings.HasPrefix(manifestPath, ".minecraft/") {
		return filepath.Join(instanceDir, "minecraft", filepath.FromSlash(manifestPath[len(".minecraft/"):]))
	}
	return filepath.Join(instanceDir, filepath.FromSlash(manifestPath))
}

// normalizeMcPath adapts a manifest path for local key comparison.
func normalizeMcPath(manifestPath string) string {
	if strings.HasPrefix(manifestPath, ".minecraft/") {
		return "minecraft/" + manifestPath[len(".minecraft/"):]
	}
	return manifestPath
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

	if a.updateTmpPath != "" {
		_ = os.Remove(a.updateTmpPath)
	}

	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return err
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

	tmpFile := currentExe + ".tmp"
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
