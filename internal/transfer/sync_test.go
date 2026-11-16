package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func actionsByKind(p SyncPlan) map[string][]string {
	m := map[string][]string{}
	for _, a := range p.Actions {
		m[a.Kind] = append(m[a.Kind], a.Rel+":"+a.Reason)
	}
	return m
}

func setMod(t *testing.T, p string, d time.Duration) {
	t.Helper()
	ts := time.Now().Add(d)
	if err := os.Chtimes(p, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestPlanSyncUpload(t *testing.T) {
	q, _ := newQ(t)
	local, remote := t.TempDir(), t.TempDir()
	write(t, filepath.Join(local, "new.txt"), "n")
	write(t, filepath.Join(local, "dir", "inner.txt"), "i")
	write(t, filepath.Join(local, "same.txt"), "same")
	write(t, filepath.Join(local, "changed.txt"), "longer content")
	write(t, filepath.Join(local, "newer.txt"), "aaaa")
	write(t, filepath.Join(local, "targetnewer.txt"), "aaaa")
	write(t, filepath.Join(remote, "same.txt"), "same")
	write(t, filepath.Join(remote, "changed.txt"), "old")
	write(t, filepath.Join(remote, "newer.txt"), "bbbb")
	write(t, filepath.Join(remote, "targetnewer.txt"), "bbbb")
	write(t, filepath.Join(remote, "extra.txt"), "e")
	write(t, filepath.Join(remote, "extradir", "x.txt"), "x")
	for _, f := range []string{"same.txt", "newer.txt", "targetnewer.txt"} {
		setMod(t, filepath.Join(local, f), -time.Hour)
		setMod(t, filepath.Join(remote, f), -time.Hour)
	}
	setMod(t, filepath.Join(local, "newer.txt"), 0)        // local newer
	setMod(t, filepath.Join(remote, "targetnewer.txt"), 0) // remote newer, same size

	plan, err := q.PlanSync(context.Background(), local, remote, SyncOptions{Direction: Upload, KeepNewer: true, DeleteExtra: true})
	if err != nil {
		t.Fatal(err)
	}
	got := actionsByKind(plan)
	wantCopy := map[string]bool{"new.txt:new": true, "dir/inner.txt:new": true, "changed.txt:size": true, "newer.txt:newer": true}
	if len(got["copy"]) != len(wantCopy) {
		t.Fatalf("copy = %v", got["copy"])
	}
	for _, c := range got["copy"] {
		if !wantCopy[c] {
			t.Errorf("unexpected copy %s", c)
		}
	}
	if len(got["mkdir"]) != 1 || got["mkdir"][0] != "dir:new" {
		t.Errorf("mkdir = %v", got["mkdir"])
	}
	// extradir is deleted as a whole, its child not listed separately
	if len(got["delete"]) != 2 {
		t.Errorf("delete = %v", got["delete"])
	}
	if plan.CopyCount != 4 || plan.DeleteCount != 2 {
		t.Errorf("counts = %d/%d", plan.CopyCount, plan.DeleteCount)
	}

	// nothing is touched by planning
	if _, err := os.Stat(filepath.Join(remote, "extra.txt")); err != nil {
		t.Fatal("planning deleted something")
	}

	n, err := q.ApplySync(plan)
	if err != nil || n != 4 {
		t.Fatalf("ApplySync = %d, %v", n, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for q.ActiveCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for _, f := range []string{"new.txt", "dir/inner.txt", "changed.txt", "newer.txt"} {
		if _, err := os.Stat(filepath.Join(remote, f)); err != nil {
			t.Errorf("%s not synced: %v", f, err)
		}
	}
	for _, f := range []string{"extra.txt", "extradir"} {
		if _, err := os.Stat(filepath.Join(remote, f)); !os.IsNotExist(err) {
			t.Errorf("%s not deleted", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(remote, "targetnewer.txt")); string(b) != "bbbb" {
		t.Error("newer target file was overwritten despite KeepNewer")
	}

	// second plan: everything in sync
	plan2, _ := q.PlanSync(context.Background(), local, remote, SyncOptions{Direction: Upload, KeepNewer: true, DeleteExtra: true})
	if len(plan2.Actions) != 0 {
		t.Errorf("second plan not empty: %+v", plan2.Actions)
	}
}

func TestPlanSyncDownloadWithoutDeleteAndMissingTarget(t *testing.T) {
	q, _ := newQ(t)
	local, remote := filepath.Join(t.TempDir(), "not-yet"), t.TempDir()
	write(t, filepath.Join(remote, "a", "b.txt"), "b")
	plan, err := q.PlanSync(context.Background(), local, remote, SyncOptions{Direction: Download})
	if err != nil {
		t.Fatal(err)
	}
	if plan.CopyCount != 1 || plan.DeleteCount != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := q.ApplySync(plan); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for q.ActiveCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if b, _ := os.ReadFile(filepath.Join(local, "a", "b.txt")); string(b) != "b" {
		t.Fatal("download sync into missing directory failed")
	}
}

func TestPlanSyncNotConnected(t *testing.T) {
	q := NewQueue(1, LocalFS{}, nil)
	if _, err := q.PlanSync(context.Background(), t.TempDir(), "/x", SyncOptions{Direction: Upload}); err == nil {
		t.Fatal("expected an error without a connection")
	}
}
