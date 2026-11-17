package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.2.3", "1.2.2", true}, {"1.2.3", "1.2.3", false}, {"1.2.2", "1.2.3", false},
		{"v1.10.0", "1.9.9", true}, {"2.0", "1.9.9", true}, {"1.0.0", "1.0.0-rc1", true},
		{"1.0.0-rc1", "1.0.0", false}, {"1.0.0-rc2", "1.0.0-rc1", true}, {"1.0.0+build5", "1.0.0", false},
	} {
		got, err := Newer(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("Newer(%q, %q) = %v, %v; want %v", c.a, c.b, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "abc", "1.2.3.4", "1.x"} {
		if _, err := Newer(bad, "1.0.0"); err == nil {
			t.Errorf("Newer(%q) accepted", bad)
		}
	}
}

// fakeRelease serves a GitHub-like API plus the release files.
type fakeRelease struct {
	srv    *httptest.Server
	files  map[string][]byte // asset name -> content
	sums   string            // override for sha256sums.txt, "" = computed
	noSums bool
	tag    string
}

func newFakeRelease(t *testing.T, tag string, files map[string][]byte) *fakeRelease {
	t.Helper()
	f := &fakeRelease{files: files, tag: tag}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		var assets []string
		for name := range f.files {
			assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, name, f.srv.URL+"/dl/"+name))
		}
		if !f.noSums {
			assets = append(assets, fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, sumsName, f.srv.URL+"/dl/"+sumsName))
		}
		fmt.Fprintf(w, `{"tag_name":%q,"body":"Neu: Updates","html_url":"https://example.invalid/r","assets":[%s]}`, f.tag, strings.Join(assets, ","))
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/dl/")
		if name == sumsName {
			if f.sums != "" {
				fmt.Fprint(w, f.sums)
				return
			}
			for n, c := range f.files {
				sum := sha256.Sum256(c)
				fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), n)
			}
			return
		}
		c, ok := f.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(c)
	})
	f.srv = httptest.NewServer(mux)
	old := APIBase
	APIBase = f.srv.URL
	t.Cleanup(func() { APIBase = old; f.srv.Close() })
	return f
}

func tarGz(t *testing.T, name, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipOf(t *testing.T, name, content string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write([]byte(content))
	zw.Close()
	return buf.Bytes()
}

func TestCheck(t *testing.T) {
	newFakeRelease(t, "v1.4.0", map[string][]byte{AssetName(runtime.GOOS): []byte("x")})

	info, rel, err := Check(context.Background(), "1.3.0")
	if err != nil || !info.Available || rel == nil || info.Latest != "1.4.0" || info.Notes != "Neu: Updates" || info.Current != "1.3.0" {
		t.Fatalf("newer: %+v, %v", info, err)
	}
	if info.Method == "" || info.Command == "" {
		t.Errorf("method/command missing: %+v", info)
	}
	if info, _, err := Check(context.Background(), "1.4.0"); err != nil || info.Available {
		t.Errorf("same version: %+v, %v", info, err)
	}
	if info, _, err := Check(context.Background(), "2.0.0"); err != nil || info.Available {
		t.Errorf("older release: %+v, %v", info, err)
	}
	if _, _, err := Check(context.Background(), "dev"); err != ErrDevBuild {
		t.Errorf("dev build: %v", err)
	}
}

func TestCheckHandlesNoReleaseAndServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()
	if _, _, err := Check(context.Background(), "1.0.0"); err == nil || !strings.Contains(err.Error(), "Release") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	asset := AssetName("linux")
	content := []byte("archive-bytes")
	f := newFakeRelease(t, "v2.0.0", map[string][]byte{asset: content})
	rel, _ := Latest(context.Background())

	var last int64
	path, err := rel.download(context.Background(), "linux", t.TempDir(), func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, content) || last != int64(len(content)) {
		t.Fatalf("downloaded %q, progress %d", b, last)
	}

	f.sums = strings.Repeat("0", 64) + "  " + asset + "\n" // checksum of something else
	dir := t.TempDir()
	if _, err := rel.download(context.Background(), "linux", dir, nil); err == nil || !strings.Contains(err.Error(), "Prüfsumme") {
		t.Fatalf("tampered file accepted: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("rejected download was left on disk")
	}

	f.sums = "abc  other-file.zip\n"
	if _, err := rel.download(context.Background(), "linux", t.TempDir(), nil); err == nil {
		t.Fatal("missing checksum line accepted")
	}

	f.sums, f.noSums = "", true
	rel, _ = Latest(context.Background())
	if _, err := rel.download(context.Background(), "linux", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), sumsName) {
		t.Fatalf("release without checksums accepted: %v", err)
	}
	if _, err := rel.download(context.Background(), "windows", t.TempDir(), nil); err == nil {
		t.Fatal("missing asset accepted")
	}
}

func TestUntrustedDownloadURLRefused(t *testing.T) {
	old := APIBase
	APIBase = "https://api.github.com" // production mode
	defer func() { APIBase = old }()
	r := &Release{assets: map[string]string{AssetName("linux"): "https://evil.example/x.tar.gz", sumsName: "https://evil.example/s"}}
	if _, err := r.download(context.Background(), "linux", t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "Adresse") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyLinuxReplacesBinary(t *testing.T) {
	newFakeRelease(t, "v2.0.0", map[string][]byte{AssetName("linux"): tarGz(t, "NoxioFTP", "NEW")})
	rel, _ := Latest(context.Background())
	dir := t.TempDir()
	bin := filepath.Join(dir, "NoxioFTP")
	os.WriteFile(bin, []byte("OLD"), 0o755)

	got, err := apply(context.Background(), rel, target{path: bin, goos: "linux"}, nil)
	if err != nil || got != bin {
		t.Fatalf("apply = %q, %v", got, err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "NEW" {
		t.Fatalf("binary = %q", b)
	}
	if fi, _ := os.Stat(bin); fi.Mode().Perm()&0o100 == 0 {
		t.Error("new binary is not executable")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("leftovers in install dir: %v", entries)
	}
}

func TestApplyWindowsKeepsOldExeAside(t *testing.T) {
	newFakeRelease(t, "v2.0.0", map[string][]byte{AssetName("windows"): zipOf(t, "NoxioFTP.exe", "NEW")})
	rel, _ := Latest(context.Background())
	dir := t.TempDir()
	exe := filepath.Join(dir, "NoxioFTP.exe")
	os.WriteFile(exe, []byte("OLD"), 0o755)

	if _, err := apply(context.Background(), rel, target{path: exe, goos: "windows"}, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW" {
		t.Fatalf("exe = %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD" {
		t.Fatalf(".old = %q (the running exe cannot be deleted, it must stay)", b)
	}
}

func TestApplyRejectsArchiveWithoutProgram(t *testing.T) {
	newFakeRelease(t, "v2.0.0", map[string][]byte{AssetName("linux"): tarGz(t, "something-else", "x")})
	rel, _ := Latest(context.Background())
	bin := filepath.Join(t.TempDir(), "NoxioFTP")
	os.WriteFile(bin, []byte("OLD"), 0o755)
	if _, err := apply(context.Background(), rel, target{path: bin, goos: "linux"}, nil); err == nil {
		t.Fatal("archive without the program accepted")
	}
	if b, _ := os.ReadFile(bin); string(b) != "OLD" {
		t.Fatal("installed program was touched by a failed update")
	}
}

func TestSwapRollsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "prog")
	os.WriteFile(old, []byte("OLD"), 0o755)
	if _, err := swap(filepath.Join(dir, "does-not-exist"), old); err == nil {
		t.Fatal("swap with a missing source succeeded")
	}
	if b, _ := os.ReadFile(old); string(b) != "OLD" {
		t.Fatal("old program was not restored")
	}
}

func TestApplyDarwinReplacesBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("codesign not available")
	}
	mkApp := func(root, marker string) string {
		app := filepath.Join(root, bundleName)
		os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
		// a real Mach-O program so that the ad-hoc signature is valid
		src, _ := os.ReadFile("/bin/echo")
		os.WriteFile(filepath.Join(app, "Contents", "MacOS", binaryName), src, 0o755)
		os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>NoxioFTP</string><key>Marker</key><string>`+marker+`</string></dict></plist>`), 0o644)
		if b, err := exec.Command("codesign", "--force", "--deep", "-s", "-", app).CombinedOutput(); err != nil {
			t.Skipf("cannot sign the test app: %s", b)
		}
		return app
	}
	newApp := mkApp(t.TempDir(), "NEW")
	zipPath := filepath.Join(t.TempDir(), "x.zip")
	if b, err := exec.Command("ditto", "-c", "-k", "--keepParent", newApp, zipPath).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, b)
	}
	zipBytes, _ := os.ReadFile(zipPath)
	newFakeRelease(t, "v2.0.0", map[string][]byte{AssetName("darwin"): zipBytes})
	rel, _ := Latest(context.Background())

	installRoot := t.TempDir()
	oldApp := mkApp(installRoot, "OLD")
	got, err := apply(context.Background(), rel, target{path: oldApp, goos: "darwin"}, nil)
	if err != nil || got != oldApp {
		t.Fatalf("apply = %q, %v", got, err)
	}
	plist, _ := os.ReadFile(filepath.Join(oldApp, "Contents", "Info.plist"))
	if !strings.Contains(string(plist), "NEW") {
		t.Fatalf("bundle not replaced: %s", plist)
	}
	if b, err := exec.Command("codesign", "--verify", "--deep", "--strict", oldApp).CombinedOutput(); err != nil {
		t.Fatalf("replaced bundle has an invalid signature: %s", b)
	}
	if entries, _ := os.ReadDir(installRoot); len(entries) != 1 {
		t.Errorf("leftovers next to the app: %v", entries)
	}
}

func TestDetectMethod(t *testing.T) {
	cfg := t.TempDir()
	dir := t.TempDir()
	bin := filepath.Join(dir, "NoxioFTP")
	os.WriteFile(bin, []byte("x"), 0o755)

	if m, why := detect(bin, "linux", cfg); m != MethodSelf {
		t.Errorf("plain install: %s (%s)", m, why)
	}

	// marker written by the install script, pointing at this very program
	os.WriteFile(filepath.Join(cfg, markerFile), []byte("script\n"+bin+"\n"), 0o644)
	if m, _ := detect(bin, "linux", cfg); m != MethodScript {
		t.Errorf("script install: %s", m)
	}
	// marker of another location (the user moved / reinstalled by hand): not trusted
	other := filepath.Join(t.TempDir(), "NoxioFTP")
	os.WriteFile(other, []byte("x"), 0o755)
	if m, _ := detect(other, "linux", cfg); m != MethodSelf {
		t.Errorf("marker for another path: %s", m)
	}

	// read-only folder: cannot replace itself
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := t.TempDir()
		p := filepath.Join(ro, "NoxioFTP")
		os.WriteFile(p, []byte("x"), 0o755)
		os.Chmod(ro, 0o555)
		defer os.Chmod(ro, 0o755)
		if m, why := detect(p, "linux", t.TempDir()); m != MethodManual || why == "" {
			t.Errorf("read-only folder: %s (%q)", m, why)
		}
	}

	// macOS specifics
	sep := string(filepath.Separator)
	if m, why := detect(dir+sep+"NoxioFTP", "darwin", t.TempDir()); m != MethodManual || why == "" {
		t.Errorf("not an app bundle: %s (%q)", m, why)
	}
	trans := sep + "private" + sep + "var" + sep + "AppTranslocation" + sep + "ABC" + sep + "d" + sep + "NoxioFTP.app" + sep + "Contents" + sep + "MacOS" + sep + "NoxioFTP"
	if m, why := detect(trans, "darwin", t.TempDir()); m != MethodManual || !strings.Contains(why, "Programme") {
		t.Errorf("translocated app: %s (%q)", m, why)
	}
	app := filepath.Join(t.TempDir(), "NoxioFTP.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	exe := filepath.Join(app, "Contents", "MacOS", "NoxioFTP")
	os.WriteFile(exe, []byte("x"), 0o755)
	if m, why := detect(exe, "darwin", t.TempDir()); m != MethodSelf {
		t.Errorf("app bundle: %s (%s)", m, why)
	}
}

func TestCommand(t *testing.T) {
	if c := Command("windows"); !strings.HasPrefix(c, "irm ") || !strings.HasSuffix(c, "install.ps1 | iex") {
		t.Errorf("windows: %s", c)
	}
	for _, os := range []string{"darwin", "linux"} {
		if c := Command(os); !strings.HasPrefix(c, "curl -fsSL ") || !strings.HasSuffix(c, "install.sh | sh") {
			t.Errorf("%s: %s", os, c)
		}
	}
}
