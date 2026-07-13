package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
)

// cmdWithLang builds a bare command carrying the persistent --lang flag (set to
// langFlag) so the flag-conflict checks can read it, exactly as the real command
// tree wires it in root.go.
func cmdWithLang(t *testing.T, langFlag string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "depdog"}
	// checkFlagConflicts reads the flag via cmd.Flags(); in the real tree cobra
	// merges the root's persistent --lang into a subcommand's flag set during
	// Execute. A standalone test command registers it on its own flag set so the
	// same lookup resolves.
	cmd.Flags().String("lang", "", "")
	if langFlag != "" {
		if err := cmd.Flags().Set("lang", langFlag); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

// TestCheckFlagConflicts is the D7/D8 usage-error matrix: every outlawed flag
// combination must be rejected (exit 2), and every allowed one accepted.
func TestCheckFlagConflicts(t *testing.T) {
	cases := []struct {
		name     string
		lang     string
		o        checkOptions
		args     []string
		workMode bool
		wantErr  string // substring; "" means no error
	}{
		{name: "plain", o: checkOptions{}},
		{name: "all alone", o: checkOptions{all: true}},
		{name: "all + unit", o: checkOptions{all: true, units: []string{"web"}}},
		{name: "config alone", o: checkOptions{configPath: "x/depdog.yaml"}},
		{name: "module in ws", o: checkOptions{modules: []string{"libs"}}},

		{name: "config + module", o: checkOptions{configPath: "x", modules: []string{"m"}}, wantErr: "--module cannot be combined with --config"},
		{name: "config + all", o: checkOptions{configPath: "x", all: true}, wantErr: "--config cannot be combined with --all"},
		{name: "config + unit", o: checkOptions{configPath: "x", units: []string{"web"}}, wantErr: "--config cannot be combined with --all or --unit"},

		{name: "all + lang", lang: "go", o: checkOptions{all: true}, wantErr: "units auto-detect their language"},
		{name: "all + module", o: checkOptions{all: true, modules: []string{"m"}}, wantErr: "--module cannot be combined with --all"},
		{name: "all + positional", o: checkOptions{all: true}, args: []string{"./pkg"}, wantErr: "use --unit to narrow the run"},

		{name: "unit without all", o: checkOptions{units: []string{"web"}}, wantErr: "--unit only applies with --all"},

		// Work mode is a fan-out like --all: --unit becomes legal without
		// --all, while --lang / --module / positional args stay outlawed.
		{name: "work + unit", workMode: true, o: checkOptions{units: []string{"web"}}},
		{name: "work + lang", workMode: true, lang: "go", o: checkOptions{}, wantErr: "units auto-detect their language"},
		{name: "work + module", workMode: true, o: checkOptions{modules: []string{"m"}}, wantErr: "--module cannot be combined with a depdog.work.yaml run"},
		{name: "work + positional", workMode: true, o: checkOptions{}, args: []string{"./pkg"}, wantErr: "no cross-unit meaning under a depdog.work.yaml run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := cmdWithLang(t, tc.lang)
			err := checkFlagConflicts(cmd, tc.o, tc.args, tc.workMode)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestSelectUnits covers --unit matching: no selector means every unit, a
// relative or absolute directory matches, and an unknown selector errors.
func TestSelectUnits(t *testing.T) {
	cwd := t.TempDir()
	web := filepath.Join(cwd, "web")
	api := filepath.Join(cwd, "services", "api")
	units := []config.Unit{
		{Dir: web, Rel: "web"},
		{Dir: api, Rel: "services/api"},
	}

	t.Run("no selectors returns all", func(t *testing.T) {
		got, err := selectUnits(cwd, units, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d units, want 2", len(got))
		}
	})

	t.Run("relative dir matches", func(t *testing.T) {
		got, err := selectUnits(cwd, units, []string{"services/api"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Rel != "services/api" {
			t.Fatalf("got %+v, want services/api", got)
		}
	})

	t.Run("absolute dir matches", func(t *testing.T) {
		got, err := selectUnits(cwd, units, []string{web})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Rel != "web" {
			t.Fatalf("got %+v, want web", got)
		}
	})

	t.Run("unknown selector errors", func(t *testing.T) {
		_, err := selectUnits(cwd, units, []string{"nope"})
		if err == nil {
			t.Fatal("an unknown --unit must error")
		}
		if !strings.Contains(err.Error(), "no discovered unit") {
			t.Errorf("error should name the miss:\n%v", err)
		}
	})
}

// TestAdapterForUnit covers the D7 resolution order: the `lang:` key pins the
// adapter (validated), else auto-detection; an unknown key names the unit dir.
func TestAdapterForUnit(t *testing.T) {
	t.Run("lang key pins adapter over ambiguous markers", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "go.mod", "module x\n")
		touch(t, dir, "package.json", `{"name":"x"}`)
		a, err := adapterForUnit(dir, "go")
		if err != nil {
			t.Fatalf("lang: go should pin the go adapter, got %v", err)
		}
		if a.Name != "go" {
			t.Errorf("adapter = %q, want go", a.Name)
		}
	})

	t.Run("empty lang auto-detects", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "tsconfig.json", "{}\n")
		a, err := adapterForUnit(dir, "")
		if err != nil {
			t.Fatalf("auto-detect: %v", err)
		}
		if a.Name != "ts" {
			t.Errorf("adapter = %q, want ts", a.Name)
		}
	})

	t.Run("empty lang on ambiguous markers errors, suggesting the lang: key", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "go.mod", "module x\n")
		touch(t, dir, "package.json", `{"name":"x"}`)
		_, err := adapterForUnit(dir, "")
		if err == nil {
			t.Fatal("ambiguous markers with no lang: key must error")
		}
		// Under --all, --lang is a usage error, so the remediation must point at
		// the `lang:` config key (D7), not the single-project --lang guidance.
		if !strings.Contains(err.Error(), "lang:") {
			t.Errorf("ambiguity error should suggest the `lang:` config key:\n%v", err)
		}
		if strings.Contains(err.Error(), "pass --lang") {
			t.Errorf("ambiguity error must not tell an --all unit to pass --lang (it is a usage error under --all):\n%v", err)
		}
	})

	t.Run("unknown lang key names the dir", func(t *testing.T) {
		dir := t.TempDir()
		touch(t, dir, "go.mod", "module x\n")
		_, err := adapterForUnit(dir, "klingon")
		if err == nil {
			t.Fatal("an unknown lang: key must be rejected")
		}
		if !strings.Contains(err.Error(), dir) {
			t.Errorf("error should name the unit dir %q:\n%v", dir, err)
		}
		if !strings.Contains(err.Error(), "klingon") {
			t.Errorf("error should name the bad value:\n%v", err)
		}
	})
}

// TestRegistryMarkers guards the DiscoverUnits marker feed: the accessor must
// surface a distinct set covering the known adapters, deduping shared markers.
func TestRegistryMarkers(t *testing.T) {
	markers := registryMarkers()
	set := map[string]bool{}
	for _, m := range markers {
		if set[m] {
			t.Errorf("registryMarkers returned %q twice — must be distinct", m)
		}
		set[m] = true
	}
	for _, want := range []string{"go.mod", "package.json", "tsconfig.json", "pyproject.toml", "Gemfile", "Cargo.toml", "elm.json"} {
		if !set[want] {
			t.Errorf("registryMarkers missing %q", want)
		}
	}
}

// TestErrResolutionClassifies confirms the two resolution-error shapes carry the
// errResolution sentinel (so the D1 fallback fires) while an evaluation error
// (a parse failure) does not.
func TestErrResolutionClassifies(t *testing.T) {
	if !errors.Is(noProjectError("/x"), errResolution) {
		t.Error("noProjectError must carry errResolution (triggers the fallback)")
	}

	dir := t.TempDir()
	touch(t, dir, "go.mod", "module x\n") // marker but no depdog.yaml
	_, _, _, err := resolveProject(dir, "")
	if err == nil {
		t.Fatal("a project with no depdog.yaml must error")
	}
	if !errors.Is(err, errResolution) {
		t.Errorf("missing-config error must carry errResolution:\n%v", err)
	}

	// A parse failure is an evaluation error, not a resolution one: it must NOT
	// carry the sentinel, so the fallback leaves it untouched.
	_, err = config.Parse([]byte("not: valid: config"))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if errors.Is(err, errResolution) {
		t.Error("a parse error must not carry errResolution (no fallback)")
	}
}
