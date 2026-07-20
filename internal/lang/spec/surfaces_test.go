package spec

import (
	"fmt"
	"reflect"
	"testing"
)

// importPairs renders an extraction's imports to "<kind>:<specifier>" in source
// order, mirroring the hand-written scanners' refStrings helpers.
func importPairs(x extraction) []string {
	if len(x.imports) == 0 {
		return nil
	}
	out := make([]string, 0, len(x.imports))
	for _, r := range x.imports {
		out = append(out, r.Kind+":"+r.Specifier)
	}
	return out
}

// importLines renders imports to "<specifier>@<line>".
func importLines(x extraction) []string {
	out := make([]string, 0, len(x.imports))
	for _, r := range x.imports {
		out = append(out, fmt.Sprintf("%s@%d", r.Specifier, r.Line))
	}
	return out
}

func provideSpecs(x extraction) []string {
	if len(x.provides) == 0 {
		return nil
	}
	out := make([]string, 0, len(x.provides))
	for _, r := range x.provides {
		out = append(out, r.Specifier)
	}
	return out
}

// TestExtractRubySurfaces ports ruby/scan_test.go's TestScanRequireForms and
// proves the declarative engine extracts the same specifiers. Ruby's autoload and
// require both classify identically (plain), so both carry the "plain" kind here;
// the hand-written scanner's separate auto/req labels were cosmetic.
func TestExtractRubySurfaces(t *testing.T) {
	sp := rubySpec(t)
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"double-quoted require", `require "json"` + "\n", []string{"plain:json"}},
		{"single-quoted require", "require 'set'\n", []string{"plain:set"}},
		{"nested feature require", `require "net/http"` + "\n", []string{"plain:net/http"}},
		{"require with parentheses", `require("logger")` + "\n", []string{"plain:logger"}},
		{"require_relative up one dir", `require_relative "../domain/order"` + "\n", []string{"relative:../domain/order"}},
		{"require_relative sibling", `require_relative "orders"` + "\n", []string{"relative:orders"}},
		{"autoload symbol then string", `autoload :Order, "domain/order"` + "\n", []string{"plain:domain/order"}},
		{"autoload with parentheses", `autoload(:Order, "domain/order")` + "\n", []string{"plain:domain/order"}},
		{"require with trailing modifier", `require "logger" if verbose` + "\n", []string{"plain:logger"}},
		{"require with trailing comment", `require "json"  # the JSON module` + "\n", []string{"plain:json"}},
		{"several statements", "require \"json\"\nrequire_relative \"./a\"\nrequire \"set\"\n", []string{"plain:json", "relative:./a", "plain:set"}},
		{"indented require inside a method", "def load\n  require \"json\"\nend\n", []string{"plain:json"}},
		{"dynamic require is ignored", "require File.join(dir, \"thing\")\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := importPairs(extract(sp, []byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("importPairs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExtractRubyIgnoresHidden confirms surface extraction inherits the lexer's
// comment/string blindness: a require hidden in a comment or string yields no ref.
func TestExtractRubyIgnoresHidden(t *testing.T) {
	sp := rubySpec(t)
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"line comment", `# require "fake"` + "\n", nil},
		{"string content", "s = \"require fake\"\n", nil},
		{"block comment hides, real survives", "=begin\nrequire \"fake\"\n=end\nrequire \"real\"\n", []string{"plain:real"}},
		{"escaped quote then real require", "s = \"he said \\\"require x\\\"\"\nrequire \"real\"\n", []string{"plain:real"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := importPairs(extract(sp, []byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("importPairs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExtractRubyLineNumbers ports ruby/scan_test.go's TestScanLineNumbers.
func TestExtractRubyLineNumbers(t *testing.T) {
	src := "require \"json\"\n\nrequire_relative \"../a\"\n=begin\nmulti\nline\n=end\nrequire \"set\"\n"
	got := importLines(extract(rubySpec(t), []byte(src)))
	want := []string{"json@1", "../a@3", "set@8"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importLines = %v, want %v", got, want)
	}
}

// pathTokenSpec builds a spec exercising path-token capture with a given
// separator and terminator, plus // and /* */ comments and "..." strings so
// hiding still applies.
func pathTokenSpec(keyword, sep string, term Terminator) *Spec {
	return &Spec{
		Name:     "pt",
		Comments: Comments{Line: []string{"//"}, Block: []BlockComment{{Open: "/*", Close: "*/"}}},
		Strings:  []StringForm{{Kind: KindQuoted, Open: "\"", Escape: "\\"}},
		Imports:  []Surface{{Keyword: keyword, Capture: CapturePathToken, Separator: sep, Terminator: term, Kind: "plain"}},
	}
}

func TestExtractPathToken(t *testing.T) {
	tests := []struct {
		name string
		spec *Spec
		src  string
		want []string
	}{
		{"dotted using semicolon", pathTokenSpec("using", ".", TermSemicolon), "using System.Text;\n", []string{"plain:System.Text"}},
		{"deep dotted", pathTokenSpec("using", ".", TermSemicolon), "using System.Collections.Generic;\n", []string{"plain:System.Collections.Generic"}},
		{"whitespace around separator", pathTokenSpec("using", ".", TermSemicolon), "using System . Text ;\n", []string{"plain:System.Text"}},
		{"two statements", pathTokenSpec("using", ".", TermSemicolon), "using A.B;\nusing C.D;\n", []string{"plain:A.B", "plain:C.D"}},
		{"colon-colon path", pathTokenSpec("use", "::", TermSemicolon), "use a::b::c;\n", []string{"plain:a::b::c"}},
		{"newline-terminated import", pathTokenSpec("import", ".", TermNewline), "import Foo.Bar\n", []string{"plain:Foo.Bar"}},
		{"newline import with alias tail", pathTokenSpec("import", ".", TermNewline), "import Foo.Bar as FB\n", []string{"plain:Foo.Bar"}},
		{"keyword in comment is hidden", pathTokenSpec("using", ".", TermSemicolon), "// using Fake.Thing;\nusing Real.Thing;\n", []string{"plain:Real.Thing"}},
		{"keyword in string is hidden", pathTokenSpec("using", ".", TermSemicolon), "var s = \"using Fake;\";\nusing Real.X;\n", []string{"plain:Real.X"}},
		{"prefixed identifier is not the keyword", pathTokenSpec("using", ".", TermSemicolon), "usings.Add(x);\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := importPairs(extract(tc.spec, []byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("importPairs = %v, want %v", got, tc.want)
			}
		})
	}
}

// csharpishSpec previews the C# (M5) surface: file/block-scoped `namespace`
// declarations feeding provides, and `using` directives as imports.
func csharpishSpec() *Spec {
	return &Spec{
		Name:     "cs-ish",
		Comments: Comments{Line: []string{"//"}, Block: []BlockComment{{Open: "/*", Close: "*/"}}},
		Strings:  []StringForm{{Kind: KindQuoted, Open: "\"", Escape: "\\"}},
		Imports:  []Surface{{Keyword: "using", Capture: CapturePathToken, Separator: ".", Terminator: TermSemicolon, Kind: "plain"}},
		Provides: &Surface{Keyword: "namespace", Capture: CapturePathToken, Separator: ".", Terminator: TermBrace, Kind: "declare"},
	}
}

func TestExtractProvidesAndImports(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		wantImports  []string
		wantProvides []string
	}{
		{
			name:         "block-scoped namespace with a using inside",
			src:          "namespace App.Svc\n{\n    using System.Text;\n}\n",
			wantImports:  []string{"plain:System.Text"},
			wantProvides: []string{"App.Svc"},
		},
		{
			name:         "file-scoped namespace then using",
			src:          "namespace App.Svc;\nusing System.Text;\n",
			wantImports:  []string{"plain:System.Text"},
			wantProvides: []string{"App.Svc"},
		},
		{
			name:         "using hidden in a block comment",
			src:          "namespace N;\n/* using Fake.Thing; */\nusing Real.Thing;\n",
			wantImports:  []string{"plain:Real.Thing"},
			wantProvides: []string{"N"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := extract(csharpishSpec(), []byte(tc.src))
			if got := importPairs(x); !reflect.DeepEqual(got, tc.wantImports) {
				t.Errorf("imports = %v, want %v", got, tc.wantImports)
			}
			if got := provideSpecs(x); !reflect.DeepEqual(got, tc.wantProvides) {
				t.Errorf("provides = %v, want %v", got, tc.wantProvides)
			}
		})
	}
}
