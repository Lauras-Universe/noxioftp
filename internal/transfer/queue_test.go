package transfer

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Local-to-local: LocalFS serves as both sides, which exercises all queue logic.
func newQ(t *testing.T) (*Queue, chan Job) {
	t.Helper()
	ch := make(chan Job, 1000)
	q := NewQueue(2, LocalFS{}, func(j Job) { ch <- j })
	q.SetRemote(LocalFS{})
	return q, ch
}

func waitFinal(t *testing.T, q *Queue, id string) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, j := range q.List() {
			if j.ID == id && j.Status != Queued && j.Status != Running {
				return j
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUploadFileAndDirectory(t *testing.T) {
	q, _ := newQ(t)
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "tree", "a.txt"), "A")
	write(t, filepath.Join(src, "tree", "deep", "b.txt"), "BB")
	os.MkdirAll(filepath.Join(src, "tree", "empty"), 0o755)

	j := q.Enqueue(Upload, "tree", filepath.Join(src, "tree"), filepath.Join(dst, "tree"), Overwrite)
	got := waitFinal(t, q, j.ID)
	if got.Status != Done || got.Total != 3 || got.Done != 3 || !got.IsDir {
		t.Fatalf("job = %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "tree", "deep", "b.txt")); string(b) != "BB" {
		t.Fatalf("copied content = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "tree", "empty")); err != nil {
		t.Fatal("empty dir not copied")
	}
}

func TestConflictPolicies(t *testing.T) {
	q, _ := newQ(t)
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "f.txt"), "new")
	write(t, filepath.Join(dst, "f.txt"), "old")
	from, to := filepath.Join(src, "f.txt"), filepath.Join(dst, "f.txt")

	if j := waitFinal(t, q, q.Enqueue(Upload, "f.txt", from, to, Skip).ID); j.Status != Skipped {
		t.Fatalf("skip: %+v", j)
	}
	if b, _ := os.ReadFile(to); string(b) != "old" {
		t.Fatal("skip overwrote file")
	}

	if j := waitFinal(t, q, q.Enqueue(Upload, "f.txt", from, to, Rename).ID); j.Status != Done {
		t.Fatalf("rename: %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "f (1).txt")); string(b) != "new" {
		t.Fatal("rename did not create f (1).txt")
	}
	if b, _ := os.ReadFile(to); string(b) != "old" {
		t.Fatal("rename overwrote original")
	}

	if j := waitFinal(t, q, q.Enqueue(Upload, "f.txt", from, to, Overwrite).ID); j.Status != Done {
		t.Fatalf("overwrite: %+v", j)
	}
	if b, _ := os.ReadFile(to); string(b) != "new" {
		t.Fatal("overwrite kept old content")
	}
}

func TestFailureAndRetry(t *testing.T) {
	q, _ := newQ(t)
	src, dst := t.TempDir(), t.TempDir()
	missing := filepath.Join(src, "later.txt")
	j := waitFinal(t, q, q.Enqueue(Upload, "later.txt", missing, filepath.Join(dst, "later.txt"), Overwrite).ID)
	if j.Status != Failed || j.Error == "" {
		t.Fatalf("job = %+v", j)
	}
	write(t, missing, "ok")
	q.Retry(j.ID)
	if j = waitFinal(t, q, j.ID); j.Status != Done {
		t.Fatalf("after retry: %+v", j)
	}
}

func TestCancelQueuedJobAndNoRemote(t *testing.T) {
	q := NewQueue(1, LocalFS{}, nil)
	src, dst := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "a"), "a")

	// no remote -> job fails cleanly instead of panicking
	j := waitFinal(t, q, q.Enqueue(Upload, "a", filepath.Join(src, "a"), filepath.Join(dst, "a"), Overwrite).ID)
	if j.Status != Failed {
		t.Fatalf("job = %+v", j)
	}
	q.Cancel(j.ID) // finished jobs ignore cancel
	q.ClearFinished()
	if len(q.List()) != 0 {
		t.Fatal("ClearFinished left jobs")
	}
}

func TestCancelRunningRemovesPartialFile(t *testing.T) {
	q, _ := newQ(t)
	src, dst := t.TempDir(), t.TempDir()
	big := make([]byte, 64<<20)
	write(t, filepath.Join(src, "big"), string(big))

	j := q.Enqueue(Upload, "big", filepath.Join(src, "big"), filepath.Join(dst, "big"), Overwrite)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			for _, x := range q.List() {
				if x.ID == j.ID && x.Done > 0 {
					q.Cancel(j.ID)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	got := waitFinal(t, q, j.ID)
	wg.Wait()
	if got.Status == Cancelled {
		if _, err := os.Stat(filepath.Join(dst, "big")); !os.IsNotExist(err) {
			t.Fatal("partial file left behind after cancel")
		}
	} else if got.Status != Done { // copy may legitimately win the race
		t.Fatalf("job = %+v", got)
	}
}

func TestFIFOWithLimitedWorkers(t *testing.T) {
	var mu sync.Mutex
	var started []string
	seen := map[string]bool{}
	q := NewQueue(1, LocalFS{}, func(j Job) {
		mu.Lock()
		defer mu.Unlock()
		if j.Status == Running && !seen[j.ID] { // progress events repeat Running
			seen[j.ID] = true
			started = append(started, j.Name)
		}
	})
	q.SetRemote(LocalFS{})
	src, dst := t.TempDir(), t.TempDir()
	var ids []string
	for _, n := range []string{"1", "2", "3", "4"} {
		write(t, filepath.Join(src, n), n)
		ids = append(ids, q.Enqueue(Upload, n, filepath.Join(src, n), filepath.Join(dst, n), Overwrite).ID)
	}
	for _, id := range ids {
		waitFinal(t, q, id)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, n := range []string{"1", "2", "3", "4"} {
		if started[i] != n {
			t.Fatalf("start order = %v", started)
		}
	}
}
