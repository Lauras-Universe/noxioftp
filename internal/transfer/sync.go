package transfer

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SyncOptions controls a one-way directory synchronisation.
type SyncOptions struct {
	// Direction Upload mirrors local -> remote, Download remote -> local.
	Direction Direction `json:"direction"`
	// DeleteExtra removes files/folders that exist only in the target.
	DeleteExtra bool `json:"deleteExtra"`
	// KeepNewer leaves files alone whose target copy is newer than the source.
	KeepNewer bool `json:"keepNewer"`
}

// SyncAction is one step of a plan.
type SyncAction struct {
	Kind   string `json:"kind"` // "mkdir", "copy" or "delete"
	Rel    string `json:"rel"`  // path relative to the synced directories, "/"-separated
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Size   int64  `json:"size"`
	IsDir  bool   `json:"isDir"`
	Reason string `json:"reason"` // "new", "newer", "size", "extra"
}

// SyncPlan is the preview of a synchronisation.
type SyncPlan struct {
	ID          string       `json:"id"`
	Direction   Direction    `json:"direction"`
	LocalDir    string       `json:"localDir"`
	RemoteDir   string       `json:"remoteDir"`
	Actions     []SyncAction `json:"actions"`
	CopyCount   int          `json:"copyCount"`
	DeleteCount int          `json:"deleteCount"`
	Bytes       int64        `json:"bytes"`
	Note        string       `json:"note"`
}

// mtimeTolerance absorbs the coarse / rounded timestamps of remote servers.
const mtimeTolerance = 2 * time.Second

func slash(s string) string { return strings.ReplaceAll(s, `\`, "/") }

func tree(ctx context.Context, f FS, root string) (map[string]entry, int, error) {
	items, skipped, err := walkTree(ctx, f, root)
	if err != nil {
		return nil, 0, err
	}
	m := make(map[string]entry, len(items))
	for _, it := range items {
		m[slash(it.rel)] = it
	}
	return m, skipped, nil
}

// PlanSync compares localDir and remoteDir and returns what a synchronisation
// would do. Nothing is changed.
func (q *Queue) PlanSync(ctx context.Context, localDir, remoteDir string, opts SyncOptions) (SyncPlan, error) {
	q.mu.Lock()
	local, remote := q.local, q.remote
	q.mu.Unlock()
	if remote == nil {
		return SyncPlan{}, errors.New("nicht verbunden")
	}
	if opts.Direction != Upload && opts.Direction != Download {
		return SyncPlan{}, errors.New("ungültige Richtung")
	}

	srcFS, dstFS, srcDir, dstDir := local, remote, localDir, remoteDir
	if opts.Direction == Download {
		srcFS, dstFS, srcDir, dstDir = remote, local, remoteDir, localDir
	}
	S, skippedS, err := tree(ctx, srcFS, srcDir)
	if err != nil {
		return SyncPlan{}, err
	}
	D := map[string]entry{}
	skippedD := 0
	if _, err := dstFS.Stat(dstDir); err == nil {
		if D, skippedD, err = tree(ctx, dstFS, dstDir); err != nil {
			return SyncPlan{}, err
		}
	}

	plan := SyncPlan{ID: newID(), Direction: opts.Direction, LocalDir: localDir, RemoteDir: remoteDir}
	join := func(f FS, root, rel string) string { return f.Join(root, rel) }

	keys := make([]string, 0, len(S))
	for k := range S {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, rel := range keys {
		s := S[rel]
		d, exists := D[rel]
		src, dst := join(srcFS, srcDir, rel), join(dstFS, dstDir, rel)
		switch {
		case s.dir:
			if !exists {
				plan.Actions = append(plan.Actions, SyncAction{Kind: "mkdir", Rel: rel, Dst: dst, IsDir: true, Reason: "new"})
			}
		case !exists:
			plan.Actions = append(plan.Actions, SyncAction{Kind: "copy", Rel: rel, Src: src, Dst: dst, Size: s.size, Reason: "new"})
		case d.dir:
			// type clash (file vs folder): never touched automatically
		case s.size != d.size || s.mod.After(d.mod.Add(mtimeTolerance)):
			reason := "newer"
			if s.size != d.size {
				reason = "size"
			}
			if opts.KeepNewer && d.mod.After(s.mod.Add(mtimeTolerance)) {
				continue
			}
			plan.Actions = append(plan.Actions, SyncAction{Kind: "copy", Rel: rel, Src: src, Dst: dst, Size: s.size, Reason: reason})
		}
	}

	if opts.DeleteExtra {
		extra := make([]string, 0)
		for k := range D {
			if _, ok := S[k]; !ok {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		var deleted []string
		for _, rel := range extra {
			covered := false
			for _, dir := range deleted {
				if strings.HasPrefix(rel, dir+"/") {
					covered = true
					break
				}
			}
			if covered {
				continue
			}
			d := D[rel]
			if d.dir {
				deleted = append(deleted, rel)
			}
			plan.Actions = append(plan.Actions, SyncAction{Kind: "delete", Rel: rel, Dst: join(dstFS, dstDir, rel), IsDir: d.dir, Size: d.size, Reason: "extra"})
		}
	}

	for _, a := range plan.Actions {
		switch a.Kind {
		case "copy":
			plan.CopyCount++
			plan.Bytes += a.Size
		case "delete":
			plan.DeleteCount++
		}
	}
	if n := skippedS + skippedD; n > 0 {
		plan.Note = fmt.Sprintf("%d Verknüpfung(en)/Sonderdateien wurden ignoriert", n)
	}
	return plan, nil
}

// ApplySync executes a plan: folders are created, files are queued as normal
// transfers (so they show up with progress) and extra items are deleted.
// It returns how many transfers were queued.
func (q *Queue) ApplySync(plan SyncPlan) (int, error) {
	q.mu.Lock()
	local, remote := q.local, q.remote
	q.mu.Unlock()
	if remote == nil {
		return 0, errors.New("nicht verbunden")
	}
	dstFS := remote
	if plan.Direction == Download {
		dstFS = local
	}
	queued := 0
	for _, a := range plan.Actions {
		if a.Kind == "mkdir" {
			if err := dstFS.MkdirAll(a.Dst); err != nil {
				return queued, fmt.Errorf("%s: %w", a.Rel, err)
			}
		}
	}
	for _, a := range plan.Actions {
		if a.Kind == "copy" {
			_, name := dstFS.Split(a.Dst)
			q.Enqueue(plan.Direction, name, a.Src, a.Dst, Overwrite)
			queued++
		}
	}
	for _, a := range plan.Actions {
		if a.Kind == "delete" {
			var err error
			if a.IsDir {
				err = dstFS.RemoveAll(a.Dst)
			} else {
				err = dstFS.Remove(a.Dst)
			}
			if err != nil {
				return queued, fmt.Errorf("%s: %w", a.Rel, err)
			}
		}
	}
	return queued, nil
}
