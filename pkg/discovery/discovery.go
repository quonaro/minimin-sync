package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// standardPaths maps OS to known launcher instance directories.
var standardPaths = map[string][]string{
	"windows": {
		`%LOCALAPPDATA%\Programs\ElyPrismLauncher\instances`,
		`%LOCALAPPDATA%\Programs\PrismLauncher\instances`,
		`%LOCALAPPDATA%\Programs\MultiMC\instances`,
		`%APPDATA%\ElyPrismLauncher\instances`,
		`%APPDATA%\PrismLauncher\instances`,
		`%APPDATA%\MultiMC\instances`,
		`%USERPROFILE%\scoop\apps\prismlauncher\current\instances`,
	},
	"darwin": {
		`~/Library/Application Support/ElyPrismLauncher/instances`,
		`~/Library/Application Support/PrismLauncher/instances`,
		`~/Library/Application Support/MultiMC/instances`,
	},
	"linux": {
		`~/.local/share/ElyPrismLauncher/instances`,
		`~/.local/share/PrismLauncher/instances`,
		`~/.var/app/org.prismlauncher.PrismLauncher/data/PrismLauncher/instances`,
		`~/snap/prismlauncher/current/.local/share/PrismLauncher/instances`,
		`~/.local/share/MultiMC/instances`,
	},
}

// FindInstancesDir searches for a valid launcher instances directory.
func FindInstancesDir() (string, error) {
	paths := standardPaths[runtime.GOOS]
	for _, p := range paths {
		p = expandPath(p)
		if isValidInstancesDir(p) {
			return filepath.ToSlash(p), nil
		}
	}
	return "", os.ErrNotExist
}

// FindAllLaunchers returns every valid launcher instances directory found.
func FindAllLaunchers() []string {
	paths := standardPaths[runtime.GOOS]
	var results []string
	for _, p := range paths {
		p = expandPath(p)
		if isValidInstancesDir(p) {
			results = append(results, filepath.ToSlash(p))
		}
	}
	return results
}

// isValidInstancesDir checks whether dir exists and is a directory.
func isValidInstancesDir(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// expandPath expands environment variables and home directory.
func expandPath(path string) string {
	if runtime.GOOS == "windows" {
		path = expandWindowsEnv(path)
	} else {
		path = os.ExpandEnv(path)
	}
	return expandHome(path)
}

// expandWindowsEnv replaces %VAR% syntax with os.Getenv values.
// Go's os.ExpandEnv only understands $VAR, not %VAR%.
func expandWindowsEnv(path string) string {
	path = strings.ReplaceAll(path, "%LOCALAPPDATA%", os.Getenv("LOCALAPPDATA"))
	path = strings.ReplaceAll(path, "%APPDATA%", os.Getenv("APPDATA"))
	path = strings.ReplaceAll(path, "%USERPROFILE%", os.Getenv("USERPROFILE"))
	path = strings.ReplaceAll(path, "%PROGRAMFILES%", os.Getenv("ProgramFiles"))
	path = strings.ReplaceAll(path, "%PROGRAMFILES(x86)%", os.Getenv("ProgramFiles(x86)"))
	return path
}

// expandHome replaces leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) < 2 || path[0] != '~' {
		return path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, path[1:])
}
