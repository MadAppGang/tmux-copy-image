package installer

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunRemote installs the tmux plugin on the remote host via SSH/SCP and
// optionally updates the remote ~/.tmux.conf with a managed TPM block.
//
// Steps:
//  1. Verify remote tmux version (abort if < 3.0).
//  2. Create remote plugin directory.
//  3. Extract embedded plugin files via scp.
//  4. Write token file to ~/.config/clip-serve/token (mode 0600) if token is set.
//  5. Detect TPM and update remote ~/.tmux.conf with managed markers.
//  6. Reload tmux config if a session is running.
func RunRemote(cfg Config) error {
	if err := cfg.defaults(); err != nil {
		return err
	}

	if cfg.PluginFS == nil {
		return fmt.Errorf("PluginFS is required for remote installation")
	}

	steps := []func() error{
		func() error { return checkRemoteTmuxVersion(cfg) },
		func() error { return createRemotePluginDir(cfg) },
		func() error { return copyPluginFiles(cfg) },
		func() error { return writeRemoteToken(cfg) },
		func() error { return updateRemoteTmuxConf(cfg) },
		func() error { return reloadRemoteTmux(cfg) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// UninstallRemote removes the plugin from the remote host.
func UninstallRemote(cfg Config) error {
	if err := cfg.defaults(); err != nil {
		return err
	}

	if cfg.DryRun {
		fmt.Printf("[dry-run] would remove %s from %s\n", cfg.PluginDir, cfg.RemoteHost)
		return nil
	}

	// Remove the plugin directory on the remote.
	pluginDir := expandRemoteTilde(cfg.PluginDir)
	_, err := runRemoteCommand(cfg.RemoteHost,
		fmt.Sprintf("rm -rf %s", shellQuote(pluginDir)))
	if err != nil {
		return fmt.Errorf("remove remote plugin dir: %w", err)
	}
	fmt.Printf("removed remote plugin dir: %s on %s\n", cfg.PluginDir, cfg.RemoteHost)

	// Remove token file if it exists.
	_, _ = runRemoteCommand(cfg.RemoteHost,
		"rm -f ~/.config/clip-serve/token")

	// Remove managed TPM block from ~/.tmux.conf.
	if err := removeTmuxConfBlock(cfg); err != nil {
		fmt.Printf("warning: remove tmux.conf block: %v\n", err)
	}

	return nil
}

// checkRemoteTmuxVersion aborts if remote tmux is older than 3.0.
func checkRemoteTmuxVersion(cfg Config) error {
	if cfg.DryRun {
		fmt.Printf("[dry-run] would check remote tmux version on %s\n", cfg.RemoteHost)
		return nil
	}
	out, err := runRemoteCommand(cfg.RemoteHost, "tmux -V")
	if err != nil {
		return fmt.Errorf("remote tmux not found on %s: %w", cfg.RemoteHost, err)
	}

	// Parse "tmux X.Y" from output.
	version := strings.TrimSpace(string(out))
	var major, minor int
	if _, err := fmt.Sscanf(version, "tmux %d.%d", &major, &minor); err != nil {
		// Try integer-only version (e.g., "tmux 3").
		if _, err2 := fmt.Sscanf(version, "tmux %d", &major); err2 != nil {
			return fmt.Errorf("cannot parse remote tmux version %q", version)
		}
	}

	if major < 3 {
		return fmt.Errorf("remote tmux version %s is too old (minimum 3.0 required)", version)
	}
	fmt.Printf("remote tmux version: %s (OK)\n", version)
	return nil
}

// createRemotePluginDir creates the plugin directory on the remote host.
func createRemotePluginDir(cfg Config) error {
	pluginDir := expandRemoteTilde(cfg.PluginDir)
	if cfg.DryRun {
		fmt.Printf("[dry-run] would create %s on %s\n", cfg.PluginDir, cfg.RemoteHost)
		return nil
	}
	_, err := runRemoteCommand(cfg.RemoteHost,
		fmt.Sprintf("mkdir -p %s && chmod 700 %s", shellQuote(pluginDir), shellQuote(pluginDir)))
	if err != nil {
		return fmt.Errorf("create remote plugin dir: %w", err)
	}
	return nil
}

// copyPluginFiles extracts the embedded plugin files and transfers them to the
// remote host via scp. Each file is written to a local temp file, scp'd, then
// deleted. Uses system ssh/scp to honour SSH config (SYNTH-10).
func copyPluginFiles(cfg Config) error {
	pluginDir := expandRemoteTilde(cfg.PluginDir)

	if cfg.DryRun {
		fmt.Printf("[dry-run] would scp plugin files to %s:%s\n", cfg.RemoteHost, cfg.PluginDir)
		return nil
	}

	// Walk the embedded PluginFS under the "plugin" prefix.
	return fs.WalkDir(cfg.PluginFS, "plugin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the leading "plugin/" prefix to get the relative path within the plugin.
		rel, err := filepath.Rel("plugin", path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			if rel == "." {
				return nil
			}
			// Create subdirectory on remote.
			remoteDir := pluginDir + "/" + rel
			_, e := runRemoteCommand(cfg.RemoteHost,
				fmt.Sprintf("mkdir -p %s", shellQuote(remoteDir)))
			return e
		}

		// Read file from embedded FS.
		data, err := fs.ReadFile(cfg.PluginFS, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}

		// Write to a local temp file.
		tmp, err := os.CreateTemp("", "clip-serve-plugin-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer os.Remove(tmp.Name())

		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return fmt.Errorf("write temp %s: %w", path, err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("close temp %s: %w", path, err)
		}

		// Determine remote path.
		remotePath := pluginDir + "/" + filepath.ToSlash(rel)

		// SCP the file.
		scpDst := fmt.Sprintf("%s:%s", cfg.RemoteHost, remotePath)
		out, err := exec.Command("scp", "-q", tmp.Name(), scpDst).CombinedOutput()
		if err != nil {
			return fmt.Errorf("scp %s -> %s: %w: %s", path, scpDst, err, out)
		}

		// Make .tmux files executable.
		if strings.HasSuffix(path, ".tmux") || strings.HasSuffix(path, ".sh") {
			_, e := runRemoteCommand(cfg.RemoteHost,
				fmt.Sprintf("chmod +x %s", shellQuote(remotePath)))
			if e != nil {
				return fmt.Errorf("chmod +x %s: %w", remotePath, e)
			}
		}

		return nil
	})
}

// writeRemoteToken writes the bearer token to ~/.config/clip-serve/token on
// the remote host with mode 0600. If no token is configured, this is a no-op.
func writeRemoteToken(cfg Config) error {
	if cfg.Token == "" {
		return nil
	}
	if cfg.DryRun {
		fmt.Printf("[dry-run] would write token to ~/.config/clip-serve/token on %s\n", cfg.RemoteHost)
		return nil
	}
	_, err := runRemoteCommand(cfg.RemoteHost, strings.Join([]string{
		"mkdir -p ~/.config/clip-serve &&",
		fmt.Sprintf("printf '%%s' %s > ~/.config/clip-serve/token &&", shellQuote(cfg.Token)),
		"chmod 600 ~/.config/clip-serve/token",
	}, " "))
	if err != nil {
		return fmt.Errorf("write remote token: %w", err)
	}
	return nil
}

const (
	tmuxConfBeginFmt = "# --- clip-serve BEGIN ---"
	tmuxConfEnd      = "# --- clip-serve END ---"
)

// tpmBlock generates the TPM managed block for ~/.tmux.conf.
func tpmBlock(pluginDir string) string {
	return fmt.Sprintf("%s\nset -g @plugin %s\n%s\n",
		tmuxConfBeginFmt,
		shellQuote(pluginDir),
		tmuxConfEnd,
	)
}

// updateRemoteTmuxConf adds a managed TPM plugin entry to ~/.tmux.conf on the
// remote host if TPM is detected. If an entry already exists, it is replaced.
func updateRemoteTmuxConf(cfg Config) error {
	if cfg.DryRun {
		fmt.Printf("[dry-run] would update ~/.tmux.conf on %s\n", cfg.RemoteHost)
		return nil
	}

	pluginDir := cfg.PluginDir // keep tilde for display

	// Check if TPM is already configured (look for tpm in tmux.conf).
	out, _ := runRemoteCommand(cfg.RemoteHost, "cat ~/.tmux.conf 2>/dev/null || true")
	existing := string(out)

	// Check if managed block already exists.
	if strings.Contains(existing, tmuxConfBeginFmt) {
		// Replace existing managed block.
		newContent := replaceRemoteTmuxBlock(existing, pluginDir)
		return writeRemoteTmuxConf(cfg.RemoteHost, newContent)
	}

	// Check if TPM is configured at all (only add plugin entry if so).
	if !strings.Contains(existing, "tpm") {
		fmt.Printf("hint: TPM not detected on %s — add to ~/.tmux.conf manually:\n  set -g @plugin %s\n",
			cfg.RemoteHost, shellQuote(pluginDir))
		return nil
	}

	// Append managed block.
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	newContent := existing + "\n" + tpmBlock(pluginDir)
	return writeRemoteTmuxConf(cfg.RemoteHost, newContent)
}

// removeTmuxConfBlock removes the managed clip-serve block from remote ~/.tmux.conf.
func removeTmuxConfBlock(cfg Config) error {
	out, _ := runRemoteCommand(cfg.RemoteHost, "cat ~/.tmux.conf 2>/dev/null || true")
	existing := string(out)
	if !strings.Contains(existing, tmuxConfBeginFmt) {
		return nil
	}
	newContent := stripRemoteTmuxBlock(existing)
	return writeRemoteTmuxConf(cfg.RemoteHost, newContent)
}

// replaceRemoteTmuxBlock replaces the managed block in existing tmux.conf content.
func replaceRemoteTmuxBlock(content, pluginDir string) string {
	var result strings.Builder
	inBlock := false
	blockWritten := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, tmuxConfBeginFmt) {
			inBlock = true
			if !blockWritten {
				result.WriteString(tpmBlock(pluginDir))
				blockWritten = true
			}
			continue
		}
		if inBlock {
			if strings.Contains(line, tmuxConfEnd) {
				inBlock = false
			}
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

// stripRemoteTmuxBlock removes the managed block from existing tmux.conf content.
func stripRemoteTmuxBlock(content string) string {
	var result strings.Builder
	inBlock := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, tmuxConfBeginFmt) {
			inBlock = true
			continue
		}
		if inBlock {
			if strings.Contains(line, tmuxConfEnd) {
				inBlock = false
			}
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

// writeRemoteTmuxConf overwrites remote ~/.tmux.conf via ssh heredoc.
func writeRemoteTmuxConf(host, content string) error {
	// Use printf to write the file — safer than heredoc for arbitrary content.
	_, err := runRemoteCommand(host,
		fmt.Sprintf("printf '%%s' %s > ~/.tmux.conf", shellQuote(content)))
	return err
}

// reloadRemoteTmux reloads the tmux config if a session is running on the remote.
func reloadRemoteTmux(cfg Config) error {
	if cfg.DryRun {
		fmt.Printf("[dry-run] would reload tmux config on %s if running\n", cfg.RemoteHost)
		return nil
	}
	// Non-fatal: if no tmux session, this exits non-zero and we ignore it.
	_, _ = runRemoteCommand(cfg.RemoteHost,
		"tmux source-file ~/.tmux.conf 2>/dev/null || true")
	return nil
}

// runRemoteCommand executes a shell command on the remote host via SSH with
// BatchMode enabled to prevent interactive prompts. Uses the system ssh binary
// to honour all SSH config settings (SYNTH-10).
func runRemoteCommand(host, command string) ([]byte, error) {
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=30",
		host,
		command,
	)
	return cmd.Output()
}

// expandRemoteTilde replaces a leading "~" with "$HOME" for remote shell expansion.
// The tilde is intentionally not expanded locally — it will be expanded by the
// remote shell when passed as part of a command.
func expandRemoteTilde(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		return "$HOME" + path[1:]
	}
	return path
}

// shellQuote wraps s in single quotes, escaping any contained single quotes.
// Used to safely pass paths and values to remote shells via SSH exec. SYNTH-17.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
