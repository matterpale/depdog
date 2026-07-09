// Package e2e builds the real depdog binary and runs it against the
// fixture modules, asserting exit codes and golden output.
package e2e

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	if runtime.GOOS == "windows" {
		// exec on Windows resolves executables by PATHEXT extension; a bare
		// "depdog" file would build fine but never launch.
		binary += ".exe"
	}

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

func TestCheckDirtyGitHub(t *testing.T) {
	out, _, exit := run(t, fixture("dirty"), "check", "--format", "github")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "dirty_github.golden", out)
}

func TestCheckDirtySARIF(t *testing.T) {
	out, _, exit := run(t, fixture("dirty"), "check", "--format", "sarif")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "dirty_sarif.golden", out)
}

func TestCheckBadFormat(t *testing.T) {
	_, stderr, exit := run(t, fixture("dirty"), "check", "--format", "toml")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "sarif") {
		t.Errorf("stderr should list valid formats:\n%s", stderr)
	}
}

func TestCheckTSClean(t *testing.T) {
	// A layered TS project auto-detected via tsconfig.json/package.json: the
	// same engine and depdog.yaml format the Go path uses, exit 0.
	out, stderr, exit := run(t, fixture("ts-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "ts_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckTSDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("ts-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "ts_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckTSDirtyJSON(t *testing.T) {
	// Proves the stable JSON schema is language-neutral: TS violations render
	// through the same renderer and field names as Go ones.
	out, _, exit := run(t, fixture("ts-dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "ts_dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestExplainTSComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("ts-dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "ts_explain_component.golden", out)
}

func TestGraphTSComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("ts-dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "ts_graph_component_dot.golden", out)
}

func TestCheckTSLangFlag(t *testing.T) {
	// Explicit --lang ts selects the adapter (bypassing auto-detect).
	out, stderr, exit := run(t, fixture("ts-clean"), "check", "--lang", "ts")
	if exit != 0 {
		t.Fatalf("--lang ts: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("--lang ts on clean fixture should pass:\n%s", out)
	}
}

func TestCheckBadLang(t *testing.T) {
	_, stderr, exit := run(t, fixture("ts-clean"), "check", "--lang", "python")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "lang") {
		t.Errorf("stderr should mention --lang:\n%s", stderr)
	}
}

func TestCheckPyClean(t *testing.T) {
	// A layered Python project auto-detected via pyproject.toml: the same engine
	// and depdog.yaml format the Go/TS paths use, exit 0.
	out, stderr, exit := run(t, fixture("python-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "py_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckPyDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("python-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "py_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckPyDirtyJSON(t *testing.T) {
	// Proves the stable JSON schema is language-neutral: Python violations render
	// through the same renderer and field names as Go/TS ones.
	out, _, exit := run(t, fixture("python-dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "py_dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestExplainPyComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("python-dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "py_explain_component.golden", out)
}

func TestGraphPyComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("python-dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "py_graph_component_dot.golden", out)
}

func TestCheckPyLangFlag(t *testing.T) {
	// Explicit --lang py selects the adapter (bypassing auto-detect).
	out, stderr, exit := run(t, fixture("python-clean"), "check", "--lang", "py")
	if exit != 0 {
		t.Fatalf("--lang py: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("--lang py on clean fixture should pass:\n%s", out)
	}
}

func TestCheckRustClean(t *testing.T) {
	// A layered Rust crate auto-detected via Cargo.toml: the same engine and
	// depdog.yaml format the Go/TS/Py paths use, exit 0.
	out, stderr, exit := run(t, fixture("rust-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "rs_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckRustDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("rust-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "rs_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckRustDirtyJSON(t *testing.T) {
	// Proves the stable JSON schema is language-neutral: Rust violations render
	// through the same renderer and field names as Go/TS/Py ones.
	out, _, exit := run(t, fixture("rust-dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "rs_dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestExplainRustComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("rust-dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "rs_explain_component.golden", out)
}

func TestGraphRustComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("rust-dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "rs_graph_component_dot.golden", out)
}

func TestCheckRustLangFlag(t *testing.T) {
	// Explicit --lang rs selects the adapter (bypassing auto-detect).
	out, stderr, exit := run(t, fixture("rust-clean"), "check", "--lang", "rs")
	if exit != 0 {
		t.Fatalf("--lang rs: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("--lang rs on clean fixture should pass:\n%s", out)
	}
}

func TestCheckJavaClean(t *testing.T) {
	// A layered Java project auto-detected via pom.xml: the same engine and
	// depdog.yaml format the Go/TS/Py/Rust paths use, exit 0.
	out, stderr, exit := run(t, fixture("java-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "java_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckJavaDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("java-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "java_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckJavaDirtyJSON(t *testing.T) {
	// Proves the stable JSON schema is language-neutral: Java violations render
	// through the same renderer and field names as Go/TS/Py/Rust ones.
	out, _, exit := run(t, fixture("java-dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "java_dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestExplainJavaComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("java-dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "java_explain_component.golden", out)
}

func TestGraphJavaComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("java-dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "java_graph_component_dot.golden", out)
}

func TestCheckJavaLangFlag(t *testing.T) {
	// Explicit --lang java selects the adapter (bypassing auto-detect).
	out, stderr, exit := run(t, fixture("java-clean"), "check", "--lang", "java")
	if exit != 0 {
		t.Fatalf("--lang java: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("--lang java on clean fixture should pass:\n%s", out)
	}
}

func TestCheckKotlinClean(t *testing.T) {
	// A layered Kotlin project auto-detected via build.gradle.kts: the same
	// engine and depdog.yaml format the Go/TS/Py/Rust/Java paths use, exit 0.
	out, stderr, exit := run(t, fixture("kotlin-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "kt_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckKotlinDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("kotlin-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "kt_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckKotlinDirtyJSON(t *testing.T) {
	// Proves the stable JSON schema is language-neutral: Kotlin violations render
	// through the same renderer and field names as Go/TS/Py/Rust/Java ones.
	out, _, exit := run(t, fixture("kotlin-dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "kt_dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestExplainKotlinComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("kotlin-dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "kt_explain_component.golden", out)
}

func TestGraphKotlinComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("kotlin-dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "kt_graph_component_dot.golden", out)
}

func TestCheckKotlinLangFlag(t *testing.T) {
	// Explicit --lang kt selects the adapter (bypassing auto-detect).
	out, stderr, exit := run(t, fixture("kotlin-clean"), "check", "--lang", "kt")
	if exit != 0 {
		t.Fatalf("--lang kt: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("--lang kt on clean fixture should pass:\n%s", out)
	}
}

func TestCheckRubyClean(t *testing.T) {
	// A layered Ruby app auto-detected via the Gemfile: the same engine and
	// depdog.yaml format the Go/TS/Py/Rust/Java paths use, exit 0.
	out, stderr, exit := run(t, fixture("ruby-clean"), "check")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "rb_clean_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckRubyDirtyText(t *testing.T) {
	out, _, exit := run(t, fixture("ruby-dirty"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "rb_dirty_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckRubyDirtyJSON(t *testing.T) {
	// Proves the stable JSON schema is language-neutral: Ruby violations render
	// through the same renderer and field names as Go/TS/Py/Rust/Java ones.
	out, _, exit := run(t, fixture("ruby-dirty"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "rb_dirty_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestExplainRubyComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("ruby-dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "rb_explain_component.golden", out)
}

func TestGraphRubyComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("ruby-dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "rb_graph_component_dot.golden", out)
}

func TestCheckRubyLangFlag(t *testing.T) {
	// Explicit --lang rb selects the adapter (bypassing auto-detect).
	out, stderr, exit := run(t, fixture("ruby-clean"), "check", "--lang", "rb")
	if exit != 0 {
		t.Fatalf("--lang rb: exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("--lang rb on clean fixture should pass:\n%s", out)
	}
}

func TestCheckAmbiguousLanguage(t *testing.T) {
	// A directory carrying both a go.mod and a package.json is not guessed at:
	// depdog errors (exit 2) and points at --lang rather than silently choosing.
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/poly\n\ngo 1.21\n")
	write("package.json", `{"name":"poly"}`+"\n")
	write("depdog.yaml", "version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: deny\n")

	_, stderr, exit := run(t, dir, "check")
	if exit != 2 {
		t.Fatalf("exit %d, want 2\nstderr:\n%s", exit, stderr)
	}
	if !strings.Contains(stderr, "--lang") {
		t.Errorf("stderr should point at --lang:\n%s", stderr)
	}
}

func TestConfigDumpClean(t *testing.T) {
	out, stderr, exit := run(t, fixture("clean"), "config")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "config_clean.golden", out)
}

func TestConfigDumpBlacklist(t *testing.T) {
	out, stderr, exit := run(t, fixture("blacklist"), "config")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "config_blacklist.golden", out)
}

func TestCheckColorAlways(t *testing.T) {
	// --color=always forces ANSI even though the run env sets NO_COLOR and pipes.
	out, _, exit := run(t, fixture("dirty"), "check", "--color", "always")
	if exit != 1 {
		t.Fatalf("exit %d, want 1", exit)
	}
	if !strings.Contains(out, "\x1b") {
		t.Errorf("--color=always should force ANSI:\n%q", out)
	}
}

func TestCheckBadColor(t *testing.T) {
	_, stderr, exit := run(t, fixture("clean"), "check", "--color", "rainbow")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "color") {
		t.Errorf("stderr should mention --color:\n%s", stderr)
	}
}

func TestCheckBlacklist(t *testing.T) {
	out, _, exit := run(t, fixture("blacklist"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "blacklist_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckCycle(t *testing.T) {
	// A component cycle (foo <-> bar) with no package-level cycle and no
	// violations: reported, but not fatal.
	out, stderr, exit := run(t, fixture("cycle"), "check")
	if exit != 0 {
		t.Fatalf("exit %d, want 0 (cycles are advisory)\nstderr:\n%s", exit, stderr)
	}
	golden(t, "cycle_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckReplaceClassifiesExternal(t *testing.T) {
	// A dependency replaced with a nested local module must still classify as
	// external (a distinct module path), not in-module. app allows only std, so
	// importing it is a violation whose target must be "external".
	out, stderr, exit := run(t, fixture("replace"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, `"import": "example.test/vendored/lib"`) {
		t.Errorf("expected the replaced import in output:\n%s", out)
	}
	if !strings.Contains(out, `"target": "external"`) {
		t.Errorf("a nested-module replace should classify as external:\n%s", out)
	}
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

// initModule lays down a fixed module tree for the init wizard to scan. The
// layout matches the ddd preset (cmd + domain/handler/service/repository) plus
// two extras (internal/telemetry, pkg/util) that exercise the "propose a
// component for an unmatched directory" path.
func initModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                         "module example.test/shop\n\ngo 1.26\n",
		"cmd/app/main.go":                "package main\n",
		"internal/domain/order/order.go": "package order\n",
		"internal/handler/handler.go":    "package handler\n",
		"internal/service/service.go":    "package service\n",
		"internal/repository/repo.go":    "package repository\n",
		"internal/telemetry/tel.go":      "package telemetry\n",
		"pkg/util/util.go":               "package util\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readConfig(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "depdog.yaml"))
	if err != nil {
		t.Fatalf("reading generated config: %v", err)
	}
	return string(data)
}

func TestInitDDDDeny(t *testing.T) {
	dir := initModule(t)
	out, stderr, exit := run(t, dir, "init", "--yes")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "init_ddd_deny.golden", readConfig(t, dir))

	// A successful init already round-trips (it parses before writing); prove
	// end to end that check accepts the file rather than erroring on config.
	if _, cerr, cexit := run(t, dir, "check"); cexit == 2 {
		t.Fatalf("generated config is a config error (exit 2):\n%s", cerr)
	}
}

func TestInitBlacklist(t *testing.T) {
	dir := initModule(t)
	_, stderr, exit := run(t, dir, "init", "--yes", "--default", "allow")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "init_ddd_allow.golden", readConfig(t, dir))
}

func TestInitFlat(t *testing.T) {
	dir := initModule(t)
	_, stderr, exit := run(t, dir, "init", "--yes", "--preset", "flat")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "init_flat_deny.golden", readConfig(t, dir))
}

func TestInitRefusesOverwrite(t *testing.T) {
	dir := initModule(t)
	if _, stderr, exit := run(t, dir, "init", "--yes"); exit != 0 {
		t.Fatalf("first init exit %d\n%s", exit, stderr)
	}
	_, stderr, exit := run(t, dir, "init", "--yes")
	if exit != 2 {
		t.Fatalf("overwrite without --force exit %d, want 2", exit)
	}
	if flat := strings.Join(strings.Fields(stderr), ""); !strings.Contains(flat, "--force") {
		t.Errorf("stderr should mention --force:\n%s", stderr)
	}
	if _, stderr, exit := run(t, dir, "init", "--yes", "--force"); exit != 0 {
		t.Fatalf("init --force exit %d\n%s", exit, stderr)
	}
}

func TestInitBadPreset(t *testing.T) {
	dir := initModule(t)
	_, stderr, exit := run(t, dir, "init", "--yes", "--preset", "nope")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "ddd") {
		t.Errorf("stderr should list valid presets:\n%s", stderr)
	}
}

func TestInitNeedsTTYWithoutYes(t *testing.T) {
	dir := initModule(t)
	_, stderr, exit := run(t, dir, "init")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("stderr should point at --yes:\n%s", stderr)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// dirtyModule copies the dirty fixture and its extlib sibling into a temp dir,
// preserving the ../extlib replace target so the module loads, and returns the
// dirty module dir. Baseline tests write into it without touching the committed
// fixtures.
func dirtyModule(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	copyTree(t, fixture("dirty"), filepath.Join(base, "dirty"))
	copyTree(t, fixture("extlib"), filepath.Join(base, "extlib"))
	return filepath.Join(base, "dirty")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestBaselineWrite(t *testing.T) {
	dir := dirtyModule(t)
	out, stderr, exit := run(t, dir, "baseline")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	golden(t, "dirty_baseline.golden", readFile(t, filepath.Join(dir, "depdog.baseline.yaml")))
	if !strings.Contains(out, "depdog.baseline.yaml") {
		t.Errorf("stdout should name the file:\n%s", out)
	}
}

func TestFailOnNewSuppressesBaselined(t *testing.T) {
	dir := dirtyModule(t)
	if _, stderr, exit := run(t, dir, "baseline"); exit != 0 {
		t.Fatalf("baseline exit %d\n%s", exit, stderr)
	}
	out, stderr, exit := run(t, dir, "check", "--fail-on", "new")
	if exit != 0 {
		t.Fatalf("exit %d, want 0\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "✓ no violations") {
		t.Errorf("all violations should be suppressed:\n%s", out)
	}
	if !strings.Contains(stderr, "4 baselined") {
		t.Errorf("stderr should note the suppression:\n%s", stderr)
	}
}

func TestFailOnNewFlagsNewViolation(t *testing.T) {
	dir := dirtyModule(t)
	// A baseline covering only two of the four violations; the other two are
	// new and must fail the run.
	partial := "version: 1\nviolations:\n" +
		"  - from: example.test/dirty/internal/domain/pricing\n    import: example.test/dirty/internal/repository\n" +
		"  - from: example.test/dirty/internal/domain/pricing\n    import: example.test/extlib\n"
	if err := os.WriteFile(filepath.Join(dir, "depdog.baseline.yaml"), []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, exit := run(t, dir, "check", "--fail-on", "new")
	if exit != 1 {
		t.Fatalf("exit %d, want 1 (two new violations)", exit)
	}
	if !strings.Contains(stderr, "2 baselined") {
		t.Errorf("stderr should note two suppressed:\n%s", stderr)
	}
}

func TestFailOnNewReportsFixed(t *testing.T) {
	dir := dirtyModule(t)
	if _, stderr, exit := run(t, dir, "baseline"); exit != 0 {
		t.Fatalf("baseline exit %d\n%s", exit, stderr)
	}
	// Append an entry no current violation matches — a resolved one.
	bl := filepath.Join(dir, "depdog.baseline.yaml")
	extra := "  - from: example.test/dirty/internal/gone\n    import: example.test/dirty/internal/repository\n"
	if err := os.WriteFile(bl, []byte(readFile(t, bl)+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, exit := run(t, dir, "check", "--fail-on", "new")
	if exit != 0 {
		t.Fatalf("exit %d, want 0 (all real violations baselined)\n%s", exit, stderr)
	}
	if !strings.Contains(stderr, "now fixed") {
		t.Errorf("stderr should report the resolved baseline entry:\n%s", stderr)
	}
}

func TestFailOnNewWithoutBaseline(t *testing.T) {
	dir := dirtyModule(t)
	if _, _, exit := run(t, dir, "check", "--fail-on", "new"); exit != 1 {
		t.Fatalf("exit %d, want 1 (no baseline: every violation is new)", exit)
	}
}

func TestCheckBadFailOn(t *testing.T) {
	_, stderr, exit := run(t, fixture("clean"), "check", "--fail-on", "sometimes")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "fail-on") {
		t.Errorf("stderr should mention --fail-on:\n%s", stderr)
	}
}

func TestGraphComponentDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "graph_component_dot.golden", out)
}

func TestGraphComponentMermaid(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "graph", "--format", "mermaid")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "graph_component_mermaid.golden", out)
}

func TestGraphPackageDOT(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "graph", "--level", "package")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "graph_package_dot.golden", out)
}

func TestGraphFocus(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "graph", "--focus", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "graph_focus_domain_dot.golden", out)
}

func TestGraphBadFocus(t *testing.T) {
	_, stderr, exit := run(t, fixture("dirty"), "graph", "--focus", "nope")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "nope") {
		t.Errorf("stderr should name the missing component:\n%s", stderr)
	}
}

func TestGraphViolationsOnly(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "graph", "--violations-only")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "graph_violations_only_dot.golden", out)
}

func TestGraphBadFormat(t *testing.T) {
	_, stderr, exit := run(t, fixture("dirty"), "graph", "--format", "svg")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "mermaid") {
		t.Errorf("stderr should list valid formats:\n%s", stderr)
	}
}

func TestGraphBadLevel(t *testing.T) {
	_, stderr, exit := run(t, fixture("dirty"), "graph", "--level", "galaxy")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "component") {
		t.Errorf("stderr should list valid levels:\n%s", stderr)
	}
}

func TestExplainComponent(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "explain", "domain")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_component.golden", out)
}

func TestExplainPackage(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "explain", "internal/domain/pricing")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_package.golden", out)
}

func TestExplainEdgeDenied(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "explain", "internal/domain/pricing", "internal/repository")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_edge_denied.golden", out)
}

func TestExplainEdgeAllowed(t *testing.T) {
	out, stderr, exit := run(t, fixture("dirty"), "explain", "internal/handler/checkout", "internal/domain/pricing")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_edge_allowed.golden", out)
}

func TestExplainEdgeBadTarget(t *testing.T) {
	_, stderr, exit := run(t, fixture("dirty"), "explain", "internal/domain/pricing", "nope")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "nope") {
		t.Errorf("stderr should name the unresolvable target:\n%s", stderr)
	}
}

func TestExplainEdgeExternalModule(t *testing.T) {
	// dirty's domain allows only std, so importing the extlib module is denied.
	out, stderr, exit := run(t, fixture("dirty"), "explain", "internal/domain/pricing", "example.test/extlib")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	if !strings.Contains(out, "external module") || !strings.Contains(out, "denied by") {
		t.Errorf("expected an external-module edge explanation:\n%s", out)
	}
}

func TestExplainUnknown(t *testing.T) {
	_, stderr, exit := run(t, fixture("dirty"), "explain", "nope")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "nope") {
		t.Errorf("stderr should name the missing target:\n%s", stderr)
	}
}

func TestCheckBoundaries(t *testing.T) {
	// The boundaries fixture: a cross-service edge (member → member) and a
	// shared-lib → service edge under a sealed boundary are both violations.
	out, _, exit := run(t, fixture("boundaries"), "check")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "boundaries_text.golden", reTextDur.ReplaceAllString(out, "checked in X"))
}

func TestCheckBoundariesJSON(t *testing.T) {
	out, _, exit := run(t, fixture("boundaries"), "check", "--format", "json")
	if exit != 1 {
		t.Fatalf("exit %d, want 1\n%s", exit, out)
	}
	golden(t, "boundaries_json.golden", reJSONDur.ReplaceAllString(out, `"duration_ms": 0`))
}

func TestConfigDumpBoundaries(t *testing.T) {
	out, stderr, exit := run(t, fixture("boundaries"), "config")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "config_boundaries.golden", out)
}

func TestExplainBoundaryEdge(t *testing.T) {
	// A cross-member edge: reported as denied by the boundary, not by a
	// component rule — the shared DecideBoundary path keeps explain and check in
	// step.
	out, stderr, exit := run(t, fixture("boundaries"), "explain", "cmd/service-b", "cmd/service-a")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_boundary.golden", out)
}

func TestExplainBoundaryEdgeSealed(t *testing.T) {
	// An ungrouped source (a shared lib) importing into a member of a sealed
	// boundary: denied by the sealed one-way rule.
	out, stderr, exit := run(t, fixture("boundaries"), "explain", "internal/badshared", "cmd/service-a")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_boundary_sealed.golden", out)
}

func TestExplainBoundaryPackage(t *testing.T) {
	out, stderr, exit := run(t, fixture("boundaries"), "explain", "cmd/service-a")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "explain_boundary_package.golden", out)
}

func TestGraphBoundaries(t *testing.T) {
	out, stderr, exit := run(t, fixture("boundaries"), "graph")
	if exit != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", exit, stderr)
	}
	golden(t, "graph_boundaries_dot.golden", out)
}

// mergeConfig is a hand-formatted config for the initModule layout that covers
// only cmd, internal/domain and internal/repository, with comments and value
// alignment a merge must preserve. handler, service, telemetry and util stay
// uncovered.
const mergeConfig = `# my architecture — hands off, depdog
version: 2

components:
  main:    { path: "cmd/**", allow: ["*"] } # entrypoints
  domain:  { path: "internal/domain/**", allow: [std] } # keep the core pure

  # data access
  storage: { path: "internal/repository/**", allow: [domain, std, external] }

default: deny

options:
  test_files: hybrid
`

func mergeModule(t *testing.T) string {
	t.Helper()
	dir := initModule(t)
	if err := os.WriteFile(filepath.Join(dir, "depdog.yaml"), []byte(mergeConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInitMergeAddsUncovered(t *testing.T) {
	dir := mergeModule(t)
	out, stderr, exit := run(t, dir, "init", "--merge", "--yes")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	for _, name := range []string{"handler", "service", "telemetry", "util"} {
		if !strings.Contains(out, name) {
			t.Errorf("stdout should name added component %q:\n%s", name, out)
		}
	}
	golden(t, "init_merge.golden", readConfig(t, dir))

	// The merged file must satisfy the same validator check uses.
	if _, cerr, cexit := run(t, dir, "check"); cexit == 2 {
		t.Fatalf("merged config is a config error (exit 2):\n%s", cerr)
	}
}

func TestInitMergeNothingNew(t *testing.T) {
	dir := initModule(t)
	cfg := "version: 2\n\ncomponents:\n  app: { path: \"**\", allow: [std, external] } # everything\n\ndefault: deny\n"
	if err := os.WriteFile(filepath.Join(dir, "depdog.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, exit := run(t, dir, "init", "--merge", "--yes")
	if exit != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", exit, out, stderr)
	}
	if !strings.Contains(out, "Nothing to merge") {
		t.Errorf("stdout should say nothing changed:\n%s", out)
	}
	if got := readConfig(t, dir); got != cfg {
		t.Errorf("a no-op merge must leave the file byte-for-byte intact:\n%s", got)
	}
}

func TestInitMergeMissingConfig(t *testing.T) {
	dir := initModule(t)
	_, stderr, exit := run(t, dir, "init", "--merge", "--yes")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "without --merge") {
		t.Errorf("stderr should point at plain init:\n%s", stderr)
	}
}

func TestInitMergeFlagConflicts(t *testing.T) {
	dir := mergeModule(t)
	if _, stderr, exit := run(t, dir, "init", "--merge", "--yes", "--force"); exit != 2 || !strings.Contains(stderr, "--force") {
		t.Errorf("--merge --force: exit %d, stderr:\n%s", exit, stderr)
	}
	if _, stderr, exit := run(t, dir, "init", "--merge", "--yes", "--preset", "ddd"); exit != 2 || !strings.Contains(stderr, "--preset") {
		t.Errorf("--merge --preset: exit %d, stderr:\n%s", exit, stderr)
	}
}

func TestInitMergeNeedsTTYWithoutYes(t *testing.T) {
	dir := mergeModule(t)
	_, stderr, exit := run(t, dir, "init", "--merge")
	if exit != 2 {
		t.Fatalf("exit %d, want 2", exit)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("stderr should point at --yes:\n%s", stderr)
	}
}
