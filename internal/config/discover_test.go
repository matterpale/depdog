package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// touch writes an empty (or given) file at dir/name, creating parents.
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

func TestDetectLanguageGo(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod", "module x\n")
	lang, root, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if lang != "go" {
		t.Errorf("lang = %q, want go", lang)
	}
	if root != mustAbs(t, dir) {
		t.Errorf("root = %q, want %q", root, dir)
	}
}

func TestDetectLanguageTSFromTSConfig(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json", "{}\n")
	lang, _, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if lang != "ts" {
		t.Errorf("lang = %q, want ts", lang)
	}
}

func TestDetectLanguageTSFromPackageJSON(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json", `{"name":"x"}`)
	lang, _, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if lang != "ts" {
		t.Errorf("lang = %q, want ts", lang)
	}
}

func TestDetectLanguageNearestMarkerWins(t *testing.T) {
	// Parent is a Go module; a nested TS project's package.json is nearer, so a
	// walk-up from the nested dir must resolve to ts, not the parent's go.mod.
	root := t.TempDir()
	touch(t, root, "go.mod", "module x\n")
	nested := filepath.Join(root, "web")
	touch(t, nested, "package.json", `{"name":"web"}`)
	lang, gotRoot, err := DetectLanguage(nested)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if lang != "ts" {
		t.Errorf("lang = %q, want ts (nearest marker wins)", lang)
	}
	if gotRoot != mustAbs(t, nested) {
		t.Errorf("root = %q, want %q", gotRoot, nested)
	}
}

func TestDetectLanguageAmbiguous(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod", "module x\n")
	touch(t, dir, "package.json", `{"name":"x"}`)
	_, _, err := DetectLanguage(dir)
	if err == nil {
		t.Fatal("both markers in one dir must be an error, not a guess")
	}
	if !strings.Contains(err.Error(), "--lang") {
		t.Errorf("error should point at --lang:\n%v", err)
	}
}

func TestDetectLanguageNone(t *testing.T) {
	dir := t.TempDir()
	_, _, err := DetectLanguage(dir)
	if err == nil {
		t.Fatal("no marker must be an error")
	}
	for _, want := range []string{"go.mod", "tsconfig.json", "package.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("not-found error should mention %q:\n%v", want, err)
		}
	}
}

func TestFindWithLanguageBadFlag(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json", "{}\n")
	_, _, _, err := FindWithLanguage(dir, "python")
	if err == nil {
		t.Fatal("an unknown --lang value must be rejected")
	}
	if !strings.Contains(err.Error(), "python") {
		t.Errorf("error should name the bad value:\n%v", err)
	}
}

func TestFindWithLanguageTSAutoDetect(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "tsconfig.json", "{}\n")
	touch(t, dir, DefaultName, "version: 2\ncomponents:\n  a: { path: \"**\", allow: [\"*\"] }\ndefault: deny\n")
	cfg, root, resolved, err := FindWithLanguage(dir, "")
	if err != nil {
		t.Fatalf("FindWithLanguage: %v", err)
	}
	if resolved != "ts" {
		t.Errorf("resolved = %q, want ts", resolved)
	}
	if root != mustAbs(t, dir) {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if cfg != filepath.Join(mustAbs(t, dir), DefaultName) {
		t.Errorf("cfg = %q, want the depdog.yaml beside the marker", cfg)
	}
}

func TestFindWithLanguageTSMissingConfig(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json", `{"name":"x"}`)
	_, _, _, err := FindWithLanguage(dir, "ts")
	if err == nil {
		t.Fatal("a TS project without depdog.yaml must error")
	}
	if !strings.Contains(err.Error(), "init") {
		t.Errorf("error should point at depdog init:\n%v", err)
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
