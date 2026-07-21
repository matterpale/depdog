package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rubySpec loads the illustrative Ruby spec used throughout the engine tests.
func rubySpec(t *testing.T) *Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ruby.yaml"))
	if err != nil {
		t.Fatalf("reading testdata/ruby.yaml: %v", err)
	}
	s, err := Load(data)
	if err != nil {
		t.Fatalf("Load(ruby.yaml): %v", err)
	}
	return s
}

func TestLoadRubySpecRoundTrips(t *testing.T) {
	s := rubySpec(t)

	if s.Name != "rb" {
		t.Errorf("Name = %q, want rb", s.Name)
	}
	if got := strings.Join(s.Markers, ","); got != "Gemfile,.ruby-version,Rakefile,*.gemspec" {
		t.Errorf("Markers = %q", got)
	}
	if len(s.Extensions) != 1 || s.Extensions[0] != ".rb" {
		t.Errorf("Extensions = %v, want [.rb]", s.Extensions)
	}

	// Comments: one line prefix, one line-anchored block comment.
	if len(s.Comments.Line) != 1 || s.Comments.Line[0] != "#" {
		t.Errorf("Comments.Line = %v", s.Comments.Line)
	}
	if len(s.Comments.Block) != 1 {
		t.Fatalf("Comments.Block = %v, want 1", s.Comments.Block)
	}
	if b := s.Comments.Block[0]; b.Open != "=begin" || b.Close != "=end" || !b.LineAnchored || b.Nesting {
		t.Errorf("block comment = %+v", b)
	}

	// Strings: two quoted forms, both multiline with backslash escapes.
	if len(s.Strings) != 2 {
		t.Fatalf("Strings = %v, want 2", s.Strings)
	}
	for _, sf := range s.Strings {
		if sf.kind() != KindQuoted || sf.Escape != "\\" || !sf.Multiline {
			t.Errorf("string form = %+v", sf)
		}
		if sf.closeDelim() != sf.Open {
			t.Errorf("closeDelim(%q) = %q, want == open", sf.Open, sf.closeDelim())
		}
	}

	// Imports: require (plain), require_relative (relative), autoload (skip-to-string).
	if len(s.Imports) != 3 {
		t.Fatalf("Imports = %v, want 3", s.Imports)
	}
	if s.Imports[0].Keyword != "require" || s.Imports[0].Capture != CaptureString || s.Imports[0].kindOf() != "plain" {
		t.Errorf("imports[0] = %+v", s.Imports[0])
	}
	if s.Imports[1].kindOf() != "relative" {
		t.Errorf("imports[1] kind = %q, want relative", s.Imports[1].kindOf())
	}
	auto := s.Imports[2]
	if auto.Keyword != "autoload" || auto.Capture != CaptureSkipToString || auto.SkipTo != "," {
		t.Errorf("imports[2] = %+v", auto)
	}

	// Resolve: path mode, "/" separator, relative kind, lib as a conditional root.
	if s.Resolve.mode() != ModePath || s.Resolve.sep() != "/" {
		t.Errorf("resolve mode/sep = %q/%q", s.Resolve.mode(), s.Resolve.sep())
	}
	if len(s.Resolve.RelativeKinds) != 1 || s.Resolve.RelativeKinds[0] != "relative" {
		t.Errorf("relativeKinds = %v", s.Resolve.RelativeKinds)
	}
	if len(s.Resolve.RootsIfExist) != 1 || s.Resolve.RootsIfExist[0] != "lib" {
		t.Errorf("rootsIfExist = %v", s.Resolve.RootsIfExist)
	}
	if s.Resolve.DropSelfEdges {
		t.Errorf("Ruby keeps self-edges; DropSelfEdges must be false")
	}

	// Stdlib: head matching, a rich table including nested net/* features.
	if s.Stdlib.Match != MatchHead || s.Stdlib.Separator != "/" {
		t.Errorf("stdlib match/sep = %q/%q", s.Stdlib.Match, s.Stdlib.Separator)
	}
	if !contains(s.Stdlib.Modules, "json") || !contains(s.Stdlib.Modules, "net/http") {
		t.Errorf("stdlib modules missing json/net/http")
	}

	// Tests + module label from a gemspec name assignment.
	if len(s.Tests.StemSuffixes) != 2 || len(s.Tests.Dirs) != 2 {
		t.Errorf("tests = %+v", s.Tests)
	}
	if s.Module.FromFile == nil || s.Module.FromFile.Glob != "*.gemspec" || s.Module.FromFile.Key != "name" {
		t.Errorf("module.fromFile = %+v", s.Module.FromFile)
	}
}

func TestLoadRejectsBadSpecs(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string // substring the error must contain
	}{
		{
			name: "missing name",
			yaml: "markers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\n",
			want: "`name` is required",
		},
		{
			name: "no markers",
			yaml: "name: r\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\n",
			want: "`markers`",
		},
		{
			name: "extension without dot",
			yaml: "name: r\nmarkers: [x]\nextensions: [rb]\nimports: [{keyword: require, capture: string}]\n",
			want: "must start with a dot",
		},
		{
			name: "no imports",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\n",
			want: "`imports`",
		},
		{
			name: "bad capture",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: bogus}]\n",
			want: "capture = \"bogus\"",
		},
		{
			name: "skip-to-string without skipTo",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: autoload, capture: skip-to-string}]\n",
			want: "no `skipTo`",
		},
		{
			name: "path-token without separator",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: use, capture: path-token}]\n",
			want: "no `separator`",
		},
		{
			name: "unknown key",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\nbogus: 1\n",
			want: "field bogus not found",
		},
		{
			name: "bad resolve mode",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\nresolve: {mode: sideways}\n",
			want: "resolve.mode",
		},
		{
			name: "stdlib head without separator",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\nstdlib: {match: head}\n",
			want: "stdlib.separator",
		},
		{
			name: "raw-hash missing hash",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\nstrings: [{kind: raw-hash, open: r, quote: \"\\\"\"}]\n",
			want: "needs `hash` and `quote`",
		},
		{
			name: "string capture without a quoted string form",
			yaml: "name: r\nmarkers: [x]\nextensions: [\".r\"]\nimports: [{keyword: require, capture: string}]\n",
			want: "captures a string but no `strings` entry is a quoted form",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Load(%s) succeeded, want error containing %q", tc.name, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestAdapterSchemaIsWellFormed guards that the shipped JSON schema parses and
// carries the top-level required keys the loader enforces, so the two stay in
// step for editor validation.
func TestAdapterSchemaIsWellFormed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "adapter.schema.json"))
	if err != nil {
		t.Fatalf("reading schema/adapter.schema.json: %v", err)
	}
	var doc struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	for _, want := range []string{"name", "markers", "extensions", "imports"} {
		if !contains(doc.Required, want) {
			t.Errorf("schema `required` is missing %q", want)
		}
		if _, ok := doc.Properties[want]; !ok {
			t.Errorf("schema `properties` is missing %q", want)
		}
	}
}
