// Package update checks GitHub for a newer release and, where the installation
// allows it, replaces the running program with it.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"noxioftp/internal/appinfo"
)

// Repo is the GitHub repository releases are taken from.
const Repo = "Lauras-Universe/noxioftp"

// APIBase is the GitHub API root. Tests (and NOXIOFTP_UPDATE_API) point it
// at a local server; the asset URL check is skipped for non-default values.
var APIBase = "https://api.github.com"

// Install methods reported to the frontend.
const (
	MethodSelf   = "self"   // this program can replace itself
	MethodScript = "script" // installed with the install script: run it again
	MethodManual = "manual" // cannot replace itself here: run the install command or download manually
)

// ErrDevBuild is returned for builds without a release version.
var ErrDevBuild = errors.New("Entwicklungsversion: keine Update-Prüfung")

// Release is a published GitHub release.
type Release struct {
	Tag     string
	Version string // tag without the leading "v"
	Notes   string
	PageURL string
	assets  map[string]string // name -> download URL
}

// Info is what the frontend gets after a check.
type Info struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
	PageURL   string `json:"pageUrl"`
	Method    string `json:"method"`
	Command   string `json:"command"` // install command to run again
	Reason    string `json:"reason"`  // why the program cannot update itself
}

// AssetName is the release file for an operating system.
func AssetName(goos string) string {
	switch goos {
	case "darwin":
		return "noxioftp-macos.zip"
	case "windows":
		return "noxioftp-windows.zip"
	default:
		return "noxioftp-linux.tar.gz"
	}
}

const sumsName = "sha256sums.txt"

// Command returns the terminal command that installs the latest release.
func Command(goos string) string {
	base := "https://raw.githubusercontent.com/" + Repo + "/main/scripts/"
	if goos == "windows" {
		return "irm " + base + "install.ps1 | iex"
	}
	return "curl -fsSL " + base + "install.sh | sh"
}

// ------------------------------------------------------------------ versions

func parseVersion(v string) (nums [3]int, pre string, err error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		if v[i] == '+' {
			pre = "" // build metadata does not make a pre-release
		}
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return nums, "", fmt.Errorf("ungültige Version %q", v)
	}
	for i, p := range parts {
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 {
			return nums, "", fmt.Errorf("ungültige Version %q", v)
		}
		nums[i] = n
	}
	return nums, pre, nil
}

// Newer reports whether version a is newer than b.
func Newer(a, b string) (bool, error) {
	an, ap, err := parseVersion(a)
	if err != nil {
		return false, err
	}
	bn, bp, err := parseVersion(b)
	if err != nil {
		return false, err
	}
	for i := range an {
		if an[i] != bn[i] {
			return an[i] > bn[i], nil
		}
	}
	switch {
	case ap == bp:
		return false, nil
	case ap == "": // release beats its own pre-release
		return true, nil
	case bp == "":
		return false, nil
	default:
		return ap > bp, nil
	}
}

// -------------------------------------------------------------------- lookup

type ghRelease struct {
	Tag     string `json:"tag_name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest fetches the newest published release.
func Latest(ctx context.Context) (*Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", appinfo.ID+"-update-check")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("Noch kein Release veröffentlicht")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub antwortete mit %s", resp.Status)
	}
	var g ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&g); err != nil {
		return nil, err
	}
	rel := &Release{Tag: g.Tag, Version: strings.TrimPrefix(g.Tag, "v"), PageURL: g.HTMLURL, assets: map[string]string{}}
	rel.Notes = g.Body
	if r := []rune(rel.Notes); len(r) > 2000 {
		rel.Notes = string(r[:2000]) + " …"
	}
	for _, a := range g.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}

// Check compares the current version with the latest release.
func Check(ctx context.Context, current string) (Info, *Release, error) {
	info := Info{Current: current, Command: Command(runtime.GOOS)}
	if current == "" || current == "dev" {
		return info, nil, ErrDevBuild
	}
	rel, err := Latest(ctx)
	if err != nil {
		return info, nil, err
	}
	info.Latest, info.Notes, info.PageURL = rel.Version, rel.Notes, rel.PageURL
	newer, err := Newer(rel.Version, current)
	if err != nil {
		return info, nil, err
	}
	if !newer {
		return info, nil, nil
	}
	info.Available = true
	info.Method, info.Reason = DetectMethod()
	return info, rel, nil
}

// ------------------------------------------------------------------ download

// Progress reports download progress.
type Progress func(done, total int64)

func (r *Release) trustedURL(u string) bool {
	if APIBase != "https://api.github.com" { // tests
		return true
	}
	return strings.HasPrefix(u, "https://github.com/"+Repo+"/releases/download/")
}

func fetch(ctx context.Context, url string, w io.Writer, progress Progress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", appinfo.ID+"-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Download fehlgeschlagen: %s", resp.Status)
	}
	var done int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// download saves the release file for goos into dir and verifies its SHA-256
// against the release's sha256sums.txt. A release without checksums is refused.
func (r *Release) download(ctx context.Context, goos, dir string, progress Progress) (string, error) {
	name := AssetName(goos)
	url, ok := r.assets[name]
	if !ok {
		return "", fmt.Errorf("Das Release enthält keine Datei %s", name)
	}
	sumsURL, ok := r.assets[sumsName]
	if !ok {
		return "", fmt.Errorf("Das Release enthält keine %s – Update aus Sicherheitsgründen abgebrochen", sumsName)
	}
	if !r.trustedURL(url) || !r.trustedURL(sumsURL) {
		return "", errors.New("Unerwartete Download-Adresse")
	}

	var sums strings.Builder
	if err := fetch(ctx, sumsURL, &sums, nil); err != nil {
		return "", err
	}
	want := ""
	for _, line := range strings.Split(sums.String(), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return "", fmt.Errorf("%s enthält keine Prüfsumme für %s", sumsName, name)
	}

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	err = fetch(ctx, url, io.MultiWriter(f, h), progress)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(path)
		return "", errors.New("Prüfsumme stimmt nicht überein – Datei verworfen")
	}
	return path, nil
}
