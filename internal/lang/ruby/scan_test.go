package ruby

import (
	"reflect"
	"testing"
)

// refStrings renders scanned refs to a comparable, source-order slice of
// "<kind>:<feature>" so table cases stay readable.
func refStrings(refs []importRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		var kind string
		switch r.Kind {
		case kindRequire:
			kind = "req"
		case kindRelative:
			kind = "rel"
		case kindAutoload:
			kind = "auto"
		}
		out = append(out, kind+":"+r.Feature)
	}
	return out
}

func TestScanRequireForms(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "double-quoted require",
			src:  `require "json"` + "\n",
			want: []string{"req:json"},
		},
		{
			name: "single-quoted require",
			src:  "require 'set'\n",
			want: []string{"req:set"},
		},
		{
			name: "nested feature require",
			src:  `require "net/http"` + "\n",
			want: []string{"req:net/http"},
		},
		{
			name: "require with parentheses",
			src:  `require("logger")` + "\n",
			want: []string{"req:logger"},
		},
		{
			name: "require_relative up one dir",
			src:  `require_relative "../domain/order"` + "\n",
			want: []string{"rel:../domain/order"},
		},
		{
			name: "require_relative sibling",
			src:  `require_relative "orders"` + "\n",
			want: []string{"rel:orders"},
		},
		{
			name: "autoload symbol then string",
			src:  `autoload :Order, "domain/order"` + "\n",
			want: []string{"auto:domain/order"},
		},
		{
			name: "autoload with parentheses",
			src:  `autoload(:Order, "domain/order")` + "\n",
			want: []string{"auto:domain/order"},
		},
		{
			name: "require with trailing modifier",
			src:  `require "logger" if verbose` + "\n",
			want: []string{"req:logger"},
		},
		{
			name: "require with trailing comment",
			src:  `require "json"  # the JSON module` + "\n",
			want: []string{"req:json"},
		},
		{
			name: "several statements",
			src:  "require \"json\"\nrequire_relative \"./a\"\nrequire \"set\"\n",
			want: []string{"req:json", "rel:./a", "req:set"},
		},
		{
			name: "indented require inside a method",
			src:  "def load\n  require \"json\"\nend\n",
			want: []string{"req:json"},
		},
		{
			name: "dynamic require is ignored",
			src:  "require File.join(dir, \"thing\")\n",
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := refStrings(scan([]byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scan(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestScanIgnoresCommentsAndStrings(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "line comment",
			src:  `# require "fake"` + "\n",
			want: nil,
		},
		{
			name: "trailing comment does not eat next line",
			src:  "x = 1  # require \"nope\"\nrequire \"real\"\n",
			want: []string{"req:real"},
		},
		{
			name: "require text inside a double-quoted string",
			src:  "s = \"require fake\"\n",
			want: nil,
		},
		{
			name: "require text inside a single-quoted string",
			src:  "s = 'require fake'\n",
			want: nil,
		},
		{
			name: "block comment hides requires",
			src:  "=begin\nrequire \"fake\"\nrequire_relative \"nope\"\n=end\nrequire \"real\"\n",
			want: []string{"req:real"},
		},
		{
			name: "block comment marker only at line start",
			src:  "x = 1 =begin not a comment\nrequire \"real\"\n",
			want: []string{"req:real"},
		},
		{
			name: "require keyword only recognised as a whole word",
			src:  "requirement = 1\nrequire \"real\"\n",
			want: []string{"req:real"},
		},
		{
			name: "escaped quote inside string",
			src:  "s = \"he said \\\"require x\\\"\"\nrequire \"real\"\n",
			want: []string{"req:real"},
		},
		{
			name: "require token as a method receiver is not a load",
			src:  "config.require \"real\"\n",
			want: []string{"req:real"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := refStrings(scan([]byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scan(%q) = %v, want %v", tc.src, got, tc.want)
			}
		})
	}
}

func TestScanLineNumbers(t *testing.T) {
	src := "require \"json\"\n\nrequire_relative \"../a\"\n=begin\nmulti\nline\n=end\nrequire \"set\"\n"
	refs := scan([]byte(src))
	if len(refs) != 3 {
		t.Fatalf("want 3 refs, got %d: %v", len(refs), refStrings(refs))
	}
	wantLines := []int{1, 3, 8}
	for i, r := range refs {
		if r.Line != wantLines[i] {
			t.Errorf("ref %d (%q) line = %d, want %d", i, r.Feature, r.Line, wantLines[i])
		}
	}
}

func TestScanBlockCommentUnterminatedRunsToEOF(t *testing.T) {
	// An unterminated =begin block swallows everything after it; no edges leak.
	src := "require \"real\"\n=begin\nrequire \"fake\"\n"
	got := refStrings(scan([]byte(src)))
	want := []string{"req:real"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scan = %v, want %v", got, want)
	}
}
