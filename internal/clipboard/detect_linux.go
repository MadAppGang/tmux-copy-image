//go:build linux

package clipboard

import (
	"os"
	"os/exec"
)

func detectBackend() Backend {
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	display := os.Getenv("DISPLAY")

	// Wayland takes precedence when both are set.
	if waylandDisplay != "" {
		wlPastePath, err := exec.LookPath("wl-paste")
		if err == nil {
			return &waylandBackend{wlPastePath: wlPastePath}
		}
		return &noneBackend{reason: "wl-paste not found (install wl-clipboard)"}
	}

	if display != "" {
		xclipPath, err := exec.LookPath("xclip")
		if err == nil {
			return &x11Backend{xclipPath: xclipPath}
		}
		return &noneBackend{reason: "xclip not found (install xclip)"}
	}

	return &noneBackend{reason: "no display environment detected (DISPLAY and WAYLAND_DISPLAY are unset)"}
}
