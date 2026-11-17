package appinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir(t *testing.T) {
	t.Setenv("NOXIOFTP_CONFIG_DIR", "/custom/place")
	if got := ConfigDir(); got != "/custom/place" {
		t.Fatalf("override ignored: %q", got)
	}
	t.Setenv("NOXIOFTP_CONFIG_DIR", "")
	if got := ConfigDir(); filepath.Base(got) != ID {
		t.Fatalf("default folder = %q, want a folder named %s", got, ID)
	}
}

func TestMigrate(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "sftp-client"), filepath.Join(base, ID)

	migrate(oldDir, newDir) // nothing to move: no error, nothing created
	if _, err := os.Stat(newDir); err == nil {
		t.Fatal("created a folder out of nothing")
	}

	os.MkdirAll(oldDir, 0o700)
	os.WriteFile(filepath.Join(oldDir, "connections.json"), []byte("[]"), 0o600)
	os.WriteFile(filepath.Join(oldDir, "known_hosts"), []byte("host key"), 0o600)
	migrate(oldDir, newDir)
	if b, err := os.ReadFile(filepath.Join(newDir, "known_hosts")); err != nil || string(b) != "host key" {
		t.Fatalf("settings not moved: %q, %v", b, err)
	}
	if _, err := os.Stat(oldDir); err == nil {
		t.Fatal("old folder still there")
	}

	// both exist: the new one wins and the old one is left alone
	os.MkdirAll(oldDir, 0o700)
	os.WriteFile(filepath.Join(oldDir, "marker"), []byte("old"), 0o600)
	migrate(oldDir, newDir)
	if _, err := os.Stat(filepath.Join(oldDir, "marker")); err != nil {
		t.Fatal("old folder was touched although the new one exists")
	}
	if _, err := os.Stat(filepath.Join(newDir, "marker")); err == nil {
		t.Fatal("old files were merged into the new folder")
	}
}
