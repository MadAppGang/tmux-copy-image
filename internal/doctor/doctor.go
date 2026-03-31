// Package doctor implements diagnostic checks for the rpaster installation.
// Run iterates checks in order, formats output, and returns an exit code:
// 0 = all pass, 1 = at least one fail, 2 = warn-only (no failures).
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Status represents the result status of a diagnostic check.
type Status int

const (
	StatusPass Status = iota
	StatusWarn
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// Result holds the outcome of a single diagnostic check.
type Result struct {
	Status      Status
	Description string // human-readable summary
	Remediation string // actionable fix text; empty for PASS
}

// Check is the interface that all diagnostic checks implement.
type Check interface {
	Name() string
	Run(ctx context.Context) Result
}

// Config holds parameters for the doctor run.
type Config struct {
	Port       int
	UnixSocket string // local Unix socket path to probe; empty = skip unix socket check
	RemoteHost string // empty = local-only
	PluginDir  string // remote plugin directory to verify
	JSONOutput bool
	Out        io.Writer // defaults to os.Stdout
}

// namedResult pairs a check name with its result.
type namedResult struct {
	CheckName string
	Result    Result
}

// jsonResult is used for JSON output.
type jsonResult struct {
	Check       string `json:"check"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Remediation string `json:"remediation,omitempty"`
}

// ExitCodeError carries a specific exit code for the CLI to use.
type ExitCodeError struct {
	Code    int
	Message string
}

func (e *ExitCodeError) Error() string {
	return e.Message
}

// Run executes all applicable checks and writes output to cfg.Out.
// Returns nil if all pass, *ExitCodeError with Code=1 if any fail,
// *ExitCodeError with Code=2 if warn-only.
func Run(cfg Config) error {
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.Port == 0 {
		cfg.Port = 18339
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	checks := localChecks(cfg)
	if cfg.RemoteHost != "" {
		checks = append(checks, remoteChecks(cfg)...)
	}

	results := make([]namedResult, 0, len(checks))
	for _, c := range checks {
		r := c.Run(ctx)
		results = append(results, namedResult{CheckName: c.Name(), Result: r})
	}

	if cfg.JSONOutput {
		return outputJSON(cfg.Out, results)
	}
	return outputText(cfg.Out, results)
}

func outputText(w io.Writer, results []namedResult) error {
	anyFail := false
	anyWarn := false

	for _, nr := range results {
		r := nr.Result
		fmt.Fprintf(w, "[%s] %s\n", r.Status, r.Description)
		if r.Remediation != "" {
			for _, line := range strings.Split(r.Remediation, "\n") {
				fmt.Fprintf(w, "       %s\n", line)
			}
		}
		switch r.Status {
		case StatusFail:
			anyFail = true
		case StatusWarn:
			anyWarn = true
		}
	}

	if anyFail {
		return &ExitCodeError{Code: 1, Message: "one or more checks failed"}
	}
	if anyWarn {
		return &ExitCodeError{Code: 2, Message: "one or more warnings"}
	}
	return nil
}

func outputJSON(w io.Writer, results []namedResult) error {
	anyFail := false
	anyWarn := false

	jrs := make([]jsonResult, 0, len(results))
	for _, nr := range results {
		jrs = append(jrs, jsonResult{
			Check:       nr.CheckName,
			Status:      nr.Result.Status.String(),
			Description: nr.Result.Description,
			Remediation: nr.Result.Remediation,
		})
		switch nr.Result.Status {
		case StatusFail:
			anyFail = true
		case StatusWarn:
			anyWarn = true
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(jrs); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}

	if anyFail {
		return &ExitCodeError{Code: 1, Message: "one or more checks failed"}
	}
	if anyWarn {
		return &ExitCodeError{Code: 2, Message: "one or more warnings"}
	}
	return nil
}

func localChecks(cfg Config) []Check {
	checks := []Check{
		&binaryCheck{},
		&backendCheck{},
		&daemonCheck{Port: cfg.Port},
		&serviceCheck{},
	}
	// Add unix socket check when a socket path is configured.
	if cfg.UnixSocket != "" {
		checks = append(checks, &unixSocketCheck{SocketPath: cfg.UnixSocket})
	}
	return checks
}

func remoteChecks(cfg Config) []Check {
	pluginDir := cfg.PluginDir
	if pluginDir == "" {
		pluginDir = "~/.tmux/plugins/tmux-clip-image"
	}
	checks := []Check{
		&sshConnectCheck{Host: cfg.RemoteHost},
		&remoteCurlCheck{Host: cfg.RemoteHost},
		&remotePluginCheck{Host: cfg.RemoteHost, PluginDir: pluginDir},
		&tunnelCheck{Host: cfg.RemoteHost, Port: cfg.Port},
	}
	// Add unix tunnel check when a remote socket path is configured.
	if cfg.UnixSocket != "" {
		// Derive the remote socket path: hostname-keyed static path used by the installer.
		// In a real session the path would be SSH_CONNECTION-hashed, but for doctor
		// we probe the static hostname-keyed path that the installer writes.
		checks = append(checks, &unixTunnelCheck{
			Host:       cfg.RemoteHost,
			SocketPath: cfg.UnixSocket,
		})
	}
	return checks
}
