package cli

import (
	"bytes"
	"context"
	"io/fs"
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

// chtimesTree pins every file AND directory under root to mtime, so a test has a
// deterministic newest-mtime baseline (newestModTime now counts directory
// mtimes, which the OS sets to real time on creation). Chtimes on a child never
// re-bumps its parent, so a single walk suffices.
func chtimesTree(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, mtime, mtime)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestTreeWatcherChanged: changed() is false until a file's mtime advances past
// the baseline, then true once, then false again.
func TestTreeWatcherChanged(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.go"), "package a\n", tEarly)
	chtimesTree(t, dir, tEarly)

	w, err := newTreeWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := w.changed(); changed {
		t.Fatal("changed() should be false before any file is touched")
	}

	if err := os.Chtimes(filepath.Join(dir, "a.go"), tLate, tLate); err != nil {
		t.Fatal(err)
	}
	if changed, err := w.changed(); err != nil || !changed {
		t.Fatalf("changed() = %v (err %v), want true after a file advanced", changed, err)
	}
	if changed, _ := w.changed(); changed {
		t.Error("changed() should be false on the second call with no new change")
	}
}

// TestTreeWatcherDetectsDeletion: removing a file bumps the parent directory
// mtime, so the watcher notices even though no surviving file got newer.
func TestTreeWatcherDetectsDeletion(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.go"), "package a\n", tEarly)
	writeAt(t, filepath.Join(dir, "b.go"), "package a\n", tEarly)
	chtimesTree(t, dir, tEarly)

	w, err := newTreeWatcher(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	if changed, err := w.changed(); err != nil || !changed {
		t.Errorf("deletion not detected: changed=%v err=%v (directory mtime should advance)", changed, err)
	}
}

// TestNewestModTimeSkipsNoise: .git and other noise directories don't count.
func TestNewestModTimeSkipsNoise(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, filepath.Join(dir, "a.go"), "package a\n", tEarly)
	writeAt(t, filepath.Join(dir, ".git", "HEAD"), "ref: x\n", tLate)
	writeAt(t, filepath.Join(dir, "node_modules", "x", "i.js"), "//\n", tLate)
	// Pin the source file and the root dir (creating the subdirs bumped root's
	// mtime to now) to tEarly; the noise dirs keep their tLate but must be skipped.
	if err := os.Chtimes(filepath.Join(dir, "a.go"), tEarly, tEarly); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, tEarly, tEarly); err != nil {
		t.Fatal(err)
	}

	got, err := newestModTime(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(tEarly) {
		t.Errorf("newestModTime = %v, want %v (noise dirs must be skipped)", got, tEarly)
	}
}

// TestWatchRoot covers the three resolution branches.
func TestWatchRoot(t *testing.T) {
	// --config → the config's directory.
	got, err := watchRoot(checkOptions{configPath: filepath.Join("some", "dir", "depdog.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	if base := filepath.Base(got); base != "dir" {
		t.Errorf("--config watchRoot = %q, want it to end at the config dir", got)
	}

	// Single-module → walk up to the enclosing project root (go.mod), even from a
	// nested subdirectory.
	root := t.TempDir()
	writeAt(t, filepath.Join(root, "go.mod"), "module x\n", tEarly)
	sub := filepath.Join(root, "internal", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	got, err = watchRoot(checkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	gotEval, _ := filepath.EvalSymlinks(got)
	rootEval, _ := filepath.EvalSymlinks(root)
	if gotEval != rootEval {
		t.Errorf("single-module watchRoot = %q, want the module root %q", gotEval, rootEval)
	}

	// --all → the cwd (discovery walks downward from there).
	got, err = watchRoot(checkOptions{all: true})
	if err != nil {
		t.Fatal(err)
	}
	gotEval, _ = filepath.EvalSymlinks(got)
	subEval, _ := filepath.EvalSymlinks(sub)
	if gotEval != subEval {
		t.Errorf("--all watchRoot = %q, want the cwd %q", gotEval, subEval)
	}
}

// TestClearScreenSkipsNonTTY: no control bytes leak into a non-terminal writer.
func TestClearScreenSkipsNonTTY(t *testing.T) {
	var buf bytes.Buffer
	clearScreen(&buf)
	if buf.Len() != 0 {
		t.Errorf("clearScreen wrote %q to a non-terminal writer; want nothing", buf.String())
	}
}

// TestWatchLoop drives the real loop: a file-mtime advance fires onChange, and
// cancelling the context returns cleanly.
func TestWatchLoop(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.go")
	writeAt(t, f, "package a\n", tEarly)
	chtimesTree(t, dir, tEarly)

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
