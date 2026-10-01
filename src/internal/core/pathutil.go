// Package core provides business logic and utility functions
package core

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// defaultAllowedRoots derives from HOME rather than hardcoding a user path.
var defaultAllowedRoots = func() []string {
	home := os.Getenv("HOME")
	return []string{home, "/code", "/vault"}
}()

var (
	allowedRootsOnce sync.Once
	allowedRoots     []string
)

// GetAllowedRoots returns the configured allowed roots
// Reads from CHROTE_ROOTS env var, defaults to HOME,/code,/vault
func GetAllowedRoots() []string {
	allowedRootsOnce.Do(func() {
		if roots := os.Getenv("CHROTE_ROOTS"); roots != "" {
			allowedRoots = normalizeRoots(strings.Split(roots, ","))
		} else {
			allowedRoots = normalizeRoots(defaultAllowedRoots)
		}
	})
	return allowedRoots
}

func normalizeRoots(parts []string) []string {
	roots := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		absRoot, err := filepath.Abs(part)
		if err != nil {
			continue
		}
		absRoot = filepath.Clean(absRoot)
		if absRoot == string(os.PathSeparator) {
			return []string{absRoot}
		}
		if seen[absRoot] {
			continue
		}
		seen[absRoot] = true
		roots = append(roots, absRoot)
	}
	return roots
}

// ResetConfigForTesting resets the cached config (for testing only)
func ResetConfigForTesting() {
	allowedRootsOnce = sync.Once{}
	allowedRoots = nil
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetWorkDir returns the default working directory for new sessions
// Reads from CHROTE_WORKDIR env var, defaults to first allowed root
func GetWorkDir() string {
	if workdir := os.Getenv("CHROTE_WORKDIR"); workdir != "" {
		return workdir
	}
	roots := GetAllowedRoots()
	if len(roots) > 0 {
		return roots[0]
	}
	return "/code"
}

// GetLaunchScript returns the terminal launch script path
// Reads from CHROTE_LAUNCH_SCRIPT env var, defaults to /usr/local/bin/terminal-launch.sh
func GetLaunchScript() string {
	if script := os.Getenv("CHROTE_LAUNCH_SCRIPT"); script != "" {
		return script
	}
	return "/usr/local/bin/terminal-launch.sh"
}

// GetBvLaunchScript returns the beads viewer launch script path
// Reads from CHROTE_BV_LAUNCH_SCRIPT env var, defaults to /usr/local/bin/bv-launch.sh
func GetBvLaunchScript() string {
	if script := os.Getenv("CHROTE_BV_LAUNCH_SCRIPT"); script != "" {
		return script
	}
	return "/usr/local/bin/bv-launch.sh"
}
