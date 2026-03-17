//go:build linux

package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// systemdUnitTemplate is the template for the Linux systemd user unit.
// Token is passed via Environment= directive (SYNTH-5) — never via ExecStart args.
const systemdUnitTemplate = `[Unit]
Description=clip-serve clipboard image HTTP daemon
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve --port {{.Port}}
{{- if .Token}}
Environment=CLIP_SERVE_TOKEN={{.Token}}
{{- end}}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

// systemdUnitData holds template variables for the systemd unit.
type systemdUnitData struct {
	BinaryPath string
	Port       int
	Token      string
}

// installSystemdService generates and writes the systemd user unit to
// ~/.config/systemd/user/clip-serve.service with mode 0600.
func installSystemdService(cfg Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	unitDir := filepath.Join(home, ".config", "systemd", "user")
	unitPath := filepath.Join(unitDir, "clip-serve.service")

	data := systemdUnitData{
		BinaryPath: cfg.BinaryPath,
		Port:       cfg.Port,
		Token:      cfg.Token,
	}

	if cfg.DryRun {
		fmt.Printf("[dry-run] would write systemd unit: %s\n", unitPath)
		return nil
	}

	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}

	tmpl, err := template.New("unit").Parse(systemdUnitTemplate)
	if err != nil {
		return fmt.Errorf("parse unit template: %w", err)
	}

	// Write atomically: render to temp, rename.
	tmpPath := unitPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create unit temp: %w", err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("render unit: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close unit temp: %w", err)
	}
	if err := os.Rename(tmpPath, unitPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("install unit: %w", err)
	}

	fmt.Printf("created systemd unit: %s\n", unitPath)
	return nil
}

// enableSystemdService enables and starts the systemd user service.
func enableSystemdService() error {
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, out)
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", "clip-serve.service").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable: %w: %s", err, out)
	}
	fmt.Printf("service enabled and started: clip-serve.service\n")
	return nil
}

// disableSystemdService stops and disables the systemd user service.
func disableSystemdService() error {
	exec.Command("systemctl", "--user", "disable", "--now", "clip-serve.service").Run() //nolint:errcheck
	return nil
}

// removeSystemdService removes the systemd unit file.
func removeSystemdService() error {
	home, _ := os.UserHomeDir()
	unit := filepath.Join(home, ".config", "systemd", "user", "clip-serve.service")
	if err := os.Remove(unit); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", unit, err)
	}
	fmt.Printf("removed service unit: %s\n", unit)
	return nil
}
