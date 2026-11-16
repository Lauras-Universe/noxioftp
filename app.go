package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sftp-client/internal/connections"
	"sftp-client/internal/fsinfo"
	"sftp-client/internal/localfs"
	"sftp-client/internal/sftpclient"
	"sftp-client/internal/transfer"
)

// App is bound to the frontend: every exported method is callable from JS.
type App struct {
	ctx      context.Context
	store    *connections.Store
	hostKeys *connections.HostKeys
	queue    *transfer.Queue

	mu      sync.Mutex
	client  *sftpclient.Client
	cfg     sftpclient.Config // kept for automatic reconnect
	profile connections.Profile
}

// NewApp creates the application.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// SFTP_CLIENT_CONFIG_DIR overrides the config location (portable use, tests).
	dir := os.Getenv("SFTP_CLIENT_CONFIG_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = "."
		}
		dir = filepath.Join(base, "sftp-client")
	}
	var err error

	a.store, err = connections.NewStore(filepath.Join(dir, "connections.json"), connections.KeyringSecrets{})
	if err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type: runtime.ErrorDialog, Title: "Fehler", Message: err.Error(),
		})
		a.store, _ = connections.NewStore(filepath.Join(os.TempDir(), "sftp-client-connections.json"), connections.KeyringSecrets{})
	}

	a.hostKeys = connections.NewHostKeys(filepath.Join(dir, "known_hosts"))
	a.hostKeys.Confirm = func(host, keyType, fingerprint string) bool {
		res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.QuestionDialog,
			Title:         "Unbekannter Server",
			Message:       fmt.Sprintf("Die Echtheit von %s kann nicht geprüft werden.\n\n%s-Fingerabdruck:\n%s\n\nDiesem Server vertrauen und die Verbindung fortsetzen?", host, keyType, fingerprint),
			Buttons:       []string{"Vertrauen", "Abbrechen"},
			DefaultButton: "Abbrechen",
			CancelButton:  "Abbrechen",
		})
		return err == nil && (res == "Vertrauen" || res == "Yes")
	}

	a.queue = transfer.NewQueue(3, transfer.LocalFS{}, func(j transfer.Job) {
		runtime.EventsEmit(ctx, "transfer:update", j)
	})
}

func (a *App) shutdown(_ context.Context) {
	a.queue.CancelAll()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		a.client.Close()
	}
}

// ---------------------------------------------------------------- profiles

func (a *App) ListProfiles() []connections.Profile { return a.store.List() }

// SaveProfile creates or updates a profile. An empty secret keeps the stored one.
func (a *App) SaveProfile(p connections.Profile, secret string) (connections.Profile, error) {
	return a.store.Save(p, secret)
}

func (a *App) DeleteProfile(id string) error { return a.store.Delete(id) }

// PickKeyFile opens a file dialog for a private key.
func (a *App) PickKeyFile() (string, error) {
	start, _ := os.UserHomeDir()
	if start != "" {
		start = filepath.Join(start, ".ssh")
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                      "Private-Key-Datei wählen",
		DefaultDirectory:           start,
		ShowHiddenFiles:            true,
		TreatPackagesAsDirectories: true,
	})
}

// ------------------------------------------------------------- connection

// ConnectResult tells the frontend where the session starts.
type ConnectResult struct {
	Home string `json:"home"`
}

// Connect opens a connection for a saved profile. secret may be empty to use
// the one in the keyring.
func (a *App) Connect(profileID, secret string) (ConnectResult, error) {
	p, ok := a.store.Get(profileID)
	if !ok {
		return ConnectResult{}, errors.New("Profil nicht gefunden")
	}
	if secret == "" {
		var err error
		if secret, err = a.store.Secret(profileID); err != nil {
			return ConnectResult{}, fmt.Errorf("Schlüsselbund: %w", err)
		}
	}
	cfg := sftpclient.Config{
		Host:            p.Host,
		Port:            p.Port,
		User:            p.User,
		HostKeyCallback: a.hostKeys.Callback(),
	}
	if p.AuthType == connections.AuthKey {
		cfg.KeyPath, cfg.Passphrase = p.KeyPath, secret
	} else {
		cfg.Password = secret
	}

	cl, err := sftpclient.Dial(cfg)
	if err != nil {
		return ConnectResult{}, err
	}
	home := p.RemoteDir
	if home == "" {
		if home, err = cl.Home(); err != nil {
			home = "/"
		}
	}

	a.mu.Lock()
	old := a.client
	a.client, a.cfg, a.profile = cl, cfg, p
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
	a.attach(cl)
	return ConnectResult{Home: home}, nil
}

// attach wires a fresh client into the queue and watches for connection loss.
func (a *App) attach(cl *sftpclient.Client) {
	a.queue.SetRemote(transfer.RemoteFS{C: cl.SFTP()})
	go func() {
		<-cl.Done()
		a.mu.Lock()
		current := a.client == cl
		a.mu.Unlock()
		if current {
			runtime.EventsEmit(a.ctx, "connection:lost")
		}
	}()
}

// Disconnect closes the current connection.
func (a *App) Disconnect() {
	a.queue.CancelAll()
	a.queue.SetRemote(nil)
	a.mu.Lock()
	cl := a.client
	a.client = nil
	a.mu.Unlock()
	if cl != nil {
		cl.Close()
	}
}

func (a *App) IsConnected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client != nil && !a.client.Closed()
}

// cli returns the live client, reconnecting once if the connection was lost.
func (a *App) cli() (*sftpclient.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil, errors.New("E_NOT_CONNECTED: Nicht verbunden")
	}
	if !a.client.Closed() {
		return a.client, nil
	}
	cl, err := sftpclient.Dial(a.cfg)
	if err != nil {
		return nil, fmt.Errorf("E_NOT_CONNECTED: Verbindung verloren, erneutes Verbinden fehlgeschlagen: %w", err)
	}
	a.client = cl
	a.attach(cl)
	return cl, nil
}

// ------------------------------------------------------------------ local

func (a *App) LocalHome() (string, error)                   { return localfs.Home() }
func (a *App) LocalRoots() []string                         { return localfs.Roots() }
func (a *App) LocalList(dir string) (fsinfo.Listing, error) { return localfs.List(dir) }
func (a *App) LocalMkdir(p string) error                    { return localfs.Mkdir(p) }
func (a *App) LocalRename(from, to string) error            { return localfs.Rename(from, to) }
func (a *App) LocalDelete(paths []string) error             { return localfs.Delete(paths) }

// ----------------------------------------------------------------- remote

func (a *App) RemoteList(dir string) (fsinfo.Listing, error) {
	cl, err := a.cli()
	if err != nil {
		return fsinfo.Listing{}, err
	}
	return cl.List(dir)
}

func (a *App) RemoteMkdir(p string) error {
	cl, err := a.cli()
	if err != nil {
		return err
	}
	return cl.Mkdir(p)
}

func (a *App) RemoteRename(from, to string) error {
	cl, err := a.cli()
	if err != nil {
		return err
	}
	return cl.Rename(from, to)
}

func (a *App) RemoteDelete(paths []string) error {
	cl, err := a.cli()
	if err != nil {
		return err
	}
	return cl.Delete(paths)
}

// RemoteChmod sets permissions from an octal string such as "644".
func (a *App) RemoteChmod(p, octal string) error {
	mode, err := strconv.ParseUint(octal, 8, 32)
	if err != nil || mode > 0o7777 {
		return errors.New("Ungültige Rechte (erwartet z. B. 644)")
	}
	cl, err := a.cli()
	if err != nil {
		return err
	}
	return cl.Chmod(p, uint32(mode))
}

// ----------------------------------------------------------------- editor

func (a *App) RemoteReadText(p string) (sftpclient.TextFile, error) {
	cl, err := a.cli()
	if err != nil {
		return sftpclient.TextFile{}, err
	}
	return cl.ReadText(p)
}

func (a *App) RemoteWriteText(p, content string, expectedMod int64, force bool) (int64, error) {
	cl, err := a.cli()
	if err != nil {
		return 0, err
	}
	return cl.WriteText(p, content, expectedMod, force)
}

// -------------------------------------------------------------- transfers

func (a *App) fsPair(direction string) (srcName func(string) string, dstFS transfer.FS, err error) {
	switch transfer.Direction(direction) {
	case transfer.Upload:
		cl, err := a.cli()
		if err != nil {
			return nil, nil, err
		}
		return filepath.Base, transfer.RemoteFS{C: cl.SFTP()}, nil
	case transfer.Download:
		if _, err := a.cli(); err != nil {
			return nil, nil, err
		}
		return path.Base, transfer.LocalFS{}, nil
	}
	return nil, nil, errors.New("ungültige Richtung")
}

// CheckConflicts returns the names of sources that already exist in dstDir.
func (a *App) CheckConflicts(direction string, srcPaths []string, dstDir string) ([]string, error) {
	base, dstFS, err := a.fsPair(direction)
	if err != nil {
		return nil, err
	}
	var existing []string
	for _, s := range srcPaths {
		name := base(s)
		if _, err := dstFS.Stat(dstFS.Join(dstDir, name)); err == nil {
			existing = append(existing, name)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return existing, nil
}

// EnqueueTransfers queues uploads or downloads into dstDir.
func (a *App) EnqueueTransfers(direction string, srcPaths []string, dstDir, policy string) error {
	base, dstFS, err := a.fsPair(direction)
	if err != nil {
		return err
	}
	pol := transfer.Policy(policy)
	switch pol {
	case transfer.Overwrite, transfer.Skip, transfer.Rename:
	case "":
		pol = transfer.Overwrite
	default:
		return errors.New("ungültige Konfliktregel")
	}
	for _, s := range srcPaths {
		name := base(s)
		a.queue.Enqueue(transfer.Direction(direction), name, s, dstFS.Join(dstDir, name), pol)
	}
	return nil
}

func (a *App) ListTransfers() []transfer.Job { return a.queue.List() }
func (a *App) CancelTransfer(id string)      { a.queue.Cancel(id) }
func (a *App) RetryTransfer(id string)       { a.queue.Retry(id) }
func (a *App) ClearFinishedTransfers()       { a.queue.ClearFinished() }
