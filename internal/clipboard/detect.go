// Package clipboard provides the Backend interface and implementations for
// reading image data from the system clipboard on various platforms.
package clipboard

import "os/exec"

// DetectBackend inspects the current OS and environment to select the
// appropriate clipboard backend. It is called once at daemon startup.
// The actual implementation is in detect_darwin.go / detect_linux.go / detect_other.go.
func DetectBackend() Backend {
	return detectBackend()
}

// isInPath reports whether the named executable is in PATH.
func isInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
