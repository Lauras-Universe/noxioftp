// Package appinfo holds the program's name and the location of its settings.
package appinfo

import (
	"os"
	"path/filepath"
)

// Name is the program's display name; ID is the form used in file and folder names.
const (
	Name = "Noxio FTP"
	ID   = "NoxioFTP"
)

// legacyDirName was the settings folder before the program got its name.
const legacyDirName = "sftp-client"

// ConfigDir returns the settings folder: $NOXIOFTP_CONFIG_DIR if set,
// otherwise a folder named after the program in the user's config directory.
func ConfigDir() string {
	if d := os.Getenv("NOXIOFTP_CONFIG_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, ID)
}

// MigrateLegacyConfig moves the settings of the unnamed first versions
// (folder "sftp-client": profiles, trusted hosts and certificates) to the
// current folder. It does nothing if the folder was overridden by the
// environment, if the new folder already exists or if there is nothing to move.
func MigrateLegacyConfig() {
	if os.Getenv("NOXIOFTP_CONFIG_DIR") != "" {
		return
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	migrate(filepath.Join(base, legacyDirName), filepath.Join(base, ID))
}

func migrate(oldDir, newDir string) {
	if _, err := os.Stat(newDir); err == nil {
		return
	}
	if fi, err := os.Stat(oldDir); err != nil || !fi.IsDir() {
		return
	}
	_ = os.Rename(oldDir, newDir) // same parent folder, so same volume
}
