// Package e2e builds the real depdog binary and runs it against the
// fixture modules, asserting exit codes and golden output.
package e2e

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

var (
	binary   string
	repoRoot string

	reTextDur = regexp.MustCompile(`checked in [^\n]+`)
	reJSONDur = regexp.MustCompile(`"duration_ms": \d+`)
)

func TestMain(m *testing.M) {
	flag.Parse()

	var err error
	if repoRoot, err = filepath.Abs(filepath.Join("..", "..")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "depdog-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "depdog")

	build := exec.Command("go", "build", "-o", binary, "./cmd/depdog")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building depdog: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func run(t *testing.T, dir string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	// Hermetic against workspaces and terminal styling on dev machines.
	cmd.Env = append(os.Environ(), "GOWORK=off", "NO_COLOR=1", "TERM=dumb")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("running depdog: %v", err)
		}
	}
	return out.String(), errb.String(), cmd.ProcessState.ExitCode()
}

func fixture(name string) string {
	return filepath.Join(repoRoot, "testdata", "fixtures", name)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("output does not match %s\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestCheckClean(t *testing.T) {
	out, stderr, exit := run(t, fixture("clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckDirtyJSON(t *testing.T) {
	out, _, exit := run(t, fixture("dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestCheckBlacklist(t *testing.T) {
	out, _, exit := run(t, fixture("blacklist"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "blacklist_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckMissingConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/naked\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, exit := run(t, dir, "check")
	if exit != 2 {
		t.Fatalf("exit %d, want 2\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(stderr, "init") {
		t.Errorf("stderr should point at depdog init:\n%s", stderr)
	}
}

func TestCheckSelf(t *testing.T) {
	out, stderr, exit := run(t, repoRoot, "check")
	if exit != 0 {
		t.Fatalf("depdog fails its own rules: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("self-check output:\n%s", out)
	}
}
