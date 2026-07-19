package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestActionYAMLValid parses the composite action.yml and checks its structure,
// so a malformed action (which can only otherwise fail inside a live workflow)
// is caught here.
func TestActionYAMLValid(t *testing.T) {
	var action struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Inputs      map[string]struct {
			Description string `yaml:"description"`
			Default     string `yaml:"default"`
		} `yaml:"inputs"`
		Runs struct {
			Using string `yaml:"using"`
			Steps []struct {
				Shell string `yaml:"shell"`
				Run   string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	readYAML(t, filepath.Join(repoRoot, "action.yml"), &action)

	if action.Name == "" || action.Description == "" {
		t.Errorf("action must have a name and description: %+v", action)
	}
	if action.Runs.Using != "composite" {
		t.Errorf("runs.using = %q, want composite", action.Runs.Using)
	}
	if len(action.Runs.Steps) < 2 {
		t.Errorf("expected install + run steps, got %d", len(action.Runs.Steps))
	}
	for _, want := range []string{"version", "args", "working-directory"} {
		if _, ok := action.Inputs[want]; !ok {
			t.Errorf("missing input %q", want)
		}
	}
}

// TestPreCommitHooksYAMLValid parses .pre-commit-hooks.yaml and checks the depdog
// hook definition the pre-commit framework consumes.
func TestPreCommitHooksYAMLValid(t *testing.T) {
	var hooks []struct {
		ID            string   `yaml:"id"`
		Name          string   `yaml:"name"`
		Entry         string   `yaml:"entry"`
		Args          []string `yaml:"args"`
		Language      string   `yaml:"language"`
		PassFilenames bool     `yaml:"pass_filenames"`
		AlwaysRun     bool     `yaml:"always_run"`
	}
	readYAML(t, filepath.Join(repoRoot, ".pre-commit-hooks.yaml"), &hooks)

	if len(hooks) != 1 {
		t.Fatalf("expected exactly one hook, got %d", len(hooks))
	}
	h := hooks[0]
	if h.ID != "depdog" {
		t.Errorf("hook id = %q, want depdog", h.ID)
	}
	if h.Language != "golang" {
		t.Errorf("language = %q, want golang", h.Language)
	}
	if h.Entry != "depdog" {
		t.Errorf("entry = %q, want depdog", h.Entry)
	}
	if len(h.Args) == 0 || h.Args[0] != "check" {
		t.Errorf("args = %v, want [check ...]", h.Args)
	}
	if h.PassFilenames {
		t.Error("pass_filenames should be false (depdog checks the whole project)")
	}
}

func readYAML(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, into); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}
}
