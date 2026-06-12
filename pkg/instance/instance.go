package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// MarkerFile is the name of the minimin marker inside a Prism instance.
const MarkerFile = ".minimin.json"

// Marker holds metadata for a synced instance.
type Marker struct {
	ServerID    string `json:"serverId"`
	Token       string `json:"token"`
	BaseURL     string `json:"baseUrl"`
	LastSyncAt  string `json:"lastSyncAt"`
	LastCheckAt string `json:"lastCheckAt"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

// ReadMarker reads .minimin.json from an instance directory.
func ReadMarker(instanceDir string) (*Marker, error) {
	path := filepath.Join(instanceDir, MarkerFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// WriteMarker writes .minimin.json into an instance directory.
func WriteMarker(instanceDir string, m *Marker) error {
	path := filepath.Join(instanceDir, MarkerFile)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ScannedInstance represents a discovered synced instance.
type ScannedInstance struct {
	Dir    string
	Name   string
	Marker *Marker
}

// Scan finds all directories inside instancesDir that contain .minimin.json.
func Scan(instancesDir string) ([]ScannedInstance, error) {
	entries, err := os.ReadDir(instancesDir)
	if err != nil {
		return []ScannedInstance{}, err
	}
	var results []ScannedInstance
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(instancesDir, e.Name())
		m, err := ReadMarker(dir)
		if err != nil {
			continue
		}
		results = append(results, ScannedInstance{
			Dir:    dir,
			Name:   e.Name(),
			Marker: m,
		})
	}
	return results, nil
}
