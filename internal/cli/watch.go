package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// watchInterval is how often --watch polls the tree for changes. A dev loop does
// not need sub-100ms latency, and a coarse poll keeps the idle cost negligible;
// re-checks coalesce because a change is only acted on once the newest mtime
// advances past the last check.
const watchInterval = 500 * time.Millisecond

// watchSkipDirs are directories whose churn should never trigger a re-check
// (.git writes constantly; the rest are build/vendor noise). Dot-directories are
// skipped too. Mirrors the polyglot discovery skip list.
var watchSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true,
	"build": true, "out": true, "__pycache__": true,
}

// runWatch runs the check once, then re-runs it whenever a file under the working
// directory changes, until interrupted. It is a dev loop, not a gate: it never
// exits non-zero on violations (Ctrl-C exits 0). Machine formats are rejected —
// a stream of json/sarif snapshots is not useful — so watch is text only.
func runWatch(cmd *cobra.Command, args []string, o checkOptions) error {
	if o.format != "text" {
		return fmt.Errorf("--watch supports only --format text (got %q)", o.format)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	runOnce := func() {
		clearScreen(out)
		if _, err := runCheckOnce(cmd, args, o); err != nil {
			fmt.Fprintf(errOut, "depdog: %v\n", err)
		}
		fmt.Fprintln(errOut, "— watching for changes (Ctrl-C to stop) —")
	}
	runOnce()

	w, err := newTreeWatcher(root)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	return watchLoop(ctx, w, watchInterval, runOnce)
}

// watchLoop polls w every interval and calls onChange whenever the tree's newest
// mtime advances, until ctx is cancelled (Ctrl-C). A scan error stops the loop
// with that error. It is the testable seam: inject a treeWatcher over a temp dir
// and a counting onChange.
func watchLoop(ctx context.Context, w *treeWatcher, interval time.Duration, onChange func()) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			changed, err := w.changed()
			if err != nil {
				return err
			}
			if changed {
				onChange()
			}
		}
	}
}

// treeWatcher tracks the newest file mtime under root, so successive changed()
// calls report whether anything changed since the last one.
type treeWatcher struct {
	root string
	last time.Time
}

// newTreeWatcher records the tree's current newest mtime as the baseline, so the
// first changed() only fires on a change made after construction.
func newTreeWatcher(root string) (*treeWatcher, error) {
	t, err := newestModTime(root)
	if err != nil {
		return nil, err
	}
	return &treeWatcher{root: root, last: t}, nil
}

// changed reports whether any file under the tree is newer than the last time it
// returned true (or than construction), advancing the watermark when so.
func (w *treeWatcher) changed() (bool, error) {
	t, err := newestModTime(w.root)
	if err != nil {
		return false, err
	}
	if t.After(w.last) {
		w.last = t
		return true, nil
	}
	return false, nil
}

// newestModTime walks root (skipping VCS/build/vendor noise and dot-directories)
// and returns the newest file modification time found. Transient errors — a file
// removed mid-walk — are tolerated rather than aborting the scan.
func newestModTime(root string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate races (a file deleted between readdir and stat)
		}
		if d.IsDir() {
			if path != root && (watchSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if mt := info.ModTime(); mt.After(newest) {
			newest = mt
		}
		return nil
	})
	return newest, err
}

// clearScreen resets the terminal to the top-left and clears it, so each watch
// re-check replaces the previous report rather than scrolling.
func clearScreen(w io.Writer) {
	fmt.Fprint(w, "\033[H\033[2J")
}
