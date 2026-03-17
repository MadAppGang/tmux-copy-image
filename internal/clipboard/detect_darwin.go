//go:build darwin

package clipboard

import "os/exec"

func detectBackend() Backend {
	pngpastePath, pngpasteErr := exec.LookPath("pngpaste")
	osascriptPath, osascriptErr := exec.LookPath("osascript")

	switch {
	case pngpasteErr == nil:
		b := &macOSBackend{pngpastePath: pngpastePath}
		if osascriptErr == nil {
			b.osascriptPath = osascriptPath
		}
		return b
	case osascriptErr == nil:
		return &macOSBackend{osascriptPath: osascriptPath}
	default:
		return &noneBackend{reason: "no clipboard tool found (install pngpaste: brew install pngpaste)"}
	}
}
