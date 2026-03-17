//go:build !linux

package installer

import "fmt"

// installSystemdService stub for non-Linux platforms.
func installSystemdService(_ Config) error {
	return fmt.Errorf("systemd not available on this platform")
}

// enableSystemdService stub for non-Linux platforms.
func enableSystemdService() error {
	return fmt.Errorf("systemd not available on this platform")
}

// disableSystemdService stub for non-Linux platforms.
func disableSystemdService() error {
	return fmt.Errorf("systemd not available on this platform")
}

// removeSystemdService stub for non-Linux platforms.
func removeSystemdService() error {
	return fmt.Errorf("systemd not available on this platform")
}
