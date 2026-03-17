// Package installer handles installation and removal of rpaster on local and
// remote machines. Local operations manage binary placement and service units.
// Remote operations push the embedded tmux plugin over SSH/SCP.
package installer

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Config holds parameters for install/uninstall operations.
type Config struct {
	// BinaryPath is where rpaster should be installed (default: ~/.local/bin/rpaster).
	BinaryPath string

	// Port is the daemon port to embed in the service unit (default: 18339).
	Port int

	// Token is the bearer token to embed in the service unit (optional).
	Token string

	// RemoteHost is the SSH target for remote operations (empty = local-only).
	RemoteHost string

	// PluginDir is the remote plugin directory (default: ~/.tmux/plugins/tmux-clip-image).
	PluginDir string

	// ModifySSHConf controls whether ~/.ssh/config is modified to add RemoteForward.
	ModifySSHConf bool

	// DryRun prints what would happen without making any changes.
	DryRun bool

	// PluginFS is the embedded filesystem containing the plugin files.
	// Must be set for RunRemote.
	PluginFS fs.FS
}

// defaults fills in missing Config fields.
func (cfg *Config) defaults() error {
	if cfg.Port == 0 {
		cfg.Port = 18339
	}
	if cfg.BinaryPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		cfg.BinaryPath = filepath.Join(home, ".local", "bin", "rpaster")
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = "~/.tmux/plugins/tmux-clip-image"
	}
	return nil
}

// RunLocal performs the full local installation:
//  1. Copies the current binary to cfg.BinaryPath.
//  2. Creates the platform-specific service unit.
//  3. Loads/enables the service.
//  4. Suggests clipboard backend installation if missing.
func RunLocal(cfg Config) error {
	if err := cfg.defaults(); err != nil {
		return err
	}

	steps := []func() error{
		func() error { return installBinary(cfg) },
		func() error { return installService(cfg) },
		func() error { return loadService(cfg) },
		func() error { return suggestBackend(cfg) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall reverses all local installation steps. If cfg.RemoteHost is set,
// also removes the remote plugin.
func Uninstall(cfg Config) error {
	if err := cfg.defaults(); err != nil {
		return err
	}

	if cfg.DryRun {
		fmt.Printf("[dry-run] would unload and remove service unit\n")
		fmt.Printf("[dry-run] would remove binary at %s\n", cfg.BinaryPath)
		if cfg.RemoteHost != "" {
			fmt.Printf("[dry-run] would remove plugin from %s on %s\n", cfg.PluginDir, cfg.RemoteHost)
		}
		if cfg.ModifySSHConf {
			fmt.Printf("[dry-run] would remove RemoteForward from ~/.ssh/config for host %s\n", cfg.RemoteHost)
		}
		return nil
	}

	// Stop and unload service first.
	if err := unloadService(cfg); err != nil {
		// Non-fatal: the service might not be loaded.
		fmt.Printf("warning: unload service: %v\n", err)
	}

	// Remove service unit file.
	if err := removeService(cfg); err != nil {
		fmt.Printf("warning: remove service unit: %v\n", err)
	}

	// Remove binary.
	if err := removeBinary(cfg); err != nil {
		fmt.Printf("warning: remove binary: %v\n", err)
	}

	// Remote cleanup.
	if cfg.RemoteHost != "" {
		if err := UninstallRemote(cfg); err != nil {
			fmt.Printf("warning: remove remote plugin: %v\n", err)
		}
	}

	// SSH config cleanup.
	if cfg.ModifySSHConf && cfg.RemoteHost != "" {
		home, _ := os.UserHomeDir()
		sshConfigPath := filepath.Join(home, ".ssh", "config")
		if err := RemoveRemoteForward(cfg.RemoteHost, sshConfigPath); err != nil {
			fmt.Printf("warning: remove SSH config entry: %v\n", err)
		}
	}

	return nil
}

// installBinary copies the running binary to the target path.
func installBinary(cfg Config) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	// Resolve symlinks.
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		real = self
	}

	if cfg.DryRun {
		fmt.Printf("[dry-run] would copy %s -> %s\n", real, cfg.BinaryPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(cfg.BinaryPath), 0755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	if err := copyFile(real, cfg.BinaryPath, 0755); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	fmt.Printf("installed binary: %s\n", cfg.BinaryPath)
	return nil
}

// removeBinary deletes the installed binary.
func removeBinary(cfg Config) error {
	if _, err := os.Stat(cfg.BinaryPath); os.IsNotExist(err) {
		return nil
	}
	if err := os.Remove(cfg.BinaryPath); err != nil {
		return fmt.Errorf("remove %s: %w", cfg.BinaryPath, err)
	}
	fmt.Printf("removed binary: %s\n", cfg.BinaryPath)
	return nil
}

// installService dispatches to the platform-specific service unit creator.
func installService(cfg Config) error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchdService(cfg)
	case "linux":
		return installSystemdService(cfg)
	default:
		fmt.Printf("warning: service unit installation not supported on %s\n", runtime.GOOS)
		return nil
	}
}

// loadService activates the service unit after creation.
func loadService(cfg Config) error {
	if cfg.DryRun {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return loadLaunchdService()
	case "linux":
		return enableSystemdService()
	default:
		return nil
	}
}

// unloadService deactivates the service unit before removal.
func unloadService(_ Config) error {
	switch runtime.GOOS {
	case "darwin":
		return unloadLaunchdService()
	case "linux":
		return disableSystemdService()
	default:
		return nil
	}
}

// removeService deletes the service unit file.
func removeService(_ Config) error {
	switch runtime.GOOS {
	case "darwin":
		return removeLaunchdService()
	case "linux":
		return removeSystemdService()
	default:
		return nil
	}
}

// suggestBackend prints a hint if no clipboard backend is in PATH.
func suggestBackend(_ Config) error {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("pngpaste"); err != nil {
			fmt.Printf("hint: pngpaste not found — install for best clipboard support: brew install pngpaste\n")
		}
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if _, err := exec.LookPath("wl-paste"); err != nil {
				fmt.Printf("hint: wl-paste not found — install: sudo apt install wl-clipboard\n")
			}
		} else if os.Getenv("DISPLAY") != "" {
			if _, err := exec.LookPath("xclip"); err != nil {
				fmt.Printf("hint: xclip not found — install: sudo apt install xclip\n")
			}
		}
	}
	return nil
}

// copyFile copies src to dst atomically with the given mode.
// Writes to a temp file alongside dst then renames to prevent partial writes.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	// Write to a temp file in the same directory, then rename.
	tmpPath := dst + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
