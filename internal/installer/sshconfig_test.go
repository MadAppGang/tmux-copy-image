package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes content to a temp file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// readConfig reads the config file content.
func readConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}

func TestInjectRemoteForward_NewFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	// File does not exist yet.
	if err := InjectRemoteForward("myhost", 18339, configPath); err != nil {
		t.Fatalf("InjectRemoteForward: %v", err)
	}

	content := readConfig(t, configPath)
	if !strings.Contains(content, "Host myhost") {
		t.Errorf("expected 'Host myhost' in config, got:\n%s", content)
	}
	if !strings.Contains(content, "RemoteForward 127.0.0.1:18339 127.0.0.1:18339") {
		t.Errorf("expected RemoteForward in config, got:\n%s", content)
	}
	if !strings.Contains(content, "# --- rpaster BEGIN myhost ---") {
		t.Errorf("expected begin marker in config, got:\n%s", content)
	}
	if !strings.Contains(content, "# --- rpaster END myhost ---") {
		t.Errorf("expected end marker in config, got:\n%s", content)
	}
}

func TestInjectRemoteForward_AppendToExisting(t *testing.T) {
	existing := "Host otherhost\n    IdentityFile ~/.ssh/id_rsa\n"
	configPath := writeTempConfig(t, existing)

	if err := InjectRemoteForward("myhost", 18339, configPath); err != nil {
		t.Fatalf("InjectRemoteForward: %v", err)
	}

	content := readConfig(t, configPath)
	// Original content should still be there.
	if !strings.Contains(content, "Host otherhost") {
		t.Errorf("expected original Host otherhost to remain")
	}
	// New block should be appended.
	if !strings.Contains(content, "Host myhost") {
		t.Errorf("expected 'Host myhost' appended to config")
	}
	if !strings.Contains(content, "RemoteForward 127.0.0.1:18339 127.0.0.1:18339") {
		t.Errorf("expected RemoteForward in config")
	}
}

func TestInjectRemoteForward_ReplacesExistingManagedBlock(t *testing.T) {
	existing := "# --- rpaster BEGIN myhost ---\nHost myhost\n    RemoteForward 127.0.0.1:9999 127.0.0.1:9999\n# --- rpaster END myhost ---\n"
	configPath := writeTempConfig(t, existing)

	// Inject with new port.
	if err := InjectRemoteForward("myhost", 18339, configPath); err != nil {
		t.Fatalf("InjectRemoteForward: %v", err)
	}

	content := readConfig(t, configPath)
	// Old port should be replaced.
	if strings.Contains(content, "9999") {
		t.Errorf("expected old port 9999 to be replaced, but still found in:\n%s", content)
	}
	if !strings.Contains(content, "RemoteForward 127.0.0.1:18339 127.0.0.1:18339") {
		t.Errorf("expected new port 18339 in config:\n%s", content)
	}
	// Should only have one managed block.
	count := strings.Count(content, "# --- rpaster BEGIN myhost ---")
	if count != 1 {
		t.Errorf("expected exactly 1 managed block, found %d in:\n%s", count, content)
	}
}

func TestInjectRemoteForward_SkipsUnmanagedHostBlock(t *testing.T) {
	// An existing un-managed Host block should cause a warning and no modification.
	existing := "Host myhost\n    IdentityFile ~/.ssh/mykey\n"
	configPath := writeTempConfig(t, existing)

	// Capture stdout by redirecting... we just check file is unchanged.
	if err := InjectRemoteForward("myhost", 18339, configPath); err != nil {
		t.Fatalf("InjectRemoteForward returned error for unmanaged host: %v", err)
	}

	content := readConfig(t, configPath)
	// The file should NOT have been modified with a RemoteForward.
	if strings.Contains(content, "RemoteForward") {
		t.Errorf("expected file unchanged (no RemoteForward added), got:\n%s", content)
	}
	// The original content should be preserved.
	if content != existing {
		t.Errorf("expected config unchanged, got:\n%s", content)
	}
}

func TestRemoveRemoteForward_RemovesManagedBlock(t *testing.T) {
	existing := "Host other\n    Port 22\n\n# --- rpaster BEGIN myhost ---\nHost myhost\n    RemoteForward 127.0.0.1:18339 127.0.0.1:18339\n# --- rpaster END myhost ---\n"
	configPath := writeTempConfig(t, existing)

	if err := RemoveRemoteForward("myhost", configPath); err != nil {
		t.Fatalf("RemoveRemoteForward: %v", err)
	}

	content := readConfig(t, configPath)
	if strings.Contains(content, "rpaster BEGIN myhost") {
		t.Errorf("expected managed block removed, but still found in:\n%s", content)
	}
	if strings.Contains(content, "RemoteForward") {
		t.Errorf("expected RemoteForward removed, but still found in:\n%s", content)
	}
	// Other content should remain.
	if !strings.Contains(content, "Host other") {
		t.Errorf("expected 'Host other' to remain after removal")
	}
}

func TestRemoveRemoteForward_NoopIfNoBlock(t *testing.T) {
	existing := "Host myhost\n    Port 22\n"
	configPath := writeTempConfig(t, existing)

	if err := RemoveRemoteForward("myhost", configPath); err != nil {
		t.Fatalf("RemoveRemoteForward: %v", err)
	}

	// File should be unchanged since there was no managed block.
	content := readConfig(t, configPath)
	if content != existing {
		t.Errorf("expected file unchanged, got:\n%s", content)
	}
}

func TestRemoveRemoteForward_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "does-not-exist")

	// Should be a no-op (file doesn't exist, so no block to remove).
	if err := RemoveRemoteForward("myhost", configPath); err != nil {
		t.Fatalf("RemoveRemoteForward on missing file: %v", err)
	}
}

func TestInjectRemoteForward_CreatesBackup(t *testing.T) {
	existing := "Host other\n    Port 22\n"
	configPath := writeTempConfig(t, existing)

	if err := InjectRemoteForward("myhost", 18339, configPath); err != nil {
		t.Fatalf("InjectRemoteForward: %v", err)
	}

	// A backup file should have been created.
	dir := filepath.Dir(configPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".rpaster.bak.") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected backup file to be created, but none found in %s", dir)
	}
}

func TestInjectRemoteForward_NoBackupForNewFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	// File does not exist.

	if err := InjectRemoteForward("myhost", 18339, configPath); err != nil {
		t.Fatalf("InjectRemoteForward: %v", err)
	}

	// No backup should be created for a new (non-existent) file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, e := range entries {
		if strings.Contains(e.Name(), ".rpaster.bak.") {
			t.Errorf("unexpected backup file created for new config: %s", e.Name())
		}
	}
}

func TestHasManagedBlock(t *testing.T) {
	tests := []struct {
		name    string
		content string
		host    string
		want    bool
	}{
		{
			name:    "has block",
			content: "# --- rpaster BEGIN myhost ---\nHost myhost\n# --- rpaster END myhost ---\n",
			host:    "myhost",
			want:    true,
		},
		{
			name:    "no block",
			content: "Host myhost\n    Port 22\n",
			host:    "myhost",
			want:    false,
		},
		{
			name:    "different host block",
			content: "# --- rpaster BEGIN otherhost ---\nHost otherhost\n# --- rpaster END otherhost ---\n",
			host:    "myhost",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasManagedBlock(tt.content, tt.host)
			if got != tt.want {
				t.Errorf("hasManagedBlock(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestHasUnmanagedHost(t *testing.T) {
	tests := []struct {
		name    string
		content string
		host    string
		want    bool
	}{
		{
			name:    "unmanaged host",
			content: "Host myhost\n    Port 22\n",
			host:    "myhost",
			want:    true,
		},
		{
			name:    "managed host only",
			content: "# --- rpaster BEGIN myhost ---\nHost myhost\n    RemoteForward 127.0.0.1:18339 127.0.0.1:18339\n# --- rpaster END myhost ---\n",
			host:    "myhost",
			want:    false,
		},
		{
			name:    "different host",
			content: "Host otherhost\n    Port 22\n",
			host:    "myhost",
			want:    false,
		},
		{
			name:    "empty config",
			content: "",
			host:    "myhost",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasUnmanagedHost(tt.content, tt.host)
			if got != tt.want {
				t.Errorf("hasUnmanagedHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestManagedBlock_ExplicitLoopback(t *testing.T) {
	// SYNTH-18: RemoteForward must use explicit 127.0.0.1: bind address.
	block := managedBlock("myhost", 18339)
	if !strings.Contains(block, "127.0.0.1:18339") {
		t.Errorf("expected explicit 127.0.0.1: bind address in managed block:\n%s", block)
	}
	// Should contain two 127.0.0.1:18339 (local bind + remote target).
	count := strings.Count(block, "127.0.0.1:18339")
	if count < 2 {
		t.Errorf("expected at least 2 occurrences of 127.0.0.1:18339 in:\n%s", block)
	}
}
