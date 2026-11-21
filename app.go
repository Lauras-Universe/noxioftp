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
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"noxioftp/internal/appinfo"
	"noxioftp/internal/connections"
	"noxioftp/internal/fsinfo"
	"noxioftp/internal/ftpclient"
	"noxioftp/internal/localfs"
	"noxioftp/internal/remote"
	"noxioftp/internal/sftpclient"
	"noxioftp/internal/transfer"
	"noxioftp/internal/update"
)

// App is bound to the frontend: every exported method is callable from JS.
type App struct {
	ctx      context.Context
	store    *connections.Store
	hostKeys *connections.HostKeys
	certPins *connections.CertPins
	queue    *transfer.Queue

	mu      sync.Mutex
	client  remote.Session
	redial  func() (remote.Session, error) // reconnects with the same settings
	profile connections.Profile
	dirty   bool // unsaved editor tabs

	plans   []transfer.SyncPlan // recent sync previews, newest last
	pending *update.Release     // release found by the last update check
}

// NewApp creates the application.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if api := os.Getenv("NOXIOFTP_UPDATE_API"); api != "" { // for testing the update flow
		update.APIBase = api
	}
	go update.CleanupOld() // leftover of the previous update (Windows)

	appinfo.MigrateLegacyConfig() // settings of the first, unnamed versions
	dir := appinfo.ConfigDir()
	if dir == "" {
		dir = "."
	}
	var err error

	a.store, err = connections.NewStore(filepath.Join(dir, "connections.json"), connections.KeyringSecrets{})
	if err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type: runtime.ErrorDialog, Title: "Fehler", Message: err.Error(),
		})
		a.store, _ = connections.NewStore(filepath.Join(os.TempDir(), "noxioftp-connections.json"), connections.KeyringSecrets{})
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

	a.hostKeys.ConfirmChanged = func(host string, old []string, now string) bool {
		res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:  runtime.WarningDialog,
			Title: "Host-Schlüssel hat sich geändert",
			Message: fmt.Sprintf("ACHTUNG: Der Server %s meldet einen anderen Schlüssel als beim letzten Mal.\n\n"+
				"Gespeichert:\n%s\n\nJetzt:\n%s\n\n"+
				"Das passiert, wenn der Server neu aufgesetzt wurde. Es kann aber auch ein Angriff sein (Man-in-the-Middle).\n"+
				"Nur fortfahren, wenn der Betreiber oder Hoster den neuen Fingerabdruck bestätigt hat, am besten über einen anderen Weg als diese Verbindung.",
				host, strings.Join(old, "\n"), now),
			Buttons:       []string{"Abbrechen", "Neuen Schlüssel vertrauen"},
			DefaultButton: "Abbrechen",
			CancelButton:  "Abbrechen",
		})
		return err == nil && res == "Neuen Schlüssel vertrauen"
	}

	a.certPins = connections.NewCertPins(filepath.Join(dir, "known_certs"))
	a.certPins.Confirm = func(host, fingerprint, verifyErr string) bool {
		res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.QuestionDialog,
			Title:         "Unbekanntes Zertifikat",
			Message:       fmt.Sprintf("Das TLS-Zertifikat von %s ist nicht von einer bekannten Stelle beglaubigt:\n%s\n\nSHA-256-Fingerabdruck:\n%s\n\nDiesem Zertifikat vertrauen und fortfahren?", host, verifyErr, fingerprint),
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

// sftpConfig builds the SSH settings for a profile, including its jump hosts.
// Secrets of jump hosts must be stored in the keyring (there is no prompt).
func (a *App) sftpConfig(p connections.Profile, secret string, visited map[string]bool) (sftpclient.Config, error) {
	if visited[p.ID] {
		return sftpclient.Config{}, sftpclient.ErrJumpDepth
	}
	visited[p.ID] = true
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
	if p.JumpID != "" {
		jp, ok := a.store.Get(p.JumpID)
		if !ok {
			return cfg, errors.New("Jump-Host-Profil nicht gefunden")
		}
		jsecret, err := a.store.Secret(jp.ID)
		if err != nil {
			return cfg, fmt.Errorf("Schlüsselbund: %w", err)
		}
		if jp.AuthType == connections.AuthPassword && jsecret == "" {
			return cfg, fmt.Errorf("Für den Jump-Host „%s“ muss das Passwort im Profil gespeichert sein", jp.Name)
		}
		jc, err := a.sftpConfig(jp, jsecret, visited)
		if err != nil {
			return cfg, err
		}
		cfg.Jump = &jc
	}
	return cfg, nil
}

// dialer returns a function that opens a session for the profile.
func (a *App) dialer(p connections.Profile, secret string) (func() (remote.Session, error), error) {
	if p.Protocol == connections.ProtoSFTP || p.Protocol == "" {
		cfg, err := a.sftpConfig(p, secret, map[string]bool{})
		if err != nil {
			return nil, err
		}
		return func() (remote.Session, error) {
			cl, err := sftpclient.Dial(cfg)
			if err != nil {
				return nil, err
			}
			return cl, nil
		}, nil
	}
	cfg := ftpclient.Config{Host: p.Host, Port: p.Port, User: p.User, Password: secret, Mode: ftpclient.Plain}
	switch p.Protocol {
	case connections.ProtoFTPS:
		cfg.Mode, cfg.TLS = ftpclient.Explicit, a.certPins.TLSConfig(p.Host)
	case connections.ProtoFTPSImplicit:
		cfg.Mode, cfg.TLS = ftpclient.Implicit, a.certPins.TLSConfig(p.Host)
	}
	return func() (remote.Session, error) {
		cl, err := ftpclient.Dial(cfg)
		if err != nil {
			return nil, err
		}
		return cl, nil
	}, nil
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
	dial, err := a.dialer(p, secret)
	if err != nil {
		return ConnectResult{}, err
	}
	cl, err := dial()
	if err != nil {
		return ConnectResult{}, err
	}
	home := p.RemoteDir
	if home == "" {
		if home, err = cl.Home(); err != nil || home == "" {
			home = "/"
		}
	}

	a.mu.Lock()
	old := a.client
	a.client, a.redial, a.profile = cl, dial, p
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
	a.attach(cl)
	return ConnectResult{Home: home}, nil
}

// attach wires a fresh session into the queue and watches for connection loss.
func (a *App) attach(cl remote.Session) {
	a.queue.SetRemote(cl.FS())
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
	a.client, a.redial = nil, nil
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

// cli returns the live session, reconnecting once if the connection was lost.
func (a *App) cli() (remote.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil, errors.New("E_NOT_CONNECTED: Nicht verbunden")
	}
	if !a.client.Closed() {
		return a.client, nil
	}
	cl, err := a.redial()
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

func (a *App) RemoteReadText(p string) (remote.TextFile, error) {
	cl, err := a.cli()
	if err != nil {
		return remote.TextFile{}, err
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
		return filepath.Base, cl.FS(), nil
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

// ------------------------------------------------------------------ move

func checkMoveTarget(src, dstDir string, sep string) error {
	if dstDir == src || strings.HasPrefix(dstDir, strings.TrimSuffix(src, sep)+sep) {
		return errors.New("Ein Ordner kann nicht in sich selbst verschoben werden")
	}
	return nil
}

// LocalMove moves local files/folders into dstDir. Existing targets are never overwritten.
func (a *App) LocalMove(paths []string, dstDir string) error {
	for _, src := range paths {
		to := filepath.Join(dstDir, filepath.Base(src))
		if filepath.Clean(to) == filepath.Clean(src) {
			continue
		}
		if err := checkMoveTarget(filepath.Clean(src), filepath.Clean(dstDir), string(filepath.Separator)); err != nil {
			return err
		}
		if err := localfs.Rename(src, to); err != nil {
			return err
		}
	}
	return nil
}

// RemoteMove moves remote files/folders into dstDir. Existing targets are never overwritten.
func (a *App) RemoteMove(paths []string, dstDir string) error {
	cl, err := a.cli()
	if err != nil {
		return err
	}
	for _, src := range paths {
		to := path.Join(dstDir, path.Base(src))
		if path.Clean(to) == path.Clean(src) {
			continue
		}
		if err := checkMoveTarget(path.Clean(src), path.Clean(dstDir), "/"); err != nil {
			return err
		}
		if err := cl.Rename(src, to); err != nil {
			return err
		}
	}
	return nil
}

// ----------------------------------------------------------------- closing

// SetDirty tells the backend whether the editor has unsaved changes.
func (a *App) SetDirty(dirty bool) {
	a.mu.Lock()
	a.dirty = dirty
	a.mu.Unlock()
}

// beforeClose asks for confirmation when closing would lose work.
// Returning true prevents the window from closing.
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	dirty := a.dirty
	a.mu.Unlock()
	active := a.queue.ActiveCount()
	if !dirty && active == 0 {
		return false
	}
	var reasons []string
	if dirty {
		reasons = append(reasons, "Im Editor gibt es ungespeicherte Änderungen.")
	}
	if active > 0 {
		reasons = append(reasons, fmt.Sprintf("%d Übertragung(en) laufen noch oder warten und würden abgebrochen.", active))
	}
	res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Programm wirklich beenden?",
		Message:       strings.Join(reasons, "\n"),
		Buttons:       []string{"Beenden", "Abbrechen"},
		DefaultButton: "Abbrechen",
		CancelButton:  "Abbrechen",
	})
	return !(err == nil && (res == "Beenden" || res == "Yes"))
}

// -------------------------------------------------------------------- sync

// PlanSync compares the two directories and returns a preview. Nothing is changed.
func (a *App) PlanSync(localDir, remoteDir string, opts transfer.SyncOptions) (transfer.SyncPlan, error) {
	if _, err := a.cli(); err != nil {
		return transfer.SyncPlan{}, err
	}
	plan, err := a.queue.PlanSync(a.ctx, localDir, remoteDir, opts)
	if err != nil {
		return plan, err
	}
	a.mu.Lock()
	a.plans = append(a.plans, plan)
	if len(a.plans) > 5 {
		a.plans = a.plans[1:]
	}
	a.mu.Unlock()
	return plan, nil
}

// RunSync executes a previously previewed plan and returns the number of queued transfers.
func (a *App) RunSync(planID string) (int, error) {
	if _, err := a.cli(); err != nil {
		return 0, err
	}
	a.mu.Lock()
	var plan *transfer.SyncPlan
	for i := range a.plans {
		if a.plans[i].ID == planID {
			p := a.plans[i]
			plan = &p
			a.plans = append(a.plans[:i], a.plans[i+1:]...)
			break
		}
	}
	a.mu.Unlock()
	if plan == nil {
		return 0, errors.New("Die Vorschau ist abgelaufen, bitte erneut vergleichen")
	}
	return a.queue.ApplySync(*plan)
}
