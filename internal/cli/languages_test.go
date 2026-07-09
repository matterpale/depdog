package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/config"
)

// touch writes a file at dir/name (creating parents) — a marker or a config.
func touch(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// TestRegistryWiring is the guardrail for the "add a language" contract: every
// registered adapter must be fully wired.
func TestRegistryWiring(t *testing.T) {
	if len(languages) == 0 {
		t.Fatal("the languages registry is empty")
	}
	for _, a := range languages {
		if a.Name == "" || len(a.Markers) == 0 || a.New == nil {
			t.Errorf("adapter %+v is not fully wired (needs Name, Markers, New)", a)
		}
		if got, ok := adapterByName(a.Name); !ok || got.Name != a.Name {
			t.Errorf("adapterByName(%q) did not round-trip", a.Name)
		}
	}
	if _, ok := adapterByName("nope"); ok {
		t.Error("adapterByName should miss an unregistered name")
	}
}

func TestDetectLanguageGo(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod", "module x\n")
	a, root, err := detectLanguage(dir)
	if err != nil {
		t.Fatalf("detectLanguage: %v", err)
	}
	if a.Name != "go" {
		t.Errorf("lang = %q, want go", a.Name)
	}
	if root != mustAbs(t, dir) {
		t.Errorf("root = %q, want %q", root, dir)
	}
}

func TestDetectLanguageTSFromTSConfig(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json", "{}\n")
	a, _, err := detectLanguage(dir)
	if err != nil {
		t.Fatalf("detectLanguage: %v", err)
	}
	if a.Name != "ts" {
		t.Errorf("lang = %q, want ts", a.Name)
	}
}

func TestDetectLanguageTSFromPackageJSON(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json", `{"name":"x"}`)
	a, _, err := detectLanguage(dir)
	if err != nil {
		t.Fatalf("detectLanguage: %v", err)
	}
	if a.Name != "ts" {
		t.Errorf("lang = %q, want ts", a.Name)
	}
}

func TestDetectLanguageNearestMarkerWins(t *testing.T) {
	// Parent is a Go module; a nested TS project's package.json is nearer, so a
	// walk-up from the nested dir must resolve to ts, not the parent's go.mod.
	root := t.TempDir()
	touch(t, root, "go.mod", "module x\n")
	nested := filepath.Join(root, "web")
	touch(t, nested, "package.json", `{"name":"web"}`)
	a, gotRoot, err := detectLanguage(nested)
	if err != nil {
		t.Fatalf("detectLanguage: %v", err)
	}
	if a.Name != "ts" {
		t.Errorf("lang = %q, want ts (nearest marker wins)", a.Name)
	}
	if gotRoot != mustAbs(t, nested) {
		t.Errorf("root = %q, want %q", gotRoot, nested)
	}
}

func TestDetectLanguageAmbiguous(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod", "module x\n")
	touch(t, dir, "package.json", `{"name":"x"}`)
	_, _, err := detectLanguage(dir)
	if err == nil {
		t.Fatal("both markers in one dir must be an error, not a guess")
	}
	if !strings.Contains(err.Error(), "--lang") {
		t.Errorf("error should point at --lang:\n%v", err)
	}
}

func TestDetectLanguageNone(t *testing.T) {
	dir := t.TempDir()
	_, _, err := detectLanguage(dir)
	if err == nil {
		t.Fatal("no marker must be an error")
	}
	for _, want := range []string{"go.mod", "tsconfig.json", "package.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("not-found error should mention %q:\n%v", want, err)
		}
	}
}

func TestResolveProjectBadFlag(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json", "{}\n")
	_, _, _, err := resolveProject(dir, "python")
	if err == nil {
		t.Fatal("an unknown --lang value must be rejected")
	}
	if !strings.Contains(err.Error(), "python") {
		t.Errorf("error should name the bad value:\n%v", err)
	}
}

func TestResolveProjectTSAutoDetect(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json", "{}\n")
	touch(t, dir, config.DefaultName, "version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: deny\n")
	a, root, cfg, err := resolveProject(dir, "")
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}
	if a.Name != "ts" {
		t.Errorf("resolved = %q, want ts", a.Name)
	}
	if root != mustAbs(t, dir) {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if cfg != filepath.Join(mustAbs(t, dir), config.DefaultName) {
		t.Errorf("cfg = %q, want the depdog.yaml beside the marker", cfg)
	}
}

func TestResolveProjectTSMissingConfig(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json", `{"name":"x"}`)
	_, _, _, err := resolveProject(dir, "ts")
	if err == nil {
		t.Fatal("a TS project without depdog.yaml must error")
	}
	if !strings.Contains(err.Error(), "init") {
		t.Errorf("error should point at depdog init:\n%v", err)
	}
}
