// rpaster is the HTTP daemon that exposes local clipboard images over a
// loopback HTTP server for use by the tmux plugin over an SSH tunnel.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jacksteamdev/tmux-image-clipboard/internal/clipboard"
	"github.com/jacksteamdev/tmux-image-clipboard/internal/daemon"
	"github.com/jacksteamdev/tmux-image-clipboard/internal/doctor"
	"github.com/jacksteamdev/tmux-image-clipboard/internal/embedded"
	"github.com/jacksteamdev/tmux-image-clipboard/internal/installer"
)

// Build-time variables injected via ldflags:
//
//	go build -ldflags="-X main.version=v1.0.0 -X main.commit=abc1234 -X main.date=2026-01-01"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	showSetupHint()
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// showSetupHint prints a one-liner on first-ever invocation of any subcommand.
// Once shown, it creates a marker file so it never appears again.
func showSetupHint() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	marker := filepath.Join(home, ".config", "rpaster", ".setup-shown")
	if _, err := os.Stat(marker); err == nil {
		return // already shown
	}
	fmt.Fprintln(os.Stderr, "\033[1mTip:\033[0m first time? Run \033[1mrpaster setup\033[0m for quickstart instructions.")
	fmt.Fprintln(os.Stderr, "")
	// Create marker so we don't show again.
	_ = os.MkdirAll(filepath.Dir(marker), 0700)
	_ = os.WriteFile(marker, []byte("shown\n"), 0600)
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rpaster",
		Short: "Clipboard image HTTP daemon for tmux remote paste",
		Long: `rpaster serves local clipboard images over a loopback HTTP server
so that tmux sessions on remote machines can fetch and paste them via
an SSH RemoteForward tunnel.

Quickstart:  rpaster setup
Diagnostics: rpaster doctor`,
	}

	root.AddCommand(newServeCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newSetupCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newInstallCmd())
	root.AddCommand(newUninstallCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("rpaster %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Show quickstart setup instructions",
		Long:  "Print step-by-step setup instructions for rpaster.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(`
┌─────────────────────────────────────────────────────┐
│  rpaster — clipboard image paste over SSH           │
└─────────────────────────────────────────────────────┘

  Paste images from your LOCAL clipboard into a remote
  tmux session (Claude Code, vim, any terminal app).

  LOCAL MACHINE            SSH tunnel          REMOTE MACHINE
  ┌──────────┐     RemoteForward 18339     ┌──────────────┐
  │ clipboard │ ◄──────────────────────── │ tmux plugin  │
  │ rpaster   │          tunnel            │ prefix + V   │
  └──────────┘                             └──────────────┘

━━━ INSTALL ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Option A — Homebrew (macOS/Linux):

    brew install MadAppGang/tap/rpaster
    brew services start rpaster

  Option B — Go install:

    go install github.com/MadAppGang/tmux-copy-image/cmd/rpaster@latest
    rpaster install

  Option C — curl installer:

    curl -sSL https://raw.githubusercontent.com/MadAppGang/tmux-copy-image/main/install.sh | bash

━━━ CONFIGURE SSH ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Add to ~/.ssh/config on your LOCAL machine:

    Host your-remote-host
        RemoteForward 127.0.0.1:18339 127.0.0.1:18339

  Or use the installer (does it automatically):

    rpaster install --remote your-remote-host

━━━ INSTALL TMUX PLUGIN ON REMOTE ━━━━━━━━━━━━━━━━━━━

  Option A — Automatic:

    rpaster install --remote your-remote-host

  Option B — Manual:

    ssh your-remote-host
    git clone https://github.com/MadAppGang/tmux-copy-image ~/.tmux/plugins/tmux-clip-image
    echo 'run-shell ~/.tmux/plugins/tmux-clip-image/tmux-clip-image.tmux' >> ~/.tmux.conf
    tmux source-file ~/.tmux.conf

━━━ USE IT ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  1. Copy an image on your local machine (Cmd+C, screenshot, etc.)
  2. SSH to your remote:  ssh your-remote-host
  3. In tmux, press:      prefix + V
  4. The image file path appears in your pane

━━━ VERIFY ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  rpaster doctor                          # check local setup
  rpaster doctor --remote your-remote     # check full pipeline

  More info: https://github.com/MadAppGang/tmux-copy-image
`)
		},
	}
}

func newServeCmd() *cobra.Command {
	var (
		port      int
		token     string
		logFormat string
		logLevel  string
		pidFile   string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the clipboard image HTTP daemon",
		Long: `Start the rpaster HTTP daemon on 127.0.0.1:<port>.

The daemon reads the system clipboard and serves its content over HTTP.
Connect a tmux plugin on a remote machine via SSH RemoteForward to use it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Expand ~ in pid file path.
			if pidFile == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("home dir: %w", err)
				}
				pidFile = filepath.Join(home, ".local", "share", "rpaster", "rpaster.pid")
			}

			// Read token from env if not provided via flag.
			if token == "" {
				token = os.Getenv("CLIP_SERVE_TOKEN")
			}

			// Warn if token is configured but too short.
			if err := daemon.ValidateTokenLength(token); err != nil {
				slog.Warn("token configuration warning", "error", err)
			}

			// Select the clipboard backend for this OS.
			backend := clipboard.DetectBackend()
			if !backend.Available() {
				slog.Warn("clipboard backend not available", "backend", backend.Name())
			}

			srv := daemon.New(daemon.Config{
				Port:      port,
				Token:     token,
				LogFormat: logFormat,
				LogLevel:  logLevel,
				PIDFile:   pidFile,
				Version:   version,
				Backend:   backend,
			})

			return srv.Start()
		},
	}

	cmd.Flags().IntVar(&port, "port", 18339, "Port to listen on (always binds to 127.0.0.1)")
	cmd.Flags().StringVar(&token, "token", "", "Bearer token for authentication (empty = disabled). Prefer CLIP_SERVE_TOKEN env var.")
	cmd.Flags().StringVar(&logFormat, "log-format", "text", `Log format: "text" or "json"`)
	cmd.Flags().StringVar(&logLevel, "log-level", "info", `Log level: "debug", "info", "warn", "error"`)
	cmd.Flags().StringVar(&pidFile, "pid-file", "", "Path to PID file (default: ~/.local/share/rpaster/rpaster.pid)")

	return cmd
}

func newDoctorCmd() *cobra.Command {
	var (
		remoteHost string
		jsonOutput bool
		port       int
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks",
		Long: `Run end-to-end diagnostic checks for the rpaster installation.

Checks include: binary availability, clipboard backend, daemon health,
and service unit presence. Use --remote to also check the remote side.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := doctor.Config{
				Port:       port,
				RemoteHost: remoteHost,
				JSONOutput: jsonOutput,
				Out:        os.Stdout,
			}
			err := doctor.Run(cfg)
			if err != nil {
				// ExitCodeError carries a specific exit code.
				var exitErr *doctor.ExitCodeError
				if errors.As(err, &exitErr) {
					fmt.Fprintf(os.Stderr, "%s\n", exitErr.Message)
					os.Exit(exitErr.Code)
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&remoteHost, "remote", "", "SSH host to also check remote side")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit check results as JSON")
	cmd.Flags().IntVar(&port, "port", 18339, "Port the daemon is listening on")

	return cmd
}

func newInstallCmd() *cobra.Command {
	var (
		remoteHost  string
		port        int
		token       string
		pluginDir   string
		noSSHConfig bool
		dryRun      bool
		binaryPath  string
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install rpaster and configure the service",
		Long: `Install rpaster locally and optionally push the tmux plugin to a remote host.

Local installation:
  - Copies the binary to ~/.local/bin/rpaster (or --binary-path)
  - Creates a launchd plist (macOS) or systemd user unit (Linux)
  - Loads/enables the service

Remote installation (requires --remote):
  - Pushes the embedded tmux plugin to the remote host
  - Optionally updates ~/.tmux.conf with the plugin entry
  - Optionally adds RemoteForward to ~/.ssh/config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read token from env if not set via flag.
			if token == "" {
				token = os.Getenv("CLIP_SERVE_TOKEN")
			}

			cfg := installer.Config{
				BinaryPath:    binaryPath,
				Port:          port,
				Token:         token,
				RemoteHost:    remoteHost,
				PluginDir:     pluginDir,
				ModifySSHConf: !noSSHConfig,
				DryRun:        dryRun,
				PluginFS:      embedded.PluginFS,
			}

			if err := installer.RunLocal(cfg); err != nil {
				return err
			}

			if remoteHost != "" {
				if err := installer.RunRemote(cfg); err != nil {
					return err
				}
				if !noSSHConfig {
					home, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("home dir: %w", err)
					}
					sshConfigPath := filepath.Join(home, ".ssh", "config")
					if err := installer.InjectRemoteForward(remoteHost, port, sshConfigPath); err != nil {
						return err
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&remoteHost, "remote", "", "SSH host to also install plugin on")
	cmd.Flags().IntVar(&port, "port", 18339, "Port for service unit and SSH tunnel")
	cmd.Flags().StringVar(&token, "token", "", "Bearer token to embed in service unit. Prefer CLIP_SERVE_TOKEN env var.")
	cmd.Flags().StringVar(&pluginDir, "plugin-dir", "", "Override remote plugin directory (default: ~/.tmux/plugins/tmux-clip-image)")
	cmd.Flags().BoolVar(&noSSHConfig, "no-ssh-config", false, "Skip ~/.ssh/config modification")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without making any changes")
	cmd.Flags().StringVar(&binaryPath, "binary-path", "", "Install binary to this path (default: ~/.local/bin/rpaster)")

	return cmd
}

func newUninstallCmd() *cobra.Command {
	var (
		remoteHost  string
		dryRun      bool
		noSSHConfig bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove rpaster and all installed components",
		Long: `Uninstall rpaster and reverse all installation steps.

- Unloads and removes the service unit (launchd/systemd)
- Removes the installed binary
- Optionally removes the tmux plugin from a remote host
- Optionally removes the RemoteForward from ~/.ssh/config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := installer.Config{
				RemoteHost:    remoteHost,
				ModifySSHConf: !noSSHConfig,
				DryRun:        dryRun,
			}
			return installer.Uninstall(cfg)
		},
	}

	cmd.Flags().StringVar(&remoteHost, "remote", "", "SSH host to also remove plugin from")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be removed without making any changes")
	cmd.Flags().BoolVar(&noSSHConfig, "no-ssh-config", false, "Skip ~/.ssh/config modification")

	return cmd
}
