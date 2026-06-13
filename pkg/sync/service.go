package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"minimin-sync/pkg/disk"
	"minimin-sync/pkg/instance"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func resolveMcPath(instanceDir string, manifestPath string) string {
	if strings.HasPrefix(manifestPath, ".minecraft/") {
		return filepath.Join(instanceDir, "minecraft", filepath.FromSlash(manifestPath[len(".minecraft/"):]))
	}
	return filepath.Join(instanceDir, filepath.FromSlash(manifestPath))
}

func normalizeMcPath(manifestPath string) string {
	if strings.HasPrefix(manifestPath, ".minecraft/") {
		return "minecraft/" + manifestPath[len(".minecraft/"):]
	}
	return manifestPath
}

// Service coordinates sync operations for a single instances directory.
type Service struct {
	ctx          context.Context
	instancesDir string
	opRunning    atomic.Bool
}

// NewService creates a sync service.
func NewService(ctx context.Context, instancesDir string) *Service {
	return &Service{ctx: ctx, instancesDir: instancesDir}
}

// IsOperationRunning reports whether AddServer or ApplyUpdates is active.
func (s *Service) IsOperationRunning() bool {
	return s.opRunning.Load()
}

// CheckUpdates compares local files with the remote manifest for a server.
func (s *Service) CheckUpdates(serverID string) (map[string]interface{}, error) {
	instanceDir := filepath.Join(s.instancesDir, serverID)
	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		wailsruntime.LogErrorf(s.ctx, "read marker failed: %v", err)
		return nil, err
	}
	wailsruntime.LogInfof(s.ctx, "marker read: baseURL=%s token=%s...", marker.BaseURL, marker.Token[:8])

	client := NewClient(marker.BaseURL, marker.Token)

	info, err := client.FetchInfo()
	if err != nil {
		wailsruntime.LogErrorf(s.ctx, "fetch info failed for %s: %v", serverID, err)
	} else {
		marker.ExpiresAt = info.ExpiresAt
		_ = instance.WriteMarker(instanceDir, marker)
	}

	manifest, err := client.FetchManifest()
	if err != nil {
		wailsruntime.LogErrorf(s.ctx, "fetch manifest failed: %v", err)
		return nil, err
	}
	wailsruntime.LogInfof(s.ctx, "manifest fetched: %d files", len(manifest.Files))

	mcDir := "minecraft"
	localDir := filepath.Join(instanceDir, mcDir)
	var missing []ManifestFile
	var outdated []ManifestFile
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
		hash, err := ComputeSHA256(localPath)
		if err != nil || hash != mf.SHA256 {
			outdated = append(outdated, mf)
		}
	}

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

	result := map[string]interface{}{
		"missing":  missing,
		"outdated": outdated,
		"orphan":   orphan,
	}
	wailsruntime.LogInfof(s.ctx, "check complete: missing=%d outdated=%d orphan=%d", len(missing), len(outdated), len(orphan))

	marker.LastCheckAt = time.Now().UTC().Format(time.RFC3339)
	_ = instance.WriteMarker(instanceDir, marker)

	return result, nil
}

// AddServer installs a new server from a prism archive URL asynchronously.
func (s *Service) AddServer(url string) error {
	if !s.opRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("another operation is already in progress")
	}

	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := ParseArchiveURL(url)
	if err != nil {
		s.opRunning.Store(false)
		return err
	}

	go func() {
		defer s.opRunning.Store(false)
		client := NewClient(baseURL, token)

		wailsruntime.EventsEmit(s.ctx, "addServer:status", "fetching info")
		info, err := client.FetchInfo()
		if err != nil {
			wailsruntime.EventsEmit(s.ctx, "addServer:error", err.Error())
			return
		}

		name := sanitizeName(info.ServerName)
		if name == "" {
			wailsruntime.EventsEmit(s.ctx, "addServer:error", "server name is empty")
			return
		}
		name = s.uniqueInstanceName(name)

		wailsruntime.EventsEmit(s.ctx, "addServer:status", "downloading archive")
		size, err := client.ArchiveSize("prism")
		if err == nil && size > 0 {
			free, derr := disk.FreeBytes(s.instancesDir)
			if derr == nil && uint64(size) > free {
				wailsruntime.EventsEmit(s.ctx, "addServer:error", fmt.Sprintf("not enough disk space (need %d MB, have %d MB)", size/1024/1024, int64(free)/1024/1024))
				return
			}
		}

		tmpPath, err := client.DownloadArchive("prism", func(d, t int64) {
			wailsruntime.EventsEmit(s.ctx, "addServer:progress", d, t)
		})
		if err != nil {
			wailsruntime.EventsEmit(s.ctx, "addServer:error", err.Error())
			return
		}
		defer func() { _ = os.Remove(tmpPath) }()

		instanceDir := filepath.Join(s.instancesDir, name)
		if err := os.MkdirAll(instanceDir, 0o755); err != nil {
			wailsruntime.EventsEmit(s.ctx, "addServer:error", err.Error())
			return
		}
		cleanOnFail := true
		defer func() {
			if cleanOnFail {
				_ = os.RemoveAll(instanceDir)
			}
		}()

		wailsruntime.EventsEmit(s.ctx, "addServer:status", "extracting")
		if err := ExtractAll(tmpPath, instanceDir); err != nil {
			wailsruntime.EventsEmit(s.ctx, "addServer:error", err.Error())
			return
		}

		marker := &instance.Marker{
			ServerID:   name,
			Token:      token,
			BaseURL:    baseURL,
			LastSyncAt: time.Now().UTC().Format(time.RFC3339),
			ExpiresAt:  info.ExpiresAt,
		}
		wailsruntime.LogInfof(s.ctx, "writing marker to %s", filepath.Join(instanceDir, instance.MarkerFile))
		if err := instance.WriteMarker(instanceDir, marker); err != nil {
			wailsruntime.EventsEmit(s.ctx, "addServer:error", err.Error())
			return
		}

		cleanOnFail = false
		wailsruntime.EventsEmit(s.ctx, "addServer:done", info.ServerName)
	}()

	return nil
}

// ApplyUpdates downloads individual files and applies selected changes asynchronously.
func (s *Service) ApplyUpdates(serverID string, selected []string) error {
	instanceDir := filepath.Join(s.instancesDir, serverID)
	marker, err := instance.ReadMarker(instanceDir)
	if err != nil {
		return err
	}

	if !s.opRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("another operation is already in progress")
	}

	go func() {
		defer s.opRunning.Store(false)

		client := NewClient(marker.BaseURL, marker.Token)

		wailsruntime.EventsEmit(s.ctx, "applyUpdates:status", "fetching manifest")
		manifest, err := client.FetchManifest()
		if err != nil {
			wailsruntime.EventsEmit(s.ctx, "applyUpdates:error", err.Error())
			return
		}

		manifestMap := make(map[string]ManifestFile)
		for _, mf := range manifest.Files {
			manifestMap[mf.Path] = mf
		}

		wailsruntime.LogInfof(s.ctx, "ApplyUpdates selected=%d", len(selected))
		var toDownload []ManifestFile
		var toDelete []string
		for _, p := range selected {
			if mf, ok := manifestMap[p]; ok {
				toDownload = append(toDownload, mf)
				wailsruntime.LogInfof(s.ctx, "toDownload: %s (%d bytes)", resolveMcPath(instanceDir, p), mf.Size)
			} else {
				toDelete = append(toDelete, p)
				wailsruntime.LogInfof(s.ctx, "toDelete: %s", resolveMcPath(instanceDir, p))
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
				_ = RestoreBackup(backupDir, instanceDir)
				wailsruntime.EventsEmit(s.ctx, "applyUpdates:error", fmt.Sprintf("not enough disk space (need %d MB, have %d MB)", totalBytes/1024/1024, int64(free)/1024/1024))
				return
			}
		}

		const workers = 4
		type job struct {
			mf   ManifestFile
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
					wailsruntime.LogInfof(s.ctx, "downloading %s", j.dest)
					wailsruntime.EventsEmit(s.ctx, "applyUpdates:status", fmt.Sprintf("downloading %s", filepath.Base(j.dest)))
					if err := client.DownloadFile(j.mf.Path, j.dest, nil); err != nil {
						wailsruntime.LogErrorf(s.ctx, "download failed %s: %v", j.dest, err)
						errChan <- err
						return
					}
					wailsruntime.LogInfof(s.ctx, "download ok %s", j.dest)
					d := downloaded.Add(j.mf.Size)
					wailsruntime.EventsEmit(s.ctx, "applyUpdates:progress", d, totalBytes)
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
			_ = RestoreBackup(backupDir, instanceDir)
			wailsruntime.EventsEmit(s.ctx, "applyUpdates:error", downloadErr.Error())
			return
		}

		wailsruntime.EventsEmit(s.ctx, "applyUpdates:status", "verifying files")
		for _, mf := range toDownload {
			dest := resolveMcPath(instanceDir, mf.Path)
			hash, err := ComputeSHA256(dest)
			if err != nil || hash != mf.SHA256 {
				_ = RestoreBackup(backupDir, instanceDir)
				wailsruntime.EventsEmit(s.ctx, "applyUpdates:error", fmt.Sprintf("hash mismatch for %s", dest))
				return
			}
		}

		marker.LastSyncAt = time.Now().UTC().Format(time.RFC3339)
		_ = instance.WriteMarker(instanceDir, marker)
		_ = os.RemoveAll(backupDir)

		wailsruntime.EventsEmit(s.ctx, "applyUpdates:done", serverID)
	}()

	return nil
}

func (s *Service) uniqueInstanceName(base string) string {
	name := base
	for i := 1; ; i++ {
		candidate := filepath.Join(s.instancesDir, name)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
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

func ParseArchiveURL(rawURL string) (token, baseURL string, err error) {
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
