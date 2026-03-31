package installer

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// markerBegin is the start of a managed block for a given host.
	// The host name is appended after "BEGIN ".
	markerBeginFmt = "# --- rpaster BEGIN %s ---"

	// markerEnd is the end of a managed block for a given host.
	markerEndFmt = "# --- rpaster END %s ---"
)

// managedBlock returns the RemoteForward stanza for the given host and port,
// wrapped in managed marker comments.
func managedBlock(host string, port int) string {
	return fmt.Sprintf("%s\nHost %s\n    RemoteForward 127.0.0.1:%d 127.0.0.1:%d\n%s\n",
		fmt.Sprintf(markerBeginFmt, host),
		host,
		port,
		port,
		fmt.Sprintf(markerEndFmt, host),
	)
}

// remoteForwardDirective returns the RemoteForward line (without newline) for
// embedding into an existing Host block.
func remoteForwardDirective(port int) string {
	return fmt.Sprintf("    RemoteForward 127.0.0.1:%d 127.0.0.1:%d", port, port)
}

// managedDirective returns a RemoteForward line wrapped in inline markers for
// injection into an existing Host block.
func managedDirective(host string, port int) string {
	return fmt.Sprintf("    %s\n%s\n    %s",
		fmt.Sprintf(markerBeginFmt, host),
		remoteForwardDirective(port),
		fmt.Sprintf(markerEndFmt, host),
	)
}

// SessionSocketPath returns the deterministic remote socket path for a given
// SSH session connection string (the SSH_CONNECTION environment variable).
//
// The socket name is /tmp/rpaster-<16hexchars>.sock where the hex is derived
// from the first 8 bytes of SHA-256(sshConnection).
//
// Example:
//
//	SSH_CONNECTION="10.0.1.5 54312 10.0.1.10 22"
//	→ /tmp/rpaster-3fa8bc12d7e4910f.sock
func SessionSocketPath(sshConnection string) string {
	h := sha256.Sum256([]byte(sshConnection))
	return fmt.Sprintf("/tmp/rpaster-%x.sock", h[:8])
}

// HostnameSocketPath returns the static hostname-keyed socket path used by the
// installer when writing a StreamLocalForward directive for a given SSH host.
// This is the simpler fallback when per-session unique paths are not needed.
//
// Example: host="m5" → "/tmp/rpaster-m5.sock"
func HostnameSocketPath(host string) string {
	// Sanitise host: keep only alphanumeric and hyphen characters.
	var sb strings.Builder
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		}
	}
	return fmt.Sprintf("/tmp/rpaster-%s.sock", sb.String())
}

// socketForwardDirective returns a RemoteForward line for Unix socket forwarding.
// Uses RemoteForward (not StreamLocalForward) because Apple's macOS OpenSSH
// does not support StreamLocalForward, but RemoteForward with socket paths works.
//
// Also adds StreamLocalBindUnlink to clean up stale sockets from previous sessions.
//
//	RemoteForward <remote-socket> <local-socket>
func socketForwardDirective(remoteSocket, localSocket string) string {
	return fmt.Sprintf("    StreamLocalBindUnlink yes\n    RemoteForward %s %s", remoteSocket, localSocket)
}

// managedUnixBlock returns a managed SSH config block with both TCP
// RemoteForward (backward compat) and Unix socket RemoteForward.
//
// remoteSocket is the path on the remote side (e.g. /tmp/rpaster-m5.sock).
// localSocket  is the local daemon socket  (e.g. /tmp/rpaster.sock).
func managedUnixBlock(host string, port int, remoteSocket, localSocket string) string {
	return fmt.Sprintf("%s\nHost %s\n    RemoteForward 127.0.0.1:%d 127.0.0.1:%d\n%s\n%s\n",
		fmt.Sprintf(markerBeginFmt, host),
		host,
		port,
		port,
		socketForwardDirective(remoteSocket, localSocket),
		fmt.Sprintf(markerEndFmt, host),
	)
}

// InjectStreamLocalForward adds a managed SSH config block that includes both
// a TCP RemoteForward (backward compat) and a Unix StreamLocalForward for the
// given host to the SSH config at configPath.
//
// Behaviour mirrors InjectRemoteForward: backup, in-place replace, or append.
func InjectStreamLocalForward(host string, port int, remoteSocket, localSocket, configPath string) error {
	existing, err := readSSHConfig(configPath)
	if err != nil {
		return err
	}

	if err := backupSSHConfig(configPath, existing); err != nil {
		return err
	}

	var newContent string
	if hasManagedBlock(existing, host) {
		newContent = replaceManagedUnixBlock(existing, host, port, remoteSocket, localSocket)
	} else {
		newContent = appendManagedUnixBlock(existing, host, port, remoteSocket, localSocket)
	}

	return writeSSHConfig(configPath, newContent)
}

// replaceManagedUnixBlock replaces the existing managed block for host with a
// new unix block (TCP + StreamLocalForward).
func replaceManagedUnixBlock(content, host string, port int, remoteSocket, localSocket string) string {
	beginMarker := fmt.Sprintf(markerBeginFmt, host)
	endMarker := fmt.Sprintf(markerEndFmt, host)

	var result strings.Builder
	inBlock := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	blockWritten := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, beginMarker) {
			inBlock = true
			if !blockWritten {
				result.WriteString(managedUnixBlock(host, port, remoteSocket, localSocket))
				blockWritten = true
			}
			continue
		}
		if inBlock {
			if strings.Contains(line, endMarker) {
				inBlock = false
			}
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

// appendManagedUnixBlock appends a managed unix block for host to the end of content.
func appendManagedUnixBlock(content, host string, port int, remoteSocket, localSocket string) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	return content + managedUnixBlock(host, port, remoteSocket, localSocket)
}

// InjectRemoteForward adds a managed RemoteForward block for host/port to the
// SSH config file at configPath.
//
// Rules:
//   - If a managed block/directive for this host already exists, overwrite it in-place.
//   - If an existing un-managed "Host <hostname>" block is found, inject a managed
//     RemoteForward directive into it.
//   - Otherwise, append a new managed block.
//   - A backup is created at <configPath>.rpaster.bak.<timestamp> before any write.
//   - The write is atomic (temp file + rename).
func InjectRemoteForward(host string, port int, configPath string) error {
	existing, err := readSSHConfig(configPath)
	if err != nil {
		return err
	}

	// Back up the config before modifying.
	if err := backupSSHConfig(configPath, existing); err != nil {
		return err
	}

	var newContent string
	if hasManagedBlock(existing, host) {
		newContent = replaceManagedBlock(existing, host, port)
	} else if hasUnmanagedHost(existing, host) {
		newContent = injectIntoExistingHost(existing, host, port)
	} else {
		newContent = appendManagedBlock(existing, host, port)
	}

	return writeSSHConfig(configPath, newContent)
}

// RemoveRemoteForward removes the managed block or inline directive for host
// from the SSH config at configPath. If no managed block is found, returns nil.
func RemoveRemoteForward(host, configPath string) error {
	existing, err := readSSHConfig(configPath)
	if err != nil {
		return err
	}

	if !hasManagedBlock(existing, host) {
		return nil // nothing to remove
	}

	if err := backupSSHConfig(configPath, existing); err != nil {
		return err
	}

	newContent := removeManagedBlock(existing, host)
	return writeSSHConfig(configPath, newContent)
}

// readSSHConfig reads the SSH config file content. Returns empty string if the
// file does not exist (not an error — it will be created on first write).
func readSSHConfig(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read ssh config %s: %w", configPath, err)
	}
	return string(data), nil
}

// backupSSHConfig writes a copy of content to <configPath>.rpaster.bak.<timestamp>.
// If content is empty (file didn't exist) no backup is created.
func backupSSHConfig(configPath, content string) error {
	if content == "" {
		return nil
	}
	ts := time.Now().UTC().Format("20060102-150405")
	bakPath := configPath + ".rpaster.bak." + ts
	if err := os.WriteFile(bakPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("backup ssh config: %w", err)
	}
	return nil
}

// writeSSHConfig atomically writes content to configPath with mode 0600.
func writeSSHConfig(configPath, content string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("create .ssh dir: %w", err)
	}
	tmpPath := configPath + ".rpaster.tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("write ssh config temp: %w", err)
	}
	if err := os.Rename(tmpPath, configPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename ssh config: %w", err)
	}
	return nil
}

// injectIntoExistingHost inserts a managed RemoteForward directive into an
// existing un-managed Host block. The directive is placed right after the
// "Host <host>" line.
func injectIntoExistingHost(content, host string, port int) string {
	hostRe := regexp.MustCompile(`(?i)^\s*Host\s+` + regexp.QuoteMeta(host) + `\s*$`)
	inManaged := false
	beginMarker := fmt.Sprintf(markerBeginFmt, host)
	endMarker := fmt.Sprintf(markerEndFmt, host)
	injected := false

	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, beginMarker) {
			inManaged = true
		}
		if strings.Contains(line, endMarker) {
			inManaged = false
		}
		result.WriteString(line)
		result.WriteByte('\n')
		// Inject after the first un-managed Host line.
		if !injected && !inManaged && hostRe.MatchString(line) {
			result.WriteString(managedDirective(host, port))
			result.WriteByte('\n')
			injected = true
		}
	}
	return result.String()
}

// hasUnmanagedHost returns true if the config contains "Host <host>" outside
// of a managed rpaster block.
func hasUnmanagedHost(content, host string) bool {
	// Build a regex for "Host <host>" lines (case-insensitive for "Host").
	// An un-managed line is one that is not between managed markers.
	inManaged := false
	beginMarker := fmt.Sprintf(markerBeginFmt, host)
	endMarker := fmt.Sprintf(markerEndFmt, host)
	// Regex matches lines like "Host hostname" or "Host hostname # comment".
	hostRe := regexp.MustCompile(`(?i)^\s*Host\s+` + regexp.QuoteMeta(host) + `\s*$`)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, beginMarker) {
			inManaged = true
		}
		if strings.Contains(line, endMarker) {
			inManaged = false
		}
		if !inManaged && hostRe.MatchString(line) {
			return true
		}
	}
	return false
}

// hasManagedBlock returns true if a managed block for host exists in content.
func hasManagedBlock(content, host string) bool {
	return strings.Contains(content, fmt.Sprintf(markerBeginFmt, host))
}

// replaceManagedBlock replaces the existing managed block for host with a new one.
func replaceManagedBlock(content, host string, port int) string {
	beginMarker := fmt.Sprintf(markerBeginFmt, host)
	endMarker := fmt.Sprintf(markerEndFmt, host)

	var result strings.Builder
	inBlock := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	blockWritten := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, beginMarker) {
			inBlock = true
			if !blockWritten {
				result.WriteString(managedBlock(host, port))
				blockWritten = true
			}
			continue
		}
		if inBlock {
			if strings.Contains(line, endMarker) {
				inBlock = false
			}
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

// appendManagedBlock appends a managed block for host to the end of content.
// Ensures there is a blank line separator if content is non-empty.
func appendManagedBlock(content, host string, port int) string {
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	return content + managedBlock(host, port)
}

// removeManagedBlock removes the managed block for host from content.
func removeManagedBlock(content, host string) string {
	beginMarker := fmt.Sprintf(markerBeginFmt, host)
	endMarker := fmt.Sprintf(markerEndFmt, host)

	var result strings.Builder
	inBlock := false
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, beginMarker) {
			inBlock = true
			continue
		}
		if inBlock {
			if strings.Contains(line, endMarker) {
				inBlock = false
			}
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}
