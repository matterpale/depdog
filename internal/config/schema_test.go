package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// schemaPath is the shipped JSON Schema, relative to this package's directory.
var schemaPath = filepath.Join("..", "..", "schema", "depdog.schema.json")

func loadSchema(t *testing.T) struct {
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
} {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	return s
}

// TestSchemaMatchesFileStruct keeps the shipped schema in lockstep with the
// parser: its top-level properties must be exactly the file struct's fields, so
// adding a config key without updating the schema (or vice versa) fails here.
func TestSchemaMatchesFileStruct(t *testing.T) {
	s := loadSchema(t)

	fields := map[string]bool{}
	ft := reflect.TypeOf(file{})
	for i := 0; i < ft.NumField(); i++ {
		name, _, _ := strings.Cut(ft.Field(i).Tag.Get("yaml"), ",")
		if name != "" {
			fields[name] = true
		}
	}
	for name := range s.Properties {
		if !fields[name] {
			t.Errorf("schema declares property %q with no matching file struct field", name)
		}
	}
	for name := range fields {
		if _, ok := s.Properties[name]; !ok {
			t.Errorf("file struct field %q is missing from the schema", name)
		}
	}
	for _, want := range []string{"version", "components"} {
		if !contains(s.Required, want) {
			t.Errorf("schema should require %q", want)
		}
	}
}

// TestFixtureConfigsConformToSchema checks every committed depdog.yaml uses only
// keys the schema declares — a cheap guard that examples stay valid.
func TestFixtureConfigsConformToSchema(t *testing.T) {
	s := loadSchema(t)
	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", "*", "depdog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no fixture configs found")
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var top map[string]any
		if err := yaml.Unmarshal(data, &top); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for key := range top {
			if _, ok := s.Properties[key]; !ok {
				t.Errorf("%s uses top-level key %q not declared in the schema", path, key)
			}
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
