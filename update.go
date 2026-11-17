package main

import (
	"errors"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"noxioftp/internal/update"
)

// version is set at build time: wails build -ldflags "-X main.version=1.2.3".
// Builds without it are "dev" and never check for updates.
var version = "dev"

func currentVersion() string {
	if v := os.Getenv("NOXIOFTP_VERSION"); v != "" { // for testing the update flow
		return v
	}
	return version
}

// AppVersion returns the version of the running program.
func (a *App) AppVersion() string { return currentVersion() }

// CheckForUpdate asks GitHub whether a newer release exists. With auto set
// (the check at program start) NOXIOFTP_NO_UPDATE_CHECK=1 turns it off.
func (a *App) CheckForUpdate(auto bool) (update.Info, error) {
	if auto && os.Getenv("NOXIOFTP_NO_UPDATE_CHECK") != "" {
		return update.Info{Current: currentVersion()}, nil
	}
	info, rel, err := update.Check(a.ctx, currentVersion())
	a.mu.Lock()
	a.pending = rel
	a.mu.Unlock()
	return info, err
}

// UpdateProgress is sent as the "update:progress" event while downloading.
type UpdateProgress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

// ApplyUpdate downloads the release found by CheckForUpdate, replaces the
// installed program and restarts it. Only works when the program can replace
// itself (see update.Info.Method).
func (a *App) ApplyUpdate() error {
	a.mu.Lock()
	rel, dirty := a.pending, a.dirty
	a.mu.Unlock()
	switch {
	case rel == nil:
		return errors.New("Bitte zuerst nach Updates suchen")
	case dirty:
		return errors.New("Im Editor gibt es ungespeicherte Änderungen. Bitte erst speichern oder schließen")
	case a.queue.ActiveCount() > 0:
		return errors.New("Es laufen noch Übertragungen. Bitte warten oder abbrechen")
	}

	path, err := update.Apply(a.ctx, rel, func(done, total int64) {
		runtime.EventsEmit(a.ctx, "update:progress", UpdateProgress{Done: done, Total: total})
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.pending = nil
	a.mu.Unlock()
	if err := update.Relaunch(path); err != nil {
		return errors.New("Das Update ist installiert, das Programm konnte aber nicht neu gestartet werden. Bitte manuell starten: " + err.Error())
	}
	go func() {
		time.Sleep(700 * time.Millisecond) // let the UI show "restarting"
		runtime.Quit(a.ctx)
	}()
	return nil
}
