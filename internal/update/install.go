package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"noxioftp/internal/appinfo"
)

const (
	binaryName  = appinfo.ID          // program file inside the archives
	bundleName  = appinfo.ID + ".app" // macOS app bundle
	markerFile  = "install-method"
	markerValue = "script"
)

// ConfigDir is where the install script leaves its marker (same place as the app's settings).
func ConfigDir() string { return appinfo.ConfigDir() }

// target describes what gets replaced: the .app bundle on macOS, the program file elsewhere.
type target struct {
	path string
	goos string
}

// locate finds the installed program for exe, or explains why it cannot be replaced.
func locate(exe, goos string) (target, string) {
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if goos == "darwin" {
		sep := string(filepath.Separator)
		i := strings.LastIndex(exe, ".app"+sep+"Contents"+sep+"MacOS"+sep)
		if i < 0 {
			return target{}, "Das Programm läuft nicht als App-Paket"
		}
		return target{path: exe[:i+len(".app")], goos: goos}, ""
	}
	return target{path: exe, goos: goos}, ""
}

func writableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".noxioftp-write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// detect decides how an update can be applied for the program at exe.
// configDir holds the marker written by the install script.
func detect(exe, goos, configDir string) (method, reason string) {
	t, why := locate(exe, goos)
	if why != "" {
		return MethodManual, why
	}
	if data, err := os.ReadFile(filepath.Join(configDir, markerFile)); err == nil {
		lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
		if len(lines) >= 2 && strings.TrimSpace(lines[0]) == markerValue && samePath(strings.TrimSpace(lines[1]), t.path) {
			return MethodScript, ""
		}
	}
	if strings.Contains(t.path, "/AppTranslocation/") {
		return MethodManual, "macOS führt das Programm aus einem schreibgeschützten Ordner aus. Bitte nach „Programme“ verschieben oder per Terminal installieren"
	}
	if !writableDir(filepath.Dir(t.path)) {
		return MethodManual, "Kein Schreibrecht auf " + filepath.Dir(t.path)
	}
	return MethodSelf, ""
}

func samePath(a, b string) bool {
	if x, err := filepath.EvalSymlinks(a); err == nil {
		a = x
	}
	if x, err := filepath.EvalSymlinks(b); err == nil {
		b = x
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// DetectMethod reports how the running program can be updated.
func DetectMethod() (method, reason string) {
	exe, err := os.Executable()
	if err != nil {
		return MethodManual, err.Error()
	}
	return detect(exe, runtime.GOOS, ConfigDir())
}

// Apply downloads the release and replaces the running program with it. It
// returns the path to start for the restart. The caller must quit afterwards.
func Apply(ctx context.Context, rel *Release, progress Progress) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	method, reason := detect(exe, runtime.GOOS, ConfigDir())
	if method != MethodSelf {
		return "", errors.New("Automatisches Aktualisieren ist hier nicht möglich: " + reason)
	}
	t, _ := locate(exe, runtime.GOOS)
	return apply(ctx, rel, t, progress)
}

func apply(ctx context.Context, rel *Release, t target, progress Progress) (string, error) {
	// the staging folder lives next to the target so that the final rename stays on one volume
	stage, err := os.MkdirTemp(filepath.Dir(t.path), ".noxioftp-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)

	archive, err := rel.download(ctx, t.goos, stage, progress)
	if err != nil {
		return "", err
	}
	switch t.goos {
	case "darwin":
		return t.path, installBundle(archive, stage, t.path)
	case "windows":
		return t.path, installWindows(archive, stage, t.path)
	default:
		return t.path, installLinux(archive, stage, t.path)
	}
}

// swap puts newPath in place of oldPath. The old version is moved aside first
// and restored if the move fails. A program that is still running can be
// renamed on every platform, which is what makes the replacement possible.
func swap(newPath, oldPath string) (aside string, err error) {
	aside = oldPath + ".old"
	os.RemoveAll(aside)
	if err := os.Rename(oldPath, aside); err != nil {
		return "", err
	}
	if err := os.Rename(newPath, oldPath); err != nil {
		os.Rename(aside, oldPath) // roll back
		return "", err
	}
	return aside, nil
}

func installBundle(zipPath, stage, bundle string) error {
	out := filepath.Join(stage, "x")
	if err := os.Mkdir(out, 0o755); err != nil {
		return err
	}
	// ditto keeps permissions and the code signature seal intact
	if b, err := exec.Command("ditto", "-x", "-k", zipPath, out).CombinedOutput(); err != nil {
		return fmt.Errorf("Entpacken fehlgeschlagen: %v: %s", err, strings.TrimSpace(string(b)))
	}
	newApp := filepath.Join(out, bundleName)
	if _, err := os.Stat(filepath.Join(newApp, "Contents", "MacOS", binaryName)); err != nil {
		return errors.New("Das Archiv enthält keine gültige App")
	}
	if _, err := exec.LookPath("codesign"); err == nil {
		if b, err := exec.Command("codesign", "--verify", "--deep", "--strict", newApp).CombinedOutput(); err != nil {
			return fmt.Errorf("Signaturprüfung der neuen App fehlgeschlagen: %s", strings.TrimSpace(string(b)))
		}
	}
	aside, err := swap(newApp, bundle)
	if err != nil {
		return err
	}
	os.RemoveAll(aside)
	return nil
}

func installLinux(archivePath, stage, binary string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	newBin := filepath.Join(stage, binaryName)
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) != binaryName || h.Typeflag != tar.TypeReg {
			continue
		}
		if err := writeFile(newBin, tr, 0o755); err != nil {
			return err
		}
		found = true
		break
	}
	if !found {
		return errors.New("Das Archiv enthält keine Programmdatei")
	}
	aside, err := swap(newBin, binary)
	if err != nil {
		return err
	}
	os.RemoveAll(aside)
	return nil
}

func installWindows(zipPath, stage, exe string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	newExe := filepath.Join(stage, binaryName+".exe")
	found := false
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.Base(f.Name), binaryName+".exe") || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(newExe, rc, 0o755)
		rc.Close()
		if err != nil {
			return err
		}
		found = true
		break
	}
	if !found {
		return errors.New("Das Archiv enthält keine Programmdatei")
	}
	// the running .exe cannot be deleted, so the old file stays as .old until the next start (CleanupOld)
	_, err = swap(newExe, exe)
	return err
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(r, 512<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// CleanupOld removes the leftover of a previous update (Windows keeps the old .exe until the next start).
func CleanupOld() {
	if exe, err := os.Executable(); err == nil {
		if t, why := locate(exe, runtime.GOOS); why == "" {
			os.RemoveAll(t.path + ".old")
		}
	}
}

// Relaunch starts the updated program. The caller quits right afterwards.
func Relaunch(path string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-n", path) // -n: a new instance, the old one is still running
	} else {
		cmd = exec.Command(path)
	}
	return cmd.Start()
}
