//go:build darwin

package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// launchdPlistTemplate is the template for the macOS launchd plist.
// Token is passed via EnvironmentVariables (SYNTH-5) — never via ProgramArguments.
const launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.clip-serve</string>

    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>serve</string>
        <string>--port</string>
        <string>{{.Port}}</string>
    </array>

    {{- if .Token}}
    <key>EnvironmentVariables</key>
    <dict>
        <key>CLIP_SERVE_TOKEN</key>
        <string>{{.Token}}</string>
    </dict>
    {{- end}}

    <key>KeepAlive</key>
    <true/>

    <key>RunAtLoad</key>
    <true/>

    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>

    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
</dict>
</plist>
`

// launchdPlistData holds template variables for the launchd plist.
type launchdPlistData struct {
	BinaryPath string
	Port       int
	Token      string
	LogPath    string
}

// installLaunchdService generates and writes the launchd plist to
// ~/Library/LaunchAgents/com.clip-serve.plist with mode 0600.
func installLaunchdService(cfg Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}

	logPath := filepath.Join(home, "Library", "Logs", "clip-serve.log")
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.clip-serve.plist")

	data := launchdPlistData{
		BinaryPath: cfg.BinaryPath,
		Port:       cfg.Port,
		Token:      cfg.Token,
		LogPath:    logPath,
	}

	if cfg.DryRun {
		fmt.Printf("[dry-run] would write launchd plist: %s\n", plistPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	tmpl, err := template.New("plist").Parse(launchdPlistTemplate)
	if err != nil {
		return fmt.Errorf("parse plist template: %w", err)
	}

	// Write atomically: render to temp, rename.
	tmpPath := plistPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create plist temp: %w", err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("render plist: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close plist temp: %w", err)
	}
	if err := os.Rename(tmpPath, plistPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("install plist: %w", err)
	}

	fmt.Printf("created launchd plist: %s\n", plistPath)
	return nil
}

// loadLaunchdService loads the launchd agent.
func loadLaunchdService() error {
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.clip-serve.plist")
	out, err := exec.Command("launchctl", "load", "-w", plist).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, out)
	}
	fmt.Printf("service loaded: com.clip-serve\n")
	return nil
}

// unloadLaunchdService stops and unloads the launchd agent.
func unloadLaunchdService() error {
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.clip-serve.plist")
	out, err := exec.Command("launchctl", "unload", "-w", plist).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl unload: %w: %s", err, out)
	}
	return nil
}

// removeLaunchdService removes the plist file.
func removeLaunchdService() error {
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.clip-serve.plist")
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", plist, err)
	}
	fmt.Printf("removed service unit: %s\n", plist)
	return nil
}
