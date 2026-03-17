//go:build !darwin && !linux

package clipboard

import "runtime"

func detectBackend() Backend {
	return &noneBackend{reason: "unsupported OS: " + runtime.GOOS}
}
