package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"minimin-sync/pkg/discovery"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

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
