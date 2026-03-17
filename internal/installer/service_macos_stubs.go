//go:build !darwin

package installer

import "fmt"

// installLaunchdService stub for non-macOS platforms.
func installLaunchdService(_ Config) error {
	return fmt.Errorf("launchd not available on this platform")
}

// loadLaunchdService stub for non-macOS platforms.
func loadLaunchdService() error {
	return fmt.Errorf("launchd not available on this platform")
}

// unloadLaunchdService stub for non-macOS platforms.
func unloadLaunchdService() error {
	return fmt.Errorf("launchd not available on this platform")
}

// removeLaunchdService stub for non-macOS platforms.
func removeLaunchdService() error {
	return fmt.Errorf("launchd not available on this platform")
}
