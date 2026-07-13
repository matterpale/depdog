package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

const workYAML = `
version: 1
units:
  web:  { path: web, lang: ts }
  api:  services/api
  billing: { path: services/billing }
  shared: shared
default: allow
rules:
  web: { allow: [shared] }
  shared: { deny: ["*"] }
boundaries:
  services: [api, billing]
surfaces:
  shared: { exports: ["src/**"], internal: ["internal/**"] }
`

func TestParseWork(t *testing.T) {
	w, err := ParseWork([]byte(workYAML))
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(w.Units))
	for _, u := range w.Units {
		names = append(names, u.Name)
	}
	want := []string{"api", "billing", "shared", "web"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("units = %v, want %v (sorted)", names, want)
	}

	if u := w.Unit("api"); u == nil || u.Dir != "services/api" {
		t.Errorf("api unit = %+v, want dir services/api", u)
	}
	if u := w.Unit("web"); u == nil || u.Lang != "ts" {
		t.Errorf("web unit = %+v, want lang ts", u)
	}

	if w.Rules.Policy != core.PolicyAllow {
		t.Errorf("policy = %v, want allow", w.Rules.Policy)
	}
	if r, ok := w.Rules.Rules["web"]; !ok || len(r.Allow) != 1 || r.Allow[0].Name != "shared" {
		t.Errorf("web rule = %+v, want allow [shared]", w.Rules.Rules["web"])
	}
	if r, ok := w.Rules.Rules["shared"]; !ok || len(r.Deny) != 1 || r.Deny[0].Kind != core.RefAny {
		t.Errorf("shared rule = %+v, want deny [*]", w.Rules.Rules["shared"])
	}

	if len(w.Rules.Boundaries) != 1 || w.Rules.Boundaries[0].Name != "services" {
		t.Fatalf("boundaries = %+v, want [services]", w.Rules.Boundaries)
	}
	b := w.Rules.Boundaries[0]
	if len(b.Members) != 2 || b.Members[0].Component != "api" || b.Members[0].Patterns[0] != "services/api" {
		t.Errorf("services members = %+v", b.Members)
	}

	s, ok := w.Surfaces["shared"]
	if !ok || len(s.Exports) != 1 || len(s.Internal) != 1 {
		t.Errorf("shared surface = %+v", s)
	}
}

func TestParseWorkErrors(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"empty", "", "work file is empty"},
		{"version", "version: 9\nunits: {a: x}", "unsupported work file version"},
		{"no units", "version: 1\nunits: {}", `no "units" defined`},
		{"reserved name", "version: 1\nunits: {std: x}", "reserved"},
		{"missing path", "version: 1\nunits: {a: {lang: go}}", "has no path"},
		{"glob dir", "version: 1\nunits: {a: \"web/**\"}", "not a glob"},
		{"escaping dir", "version: 1\nunits: {a: ../up}", "escapes"},
		{"absolute dir", "version: 1\nunits: {a: /abs}", "must be relative"},
		{"duplicate dir", "version: 1\nunits: {a: web, b: web/.}", "same directory"},
		{"unknown rule name", "version: 1\nunits: {a: x}\nrules: {b: {allow: [a]}}", "rules name unknown unit"},
		{"unknown rule ref", "version: 1\nunits: {a: x}\nrules: {a: {allow: [b]}}", "unknown unit \"b\""},
		{"special ref", "version: 1\nunits: {a: x}\nrules: {a: {allow: [std]}}", "no meaning between units"},
		{"bad default", "version: 1\nunits: {a: x}\ndefault: maybe", "default must be"},
		{"unknown boundary member", "version: 1\nunits: {a: x}\nboundaries: {b: [a, c]}", "not a known unit"},
		{"boundary dup", "version: 1\nunits: {a: x, b: y}\nboundaries: {z: [a, a]}", "twice"},
		{"unknown surface", "version: 1\nunits: {a: x}\nsurfaces: {b: {exports: [y]}}", "surfaces name unknown unit"},
		{"empty surface", "version: 1\nunits: {a: x}\nsurfaces: {a: {}}", "surface for unit \"a\" is empty"},
		{"bad surface glob", "version: 1\nunits: {a: x}\nsurfaces: {a: {internal: [\"[\"]}}", "surface for unit \"a\""},
		{"unknown key", "version: 1\nunits: {a: x}\nrulez: {}", "rulez"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseWork([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestFindWorkFile(t *testing.T) {
	dir := t.TempDir()
	if _, ok := FindWorkFile(dir); ok {
		t.Error("found a work file in an empty dir")
	}
	path := filepath.Join(dir, WorkFileName)
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := FindWorkFile(dir)
	if !ok || got != path {
		t.Errorf("FindWorkFile = %q, %v; want %q, true", got, ok, path)
	}
}

// TestWorkSchemaMatchesFileStruct keeps the shipped work-file schema in
// lockstep with the parser, exactly like TestSchemaMatchesFileStruct does for
// depdog.yaml.
func TestWorkSchemaMatchesFileStruct(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "depdog.work.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("work schema is not valid JSON: %v", err)
	}

	fields := map[string]bool{}
	ft := reflect.TypeOf(workFile{})
	for i := 0; i < ft.NumField(); i++ {
		name, _, _ := strings.Cut(ft.Field(i).Tag.Get("yaml"), ",")
		if name != "" {
			fields[name] = true
		}
	}
	for name := range s.Properties {
		if !fields[name] {
			t.Errorf("work schema declares property %q with no matching workFile field", name)
		}
	}
	for name := range fields {
		if _, ok := s.Properties[name]; !ok {
			t.Errorf("workFile field %q is missing from the work schema", name)
		}
	}
	for _, want := range []string{"version", "units"} {
		if !contains(s.Required, want) {
			t.Errorf("work schema should require %q", want)
		}
	}
}

func TestUnitIdentities(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/api\n\ngo 1.22\n")
	write("package.json", `{"name": "@acme/api", "version": "1.0.0"}`)
	write("Cargo.toml", "[package]\nname = \"acme-api\" # crate\nversion = \"0.1.0\"\n\n[dependencies]\nname = \"decoy\"\n")
	write("pyproject.toml", "[build-system]\nrequires = [\"setuptools\"]\n\n[project]\nname = 'acme_api'\n")

	got := UnitIdentities(dir)
	want := []string{"@acme/api", "acme-api", "acme_api", "example.com/api"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnitIdentities = %v, want %v", got, want)
	}

	if ids := UnitIdentities(t.TempDir()); len(ids) != 0 {
		t.Errorf("empty dir identities = %v, want none", ids)
	}
}
