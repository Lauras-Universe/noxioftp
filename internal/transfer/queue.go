// Package transfer implements the upload/download queue.
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"time"
)

type Direction string

const (
	Upload   Direction = "upload"
	Download Direction = "download"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Done      Status = "done"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
	Skipped   Status = "skipped"
)

// Policy decides what happens when the destination already exists.
type Policy string

const (
	Overwrite Policy = "overwrite"
	Skip      Policy = "skip"
	Rename    Policy = "rename"
)

// Job is one queued transfer of a file or a whole directory.
type Job struct {
	ID        string    `json:"id"`
	Direction Direction `json:"direction"`
	Name      string    `json:"name"`
	Src       string    `json:"src"`
	Dst       string    `json:"dst"`
	IsDir     bool      `json:"isDir"`
	Status    Status    `json:"status"`
	Done      int64     `json:"done"`
	Total     int64     `json:"total"`
	Error     string    `json:"error"`
	Note      string    `json:"note"` // e.g. skipped links
	Policy    Policy    `json:"policy"`
}

var errSkipped = errors.New("übersprungen")

// Queue runs jobs with a limited number of parallel workers, in FIFO order.
type Queue struct {
	mu       sync.Mutex
	jobs     []*Job
	cancels  map[string]context.CancelFunc
	running  int
	workers  int
	local    FS
	remote   FS
	notify   func(Job)
	lastEmit map[string]time.Time
}

// NewQueue creates a queue. notify is called (from worker goroutines) on every
// state change and, throttled, on progress.
func NewQueue(workers int, local FS, notify func(Job)) *Queue {
	if workers < 1 {
		workers = 1
	}
	return &Queue{
		workers:  workers,
		local:    local,
		notify:   notify,
		cancels:  map[string]context.CancelFunc{},
		lastEmit: map[string]time.Time{},
	}
}

// SetRemote sets (or clears with nil) the remote file system.
func (q *Queue) SetRemote(r FS) {
	q.mu.Lock()
	q.remote = r
	q.mu.Unlock()
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Enqueue adds a job and returns it.
func (q *Queue) Enqueue(dir Direction, name, src, dst string, p Policy) Job {
	if p == "" {
		p = Overwrite
	}
	j := &Job{ID: newID(), Direction: dir, Name: name, Src: src, Dst: dst, Status: Queued, Policy: p}
	q.mu.Lock()
	q.jobs = append(q.jobs, j)
	snap := *j
	q.mu.Unlock()
	q.emit(snap, true)
	q.schedule()
	return snap
}

// List returns a snapshot of all jobs.
func (q *Queue) List() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, len(q.jobs))
	for i, j := range q.jobs {
		out[i] = *j
	}
	return out
}

// Cancel cancels a queued or running job.
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	var snap *Job
	for _, j := range q.jobs {
		if j.ID != id {
			continue
		}
		switch j.Status {
		case Queued:
			j.Status = Cancelled
			s := *j
			snap = &s
		case Running:
			if c := q.cancels[id]; c != nil {
				c()
			}
		}
	}
	q.mu.Unlock()
	if snap != nil {
		q.emit(*snap, true)
	}
}

// ActiveCount returns the number of queued and running jobs.
func (q *Queue) ActiveCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, j := range q.jobs {
		if j.Status == Queued || j.Status == Running {
			n++
		}
	}
	return n
}

// CancelAll cancels everything that has not finished.
func (q *Queue) CancelAll() {
	for _, j := range q.List() {
		if j.Status == Queued || j.Status == Running {
			q.Cancel(j.ID)
		}
	}
}

// Retry puts a failed, cancelled or skipped job back into the queue.
func (q *Queue) Retry(id string) {
	q.mu.Lock()
	var snap *Job
	for _, j := range q.jobs {
		if j.ID == id && (j.Status == Failed || j.Status == Cancelled || j.Status == Skipped) {
			j.Status, j.Done, j.Total, j.Error = Queued, 0, 0, ""
			s := *j
			snap = &s
		}
	}
	q.mu.Unlock()
	if snap != nil {
		q.emit(*snap, true)
		q.schedule()
	}
}

// ClearFinished removes finished jobs from the list.
func (q *Queue) ClearFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.jobs[:0]
	for _, j := range q.jobs {
		if j.Status == Queued || j.Status == Running {
			kept = append(kept, j)
		} else {
			delete(q.lastEmit, j.ID)
		}
	}
	q.jobs = kept
}

// schedule starts queued jobs while worker slots are free.
func (q *Queue) schedule() {
	for {
		q.mu.Lock()
		if q.running >= q.workers {
			q.mu.Unlock()
			return
		}
		var j *Job
		for _, c := range q.jobs {
			if c.Status == Queued {
				j = c
				break
			}
		}
		if j == nil {
			q.mu.Unlock()
			return
		}
		j.Status = Running
		q.running++
		ctx, cancel := context.WithCancel(context.Background())
		q.cancels[j.ID] = cancel
		local, remote := q.local, q.remote
		snap := *j
		q.mu.Unlock()

		q.emit(snap, true)
		go q.run(ctx, j, local, remote)
	}
}

func (q *Queue) run(ctx context.Context, j *Job, local, remote FS) {
	err := q.execute(ctx, j, local, remote)

	q.mu.Lock()
	switch {
	case ctx.Err() != nil:
		j.Status = Cancelled
	case errors.Is(err, errSkipped):
		j.Status = Skipped
	case err != nil:
		j.Status, j.Error = Failed, err.Error()
	default:
		j.Status = Done
		j.Done = j.Total
	}
	if cancel := q.cancels[j.ID]; cancel != nil {
		cancel()
		delete(q.cancels, j.ID)
	}
	q.running--
	snap := *j
	q.mu.Unlock()

	q.emit(snap, true)
	q.schedule()
}

func (q *Queue) emit(j Job, force bool) {
	if q.notify == nil {
		return
	}
	if !force {
		q.mu.Lock()
		last := q.lastEmit[j.ID]
		if time.Since(last) < 100*time.Millisecond {
			q.mu.Unlock()
			return
		}
		q.lastEmit[j.ID] = time.Now()
		q.mu.Unlock()
	}
	q.notify(j)
}

func (q *Queue) addBytes(j *Job, n int64) {
	q.mu.Lock()
	j.Done += n
	snap := *j
	q.mu.Unlock()
	q.emit(snap, false)
}

type entry struct {
	rel  string // path relative to the job root, "" for a single file
	size int64
	mod  time.Time
	dir  bool
}

func (q *Queue) execute(ctx context.Context, j *Job, local, remote FS) error {
	var srcFS, dstFS FS
	if j.Direction == Upload {
		srcFS, dstFS = local, remote
	} else {
		srcFS, dstFS = remote, local
	}
	if srcFS == nil || dstFS == nil {
		return errors.New("nicht verbunden")
	}

	st, err := srcFS.Stat(j.Src)
	if err != nil {
		return err
	}

	if !st.IsDir() {
		q.mu.Lock()
		j.Total = st.Size()
		q.mu.Unlock()
		dst, err := resolveDst(dstFS, j.Dst, j.Policy)
		if err != nil {
			return err
		}
		if d, _ := dstFS.Split(dst); d != "" {
			if err := dstFS.MkdirAll(d); err != nil {
				return err
			}
		}
		return q.copyFile(ctx, j, srcFS, dstFS, j.Src, dst)
	}

	// Directory: collect everything first so the total is known.
	q.mu.Lock()
	j.IsDir = true
	q.mu.Unlock()
	items, skippedLinks, err := walkTree(ctx, srcFS, j.Src)
	if err != nil {
		return err
	}
	var total int64
	for _, it := range items {
		total += it.size
	}
	q.mu.Lock()
	j.Total = total
	if skippedLinks > 0 {
		j.Note = fmt.Sprintf("%d Verknüpfung(en) übersprungen (defekt oder Schleife)", skippedLinks)
	}
	q.mu.Unlock()

	root := j.Dst
	if j.Policy == Rename {
		if root, err = resolveDst(dstFS, j.Dst, Rename); err != nil {
			return err
		}
	}
	if err := dstFS.MkdirAll(root); err != nil {
		return err
	}
	skipped := 0
	files := 0
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		src := srcFS.Join(j.Src, it.rel)
		dst := dstFS.Join(root, it.rel)
		if it.dir {
			if err := dstFS.MkdirAll(dst); err != nil {
				return err
			}
			continue
		}
		files++
		target, err := resolveDst(dstFS, dst, j.Policy)
		if errors.Is(err, errSkipped) {
			skipped++
			q.addBytes(j, it.size)
			continue
		}
		if err != nil {
			return err
		}
		if err := q.copyFile(ctx, j, srcFS, dstFS, src, target); err != nil {
			return err
		}
	}
	if files > 0 && skipped == files {
		return errSkipped
	}
	return nil
}

// walkTree lists a directory tree below root. Symlinks are followed; broken
// links and links that point back to a directory currently being walked
// (loops) are skipped and counted.
func walkTree(ctx context.Context, f FS, root string) (items []entry, skipped int, err error) {
	real, err := f.Real(root)
	if err != nil {
		real = root
	}
	w := &walker{f: f, root: root}
	err = w.walk(ctx, "", real, map[string]bool{real: true}, 0)
	return w.items, w.skipped, err
}

type walker struct {
	f       FS
	root    string
	items   []entry
	skipped int
}

// walk lists root/rel. real is the canonical path of that directory and chain
// the canonical paths of all directories on the way down (for loop detection).
// maxDepth guards against loops that cannot be detected by canonical path
// (e.g. FTP servers, which do not resolve symlinks).
const maxDepth = 64

func (w *walker) walk(ctx context.Context, rel, real string, chain map[string]bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxDepth {
		w.skipped++
		return nil
	}
	infos, err := w.f.ReadDir(w.f.Join(w.root, rel))
	if err != nil {
		return err
	}
	for _, fi := range infos {
		r := w.f.Join(rel, fi.Name())
		childReal := w.f.Join(real, fi.Name())
		info := fi
		if fi.Mode()&fs.ModeSymlink != 0 {
			st, err := w.f.Stat(w.f.Join(w.root, r)) // follows the link
			if err != nil {
				w.skipped++ // broken link
				continue
			}
			info = st
			if st.IsDir() {
				if childReal, err = w.f.Real(w.f.Join(w.root, r)); err != nil || chain[childReal] {
					w.skipped++ // loop (or unresolvable)
					continue
				}
			}
		}
		switch {
		case info.IsDir():
			w.items = append(w.items, entry{rel: r, dir: true, mod: info.ModTime()})
			chain[childReal] = true
			err := w.walk(ctx, r, childReal, chain, depth+1)
			delete(chain, childReal)
			if err != nil {
				return err
			}
		case info.Mode().IsRegular():
			w.items = append(w.items, entry{rel: r, size: info.Size(), mod: info.ModTime()})
		default:
			w.skipped++ // sockets, devices, ...
		}
	}
	return nil
}

// resolveDst applies the conflict policy to a destination path.
func resolveDst(f FS, dst string, p Policy) (string, error) {
	if _, err := f.Stat(dst); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dst, nil
		}
		return "", err
	}
	switch p {
	case Skip:
		return "", errSkipped
	case Rename:
		return uniqueName(f, dst)
	default:
		return dst, nil
	}
}

func uniqueName(f FS, dst string) (string, error) {
	dir, base := f.Split(dst)
	ext := ""
	if i := strings.LastIndex(base, "."); i > 0 {
		base, ext = base[:i], base[i:]
	}
	for n := 1; n < 10000; n++ {
		cand := f.Join(dir, fmt.Sprintf("%s (%d)%s", base, n, ext))
		if _, err := f.Stat(cand); errors.Is(err, fs.ErrNotExist) {
			return cand, nil
		}
	}
	return "", errors.New("kein freier Dateiname gefunden")
}

// copyFile copies one file and reports progress. For uploads the reader is
// wrapped, for downloads the writer, so pkg/sftp can use its fast concurrent
// ReadFrom / WriteTo paths.
func (q *Queue) copyFile(ctx context.Context, j *Job, srcFS, dstFS FS, src, dst string) (err error) {
	in, err := srcFS.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Write next to the target and swap it in at the end: an existing file is
	// never left half-written, and busy targets (running binaries) can be replaced.
	dir, base := dstFS.Split(dst)
	tmp := dstFS.Join(dir, "."+base+".noxio-part")
	old, statErr := dstFS.Stat(dst)

	out, err := dstFS.Create(tmp)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			out.Close()
		}
		if err != nil {
			_ = dstFS.Remove(tmp) // never leave a partial file behind
		}
	}()

	progress := func(n int) { q.addBytes(j, int64(n)) }
	if j.Direction == Upload {
		_, err = io.Copy(out, &ctxReader{ctx: ctx, r: in, onRead: progress})
	} else {
		_, err = io.Copy(&ctxWriter{ctx: ctx, w: out, onWrite: progress}, in)
	}
	closed = true
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if statErr == nil && !old.IsDir() {
		if c, ok := dstFS.(chmodFS); ok {
			_ = c.Chmod(tmp, old.Mode().Perm()) // keep the permissions (e.g. executable) of the replaced file
		}
	}
	return dstFS.Replace(tmp, dst)
}

type ctxReader struct {
	ctx    context.Context
	r      io.Reader
	onRead func(int)
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.onRead(n)
	}
	return n, err
}

type ctxWriter struct {
	ctx     context.Context
	w       io.Writer
	onWrite func(int)
}

func (c *ctxWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(p)
	if n > 0 {
		c.onWrite(n)
	}
	return n, err
}
