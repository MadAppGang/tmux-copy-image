package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// binaryCheck verifies that the rpaster binary exists and is executable.
type binaryCheck struct{}

func (c *binaryCheck) Name() string { return "rpaster binary" }

func (c *binaryCheck) Run(_ context.Context) Result {
	// Check if the current executable is accessible.
	self, err := os.Executable()
	if err != nil {
		return Result{
			Status:      StatusFail,
			Description: "rpaster binary: cannot determine executable path",
			Remediation: "Run rpaster install to install the binary to ~/.local/bin/rpaster",
		}
	}

	// Resolve symlinks to find the real path.
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		real = self
	}

	info, err := os.Stat(real)
	if err != nil {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("rpaster binary not found at %s", real),
			Remediation: "Run rpaster install to install the binary",
		}
	}

	// Check that it is executable by owner.
	mode := info.Mode()
	if mode&0111 == 0 {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("rpaster binary at %s is not executable", real),
			Remediation: fmt.Sprintf("Run: chmod +x %s", real),
		}
	}

	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("rpaster binary found at %s", real),
	}
}

// backendCheck verifies that a clipboard backend tool is available in PATH.
type backendCheck struct{}

func (c *backendCheck) Name() string { return "clipboard backend" }

func (c *backendCheck) Run(_ context.Context) Result {
	switch runtime.GOOS {
	case "darwin":
		return c.checkDarwin()
	case "linux":
		return c.checkLinux()
	default:
		return Result{
			Status:      StatusWarn,
			Description: fmt.Sprintf("clipboard backend: unsupported OS %q — cannot verify", runtime.GOOS),
		}
	}
}

func (c *backendCheck) checkDarwin() Result {
	if path, err := exec.LookPath("pngpaste"); err == nil {
		return Result{
			Status:      StatusPass,
			Description: fmt.Sprintf("clipboard backend: pngpaste found at %s", path),
		}
	}
	if _, err := exec.LookPath("osascript"); err == nil {
		return Result{
			Status:      StatusWarn,
			Description: "clipboard backend: pngpaste not found, using osascript fallback (PNG only)",
			Remediation: "Install pngpaste for better multi-format support: brew install pngpaste",
		}
	}
	return Result{
		Status:      StatusFail,
		Description: "clipboard backend: no clipboard tool found on macOS",
		Remediation: "Install pngpaste: brew install pngpaste",
	}
}

func (c *backendCheck) checkLinux() Result {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if path, err := exec.LookPath("wl-paste"); err == nil {
			return Result{
				Status:      StatusPass,
				Description: fmt.Sprintf("clipboard backend: wl-paste found at %s (Wayland)", path),
			}
		}
		return Result{
			Status:      StatusFail,
			Description: "clipboard backend: Wayland detected but wl-paste not found",
			Remediation: "Install wl-clipboard: sudo apt install wl-clipboard  (or equivalent)",
		}
	}
	if os.Getenv("DISPLAY") != "" {
		if path, err := exec.LookPath("xclip"); err == nil {
			return Result{
				Status:      StatusPass,
				Description: fmt.Sprintf("clipboard backend: xclip found at %s (X11)", path),
			}
		}
		return Result{
			Status:      StatusFail,
			Description: "clipboard backend: X11 detected but xclip not found",
			Remediation: "Install xclip: sudo apt install xclip  (or equivalent)",
		}
	}
	return Result{
		Status:      StatusWarn,
		Description: "clipboard backend: no display environment detected (DISPLAY and WAYLAND_DISPLAY are unset)",
		Remediation: "Ensure a display server is running and DISPLAY or WAYLAND_DISPLAY is exported",
	}
}

// daemonCheck queries /health on the loopback port to verify the daemon is running.
type daemonCheck struct {
	Port int
}

func (c *daemonCheck) Name() string { return "rpaster daemon" }

func (c *daemonCheck) Run(ctx context.Context) Result {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", c.Port)

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return Result{
			Status:      StatusFail,
			Description: "rpaster daemon: failed to construct health request",
			Remediation: "This is an internal error; please report it",
		}
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("rpaster daemon: not running or unreachable on port %d", c.Port),
			Remediation: "Run: rpaster serve  (or: rpaster install to set up autostart)",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{
			Status:      StatusWarn,
			Description: fmt.Sprintf("rpaster daemon: /health returned HTTP %d", resp.StatusCode),
			Remediation: "Restart rpaster: launchctl kickstart -k gui/$(id -u)/com.rpaster",
		}
	}

	var health struct {
		Status        string `json:"status"`
		UptimeSeconds int64  `json:"uptime_seconds"`
		Backend       string `json:"backend"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return Result{
			Status:      StatusWarn,
			Description: "rpaster daemon: running but could not parse /health response",
		}
	}

	uptime := time.Duration(health.UptimeSeconds) * time.Second
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("rpaster daemon is running (backend: %s, uptime: %s)", health.Backend, fmtDuration(uptime)),
	}
}

// fmtDuration formats a duration in a human-friendly way (e.g., "3h22m").
func fmtDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// serviceCheck verifies that a launchd plist or systemd unit is installed.
type serviceCheck struct{}

func (c *serviceCheck) Name() string { return "service unit" }

func (c *serviceCheck) Run(_ context.Context) Result {
	switch runtime.GOOS {
	case "darwin":
		return c.checkLaunchd()
	case "linux":
		return c.checkSystemd()
	default:
		return Result{
			Status:      StatusWarn,
			Description: fmt.Sprintf("service unit: unsupported OS %q — cannot verify service", runtime.GOOS),
		}
	}
}

func (c *serviceCheck) checkLaunchd() Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{
			Status:      StatusWarn,
			Description: "service unit: cannot determine home directory",
		}
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.rpaster.plist")
	if _, err := os.Stat(plistPath); err != nil {
		return Result{
			Status:      StatusWarn,
			Description: "service unit: launchd plist not found at ~/Library/LaunchAgents/com.rpaster.plist",
			Remediation: "Run: rpaster install  to create the service unit",
		}
	}
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("service unit: launchd plist found at %s", plistPath),
	}
}

func (c *serviceCheck) checkSystemd() Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{
			Status:      StatusWarn,
			Description: "service unit: cannot determine home directory",
		}
	}
	unitPath := filepath.Join(home, ".config", "systemd", "user", "rpaster.service")
	if _, err := os.Stat(unitPath); err != nil {
		return Result{
			Status:      StatusWarn,
			Description: "service unit: systemd user unit not found at ~/.config/systemd/user/rpaster.service",
			Remediation: "Run: rpaster install  to create the service unit",
		}
	}
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("service unit: systemd user unit found at %s", unitPath),
	}
}

// --- Remote checks (run only with --remote flag) ---

// sshConnectCheck verifies SSH batch-mode connectivity to the remote host.
type sshConnectCheck struct {
	Host string
}

func (c *sshConnectCheck) Name() string { return "SSH connectivity" }

func (c *sshConnectCheck) Run(ctx context.Context) Result {
	out, err := runSSH(ctx, c.Host, "echo ok")
	if err != nil {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("SSH connectivity: cannot connect to %s", c.Host),
			Remediation: fmt.Sprintf("Verify SSH access: ssh -o BatchMode=yes %s echo ok", c.Host),
		}
	}
	if strings.TrimSpace(string(out)) != "ok" {
		return Result{
			Status:      StatusWarn,
			Description: fmt.Sprintf("SSH connectivity: connected to %s but unexpected response", c.Host),
		}
	}
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("SSH connectivity: successfully connected to %s", c.Host),
	}
}

// remoteCurlCheck verifies that curl is available on the remote host.
type remoteCurlCheck struct {
	Host string
}

func (c *remoteCurlCheck) Name() string { return "remote curl" }

func (c *remoteCurlCheck) Run(ctx context.Context) Result {
	out, err := runSSH(ctx, c.Host, "which curl")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("remote curl: curl not found on %s", c.Host),
			Remediation: "Install curl on the remote host: sudo apt install curl  (or equivalent)",
		}
	}
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("remote curl: curl found at %s on %s", strings.TrimSpace(string(out)), c.Host),
	}
}

// remotePluginCheck verifies that the tmux plugin is installed on the remote host.
type remotePluginCheck struct {
	Host      string
	PluginDir string
}

func (c *remotePluginCheck) Name() string { return "remote plugin" }

func (c *remotePluginCheck) Run(ctx context.Context) Result {
	pluginFile := c.PluginDir + "/tmux-clip-image.tmux"
	_, err := runSSH(ctx, c.Host, fmt.Sprintf("test -f %s && echo found", shellQuote(pluginFile)))
	if err != nil {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("remote plugin: tmux-clip-image.tmux not found at %s on %s", c.PluginDir, c.Host),
			Remediation: fmt.Sprintf("Run: rpaster install --remote %s  to push the plugin", c.Host),
		}
	}
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("remote plugin: found at %s on %s", c.PluginDir, c.Host),
	}
}

// tunnelCheck verifies that the SSH RemoteForward tunnel is active by probing
// the daemon's /health endpoint via curl on the remote host.
type tunnelCheck struct {
	Host string
	Port int
}

func (c *tunnelCheck) Name() string { return "SSH tunnel" }

func (c *tunnelCheck) Run(ctx context.Context) Result {
	cmd := fmt.Sprintf("curl -s --max-time 5 --max-redirs 0 http://127.0.0.1:%d/health", c.Port)
	out, err := runSSH(ctx, c.Host, cmd)
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return Result{
			Status:      StatusFail,
			Description: fmt.Sprintf("SSH tunnel: cannot reach rpaster on %s via tunnel (port %d)", c.Host, c.Port),
			Remediation: fmt.Sprintf("Ensure RemoteForward 127.0.0.1:%d 127.0.0.1:%d is in ~/.ssh/config for host %s,\nand that rpaster is running locally.", c.Port, c.Port, c.Host),
		}
	}
	// Check that the response looks like a healthy JSON response.
	if !strings.Contains(string(out), `"status"`) {
		return Result{
			Status:      StatusWarn,
			Description: fmt.Sprintf("SSH tunnel: reached %s but /health response unexpected: %s", c.Host, strings.TrimSpace(string(out))),
		}
	}
	return Result{
		Status:      StatusPass,
		Description: fmt.Sprintf("SSH tunnel: /health reachable from %s (tunnel active)", c.Host),
	}
}

// runSSH runs a command on the remote host via ssh with BatchMode enabled.
// Uses the system ssh binary to honour all SSH config and agent settings.
func runSSH(ctx context.Context, host, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "RemoteCommand=none",
		host,
		command,
	)
	return cmd.Output()
}

// shellQuote wraps a string in single quotes, escaping any single quotes within.
// Used to safely pass file paths containing spaces or special characters to the
// remote shell via SSH exec. SYNTH-17.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
