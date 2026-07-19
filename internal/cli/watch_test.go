package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var (
	tEarly = time.Unix(1_700_000_000, 0)
	tLate  = time.Unix(1_700_003_600, 0) // +1h
)

func writeAt(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// TestTreeWatcherChanged: changed() is false until a file's mtime advances past
// the baseline, then true once, then false again.
func TestTreeWatcherChanged(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.go"), "package a\n", tEarly)

	w, err := newTreeWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := w.changed(); changed {
		t.Fatal("changed() should be false before any file is touched")
	}

	writeAt(t, filepath.Join(dir, "b.go"), "package a\n", tLate)
	if changed, err := w.changed(); err != nil || !changed {
		t.Fatalf("changed() = %v (err %v), want true after a newer file appeared", changed, err)
	}
	if changed, _ := w.changed(); changed {
		t.Error("changed() should be false on the second call with no new change")
	}
}

// TestNewestModTimeSkipsNoise: .git and other noise directories don't count, so
// their churn never triggers a re-check.
func TestNewestModTimeSkipsNoise(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.go"), "package a\n", tEarly)
	// A much newer file inside .git must be ignored.
	writeAt(t, filepath.Join(dir, ".git", "HEAD"), "ref: x\n", tLate)
	writeAt(t, filepath.Join(dir, "node_modules", "x", "i.js"), "//\n", tLate)

	got, err := newestModTime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(tEarly) {
		t.Errorf("newestModTime = %v, want %v (noise dirs must be skipped)", got, tEarly)
	}
}

// TestWatchLoop drives the real loop: a file-mtime advance fires onChange, and
// cancelling the context returns cleanly.
func TestWatchLoop(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.go")
	writeAt(t, f, "package a\n", tEarly)

	w, err := newTreeWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{}, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- watchLoop(ctx, w, 5*time.Millisecond, func() { fired <- struct{}{} }) }()

	if err := os.Chtimes(f, tLate, tLate); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("watchLoop did not fire onChange after a file mtime advanced")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("watchLoop returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchLoop did not return after ctx cancel")
	}
}
