package spec

import (
	"fmt"
	"reflect"
	"testing"
)

// codeKeywordHits runs the lexer over src and returns "<keyword>@<line>" for
// every occurrence of one of the given import keywords that the lexer offers as
// a *code* word — i.e. outside comments and strings. This is the M1 correctness
// probe: a keyword hidden in a comment or string must never appear here, and a
// keyword in real code must appear at the right line. Returns nil when none hit.
//
// The callback returns false (does not consume), so the lexer skips the
// identifier and keeps scanning; surface *consumption* of arguments/clauses is
// M2's job, not the lexer's, so a real-code `from X import Y` legitimately offers
// both `from` and the inner `import` here.
func codeKeywordHits(sp *Spec, src string, keywords ...string) []string {
	kw := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		kw[k] = true
	}
	var hits []string
	lx := newLexer(sp, []byte(src))
	lx.run(func(l *lexer) bool {
		if w := l.peekWord(); kw[w] {
			hits = append(hits, fmt.Sprintf("%s@%d", w, l.line))
		}
		return false
	})
	return hits
}

// --- language lexer configs (comments + strings only; the lexer reads nothing
// else). Ruby comes from the shipped testdata spec; Rust and Python are built
// inline since they stay hand-written and ship no spec. ---

func rustLexerSpec() *Spec {
	return &Spec{
		Name: "rs-lexer",
		Comments: Comments{
			Line:  []string{"//"},
			Block: []BlockComment{{Open: "/*", Close: "*/", Nesting: true}},
		},
		Strings: []StringForm{
			{Kind: KindRawHash, Open: "r", Hash: "#", Quote: "\""},
			{Kind: KindQuoted, Open: "b\"", Close: "\"", Escape: "\\", Multiline: true},
			{Kind: KindQuoted, Open: "\"", Escape: "\\", Multiline: true},
			{Kind: KindChar, Open: "'", Escape: "\\"},
		},
	}
}

func pythonLexerSpec() *Spec {
	return &Spec{
		Name:     "py-lexer",
		Comments: Comments{Line: []string{"#"}},
		Strings: []StringForm{
			{Kind: KindQuoted, Open: "\"\"\"", Escape: "\\", Multiline: true},
			{Kind: KindQuoted, Open: "'''", Escape: "\\", Multiline: true},
			{Kind: KindQuoted, Open: "\"", Escape: "\\"},
			{Kind: KindQuoted, Open: "'", Escape: "\\"},
		},
	}
}

var rubyKw = []string{"require", "require_relative", "autoload"}
var rustKw = []string{"use", "mod", "extern"}
var pyKw = []string{"import", "from"}

func TestLexerRubyHidesCommentsAndStrings(t *testing.T) {
	sp := rubySpec(t)
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"line comment", `# require "fake"` + "\n", nil},
		{"trailing comment does not eat next line", "x = 1  # require \"nope\"\nrequire \"real\"\n", []string{"require@2"}},
		{"require text inside double-quoted string", "s = \"require fake\"\n", nil},
		{"require text inside single-quoted string", "s = 'require fake'\n", nil},
		{"block comment hides requires", "=begin\nrequire \"fake\"\nrequire_relative \"nope\"\n=end\nrequire \"real\"\n", []string{"require@5"}},
		{"block comment marker only at line start", "x = 1 =begin not a comment\nrequire \"real\"\n", []string{"require@2"}},
		{"require only as a whole word", "requirement = 1\nrequire \"real\"\n", []string{"require@2"}},
		{"escaped quote inside string", "s = \"he said \\\"require x\\\"\"\nrequire \"real\"\n", []string{"require@2"}},
		{"require as a method receiver is still offered", "config.require \"real\"\n", []string{"require@1"}},
		{"unterminated block runs to EOF", "require \"real\"\n=begin\nrequire \"fake\"\n", []string{"require@1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codeKeywordHits(sp, tc.src, rubyKw...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLexerRustHidesCommentsAndStrings(t *testing.T) {
	sp := rustLexerSpec()
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"line comment", "// use fake::thing;\n", nil},
		{"trailing comment", "let x = 1; // use nope::x;\nuse real::thing;\n", []string{"use@2"}},
		{"block comment", "/* use fake::a;\n use fake::b; */\nuse real::c;\n", []string{"use@3"}},
		{"nested block comment", "/* outer /* use fake::a; */ still */\nuse real::c;\n", []string{"use@2"}},
		{"import text inside string", "let s = \"use fake::thing;\";\n", nil},
		{"raw string with hashes", "let s = r#\"use fake::thing;\"#;\nuse real::x;\n", []string{"use@2"}},
		{"escaped quote in string", "let s = \"he said \\\"use x::y;\\\"\";\nuse real::z;\n", []string{"use@2"}},
		{"char literal brace", "let c = '}';\nuse real::a;\n", []string{"use@2"}},
		{"lifetime not a char", "fn f<'a>(x: &'a str) {}\nuse real::b;\n", []string{"use@2"}},
		{"use only at item start", "let use_count = call();\nuse real::c;\n", []string{"use@2"}},
		{"identifier prefixed with use", "let useful = 1;\n", nil},
		{"byte string", "let b = b\"use fake::x;\";\nuse real::d;\n", []string{"use@2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codeKeywordHits(sp, tc.src, rustKw...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLexerPythonHidesCommentsAndStrings(t *testing.T) {
	sp := pythonLexerSpec()
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"line comment", "# import fake\n", nil},
		{"trailing comment does not eat next line", "x = 1  # import nope\nimport real\n", []string{"import@2"}},
		{"import text inside single-quoted string", "s = 'import fake'\n", nil},
		{"import text inside double-quoted string", "s = \"from fake import x\"\n", nil},
		{"triple docstring with import text", "\"\"\"\nimport fake\nfrom fake import x\n\"\"\"\nimport real\n", []string{"import@5"}},
		{"single-quote triple docstring", "'''from a import b'''\nimport real\n", []string{"import@2"}},
		{"import only recognised as a whole word", "result = do_import()\nimport real\n", []string{"import@2"}},
		{"identifier prefixed with import", "importlib_metadata = 1\n", nil},
		{"escaped quote inside string", "s = \"he said \\\"import x\\\"\"\n", nil},
		{"indented import inside a block", "def f():\n    import os\n    return os\n", []string{"import@2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codeKeywordHits(sp, tc.src, pyKw...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestLexerLineTrackingThroughConstructs mirrors each scanner's TestScanLineNumbers:
// a keyword after a multiline comment/string is offered at the correct line, proving
// the lexer counts newlines inside every construct it skips. (Python uses plain
// imports so the from/import interplay — an M2 concern — does not muddy the probe.)
func TestLexerLineTrackingThroughConstructs(t *testing.T) {
	t.Run("ruby =begin/=end block", func(t *testing.T) {
		src := "require \"json\"\n\nrequire_relative \"../a\"\n=begin\nmulti\nline\n=end\nrequire \"set\"\n"
		got := codeKeywordHits(rubySpec(t), src, rubyKw...)
		want := []string{"require@1", "require_relative@3", "require@8"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("hits = %v, want %v", got, want)
		}
	})
	t.Run("rust /* */ block", func(t *testing.T) {
		src := "use std::io;\n\nuse crate::domain::Order;\n/*\nblock\n*/\nuse serde::Serialize;\n"
		got := codeKeywordHits(rustLexerSpec(), src, rustKw...)
		want := []string{"use@1", "use@3", "use@7"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("hits = %v, want %v", got, want)
		}
	})
	t.Run("python triple-quoted docstring", func(t *testing.T) {
		src := "import os\n\nimport a.b.c\n\"\"\"\nmulti\nline\n\"\"\"\nimport sys\n"
		got := codeKeywordHits(pythonLexerSpec(), src, pyKw...)
		want := []string{"import@1", "import@3", "import@8"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("hits = %v, want %v", got, want)
		}
	})
}
