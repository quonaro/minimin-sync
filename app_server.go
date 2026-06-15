package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"minimin-sync/pkg/instance"
	syncpkg "minimin-sync/pkg/sync"
)

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
func (a *App) PreviewServer(url string) (syncpkg.InfoResponse, error) {
	if !strings.Contains(url, "?format=") {
		url = url + "?format=prism"
	}
	token, baseURL, err := syncpkg.ParseArchiveURL(url)
	if err != nil {
		return syncpkg.InfoResponse{}, err
	}
	client := syncpkg.NewClient(baseURL, token)
	info, err := client.FetchInfo()
	if err != nil {
		return syncpkg.InfoResponse{}, err
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

	client := syncpkg.NewClient(marker.BaseURL, marker.Token)
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
	token, baseURL, err := syncpkg.ParseArchiveURL(url)
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

	client := syncpkg.NewClient(baseURL, token)
	info, err := client.FetchInfo()
	if err != nil {
		return err
	}

	marker.Token = token
	marker.BaseURL = baseURL
	marker.ExpiresAt = info.ExpiresAt
	return instance.WriteMarker(instanceDir, marker)
}
