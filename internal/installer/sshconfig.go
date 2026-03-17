package installer

import (
	"bufio"
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

// InjectRemoteForward adds a managed RemoteForward block for host/port to the
// SSH config file at configPath.
//
// Rules (SYNTH-1):
//   - If an existing un-managed "Host <hostname>" block is found, warn and skip.
//   - If a managed block for this host already exists, overwrite it in-place.
//   - Otherwise, append a new managed block.
//   - A backup is created at <configPath>.rpaster.bak.<timestamp> before any write.
//   - The write is atomic (temp file + rename).
func InjectRemoteForward(host string, port int, configPath string) error {
	existing, err := readSSHConfig(configPath)
	if err != nil {
		return err
	}

	// Check for an un-managed Host block first.
	if hasUnmanagedHost(existing, host) {
		fmt.Printf("warning: existing un-managed 'Host %s' found in %s\n", host, configPath)
		fmt.Printf("  To enable the SSH tunnel, manually add this to your SSH config:\n\n")
		fmt.Printf("  %s\n", managedBlock(host, port))
		return nil
	}

	// Back up the config before modifying.
	if err := backupSSHConfig(configPath, existing); err != nil {
		return err
	}

	var newContent string
	if hasManagedBlock(existing, host) {
		newContent = replaceManagedBlock(existing, host, port)
	} else {
		newContent = appendManagedBlock(existing, host, port)
	}

	return writeSSHConfig(configPath, newContent)
}

// RemoveRemoteForward removes the managed block for host from the SSH config
// at configPath. If no managed block is found, it returns nil.
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
