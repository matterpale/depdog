package elm

import (
	"reflect"
	"testing"
)

// impModules returns the imported module names of a scan, in source order.
func impModules(src string) []string {
	res := scan([]byte(src))
	out := make([]string, 0, len(res.imports))
	for _, ref := range res.imports {
		out = append(out, ref.Module)
	}
	return out
}

func TestScanModuleDeclaration(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "plain module",
			src:  "module Foo.Bar exposing (..)\n",
			want: "Foo.Bar",
		},
		{
			name: "port module",
			src:  "port module Foo.Bar exposing (..)\n",
			want: "Foo.Bar",
		},
		{
			name: "effect module",
			src:  "effect module Foo.Bar where { command = MyCmd } exposing (..)\n",
			want: "Foo.Bar",
		},
		{
			name: "single-segment module",
			src:  "module Main exposing (main)\n",
			want: "Main",
		},
		{
			name: "no module header",
			src:  "import List\n\nx = 1\n",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scan([]byte(tc.src)).module; got != tc.want {
				t.Errorf("module = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScanImportSurfaces(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "plain import",
			src:  "module M exposing (..)\nimport Foo.Bar\n",
			want: []string{"Foo.Bar"},
		},
		{
			name: "import exposing all",
			src:  "module M exposing (..)\nimport Foo.Bar exposing (..)\n",
			want: []string{"Foo.Bar"},
		},
		{
			name: "import exposing list",
			src:  "module M exposing (..)\nimport Foo.Bar exposing (a, b)\n",
			want: []string{"Foo.Bar"},
		},
		{
			name: "import as alias",
			src:  "module M exposing (..)\nimport Foo.Bar as FB\n",
			want: []string{"Foo.Bar"},
		},
		{
			name: "import as alias exposing list",
			src:  "module M exposing (..)\nimport Foo.Bar as FB exposing (x, y)\n",
			want: []string{"Foo.Bar"},
		},
		{
			name: "single-segment import",
			src:  "module M exposing (..)\nimport List\n",
			want: []string{"List"},
		},
		{
			name: "deeply nested module import",
			src:  "module M exposing (..)\nimport A.B.C.D exposing (thing)\n",
			want: []string{"A.B.C.D"},
		},
		{
			name: "multiple imports in source order",
			src:  "module M exposing (..)\nimport Zebra\nimport Apple\n",
			want: []string{"Zebra", "Apple"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := impModules(tc.src); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("imports = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScanExposingListNotMisread(t *testing.T) {
	// The names inside `exposing (...)` — especially a multi-line one — must never
	// be captured as imports themselves.
	src := "module M exposing (..)\n" +
		"import Foo.Bar\n" +
		"    exposing\n" +
		"        ( alpha\n" +
		"        , beta\n" +
		"        , gamma\n" +
		"        )\n" +
		"import Baz.Qux\n"
	// Note: an Elm import's `exposing` clause on continuation lines is still part
	// of the same statement; but even scanned as separate lines, `alpha`/`beta`
	// are lowercase identifiers, not `import` keywords, so they never produce an
	// edge. Both real module names are captured.
	got := impModules(src)
	want := []string{"Foo.Bar", "Baz.Qux"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("imports = %v, want %v", got, want)
	}
}

func TestScanIgnoresLineComments(t *testing.T) {
	src := "module M exposing (..)\n" +
		"-- import Fake.Line\n" +
		"import Real.One\n" +
		"x = 1 -- import Fake.Trailing\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One"}) {
		t.Errorf("imports = %v, want [Real.One] (line comments ignored)", got)
	}
}

func TestScanIgnoresBlockComment(t *testing.T) {
	src := "module M exposing (..)\n" +
		"{- import Fake.Block\n" +
		"   import Fake.Block2 -}\n" +
		"import Real.One\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One"}) {
		t.Errorf("imports = %v, want [Real.One] (block comment ignored)", got)
	}
}

func TestScanIgnoresNestedBlockComment(t *testing.T) {
	// Elm block comments NEST: the first inner `-}` closes only the inner comment,
	// so the whole span up to the balancing outer `-}` is a comment.
	src := "module M exposing (..)\n" +
		"{- outer {- inner import Fake.Nested -} still commented import Fake.Also -}\n" +
		"import Real.One\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One"}) {
		t.Errorf("imports = %v, want [Real.One] (nested block comment ignored)", got)
	}
}

func TestScanNestedBlockCommentDeeper(t *testing.T) {
	// Three levels of nesting: only the final balancing `-}` ends the comment.
	src := "module M exposing (..)\n" +
		"{- a {- b {- c import Fake.Deep -} b -} a import Fake.Mid -}\n" +
		"import Real.Two\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.Two"}) {
		t.Errorf("imports = %v, want [Real.Two] (deeply nested block comment ignored)", got)
	}
}

func TestScanIgnoresStrings(t *testing.T) {
	src := "module M exposing (..)\n" +
		"import Real.One\n" +
		"greeting = \"import Fake.FromString\"\n" +
		"escaped = \"a quote \\\" import Fake.Escaped\"\n" +
		"import Real.Two\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One", "Real.Two"}) {
		t.Errorf("imports = %v, want [Real.One Real.Two] (strings ignored)", got)
	}
}

func TestScanIgnoresTripleQuotedString(t *testing.T) {
	// A `"""..."""` multiline string must not be scanned for imports even though it
	// spans lines that begin with `import`.
	src := "module M exposing (..)\n" +
		"import Real.One\n" +
		"doc = \"\"\"\n" +
		"import Fake.FromTriple\n" +
		"import Fake.AlsoFake\n" +
		"\"\"\"\n" +
		"import Real.Two\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One", "Real.Two"}) {
		t.Errorf("imports = %v, want [Real.One Real.Two] (triple-quoted string ignored)", got)
	}
}

func TestScanIgnoresCharLiteral(t *testing.T) {
	// A `'x'` char literal (including an escaped quote) must not derail scanning or
	// swallow a following import.
	src := "module M exposing (..)\n" +
		"import Real.One\n" +
		"c = '\\''\n" +
		"d = '\"'\n" +
		"import Real.Two\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One", "Real.Two"}) {
		t.Errorf("imports = %v, want [Real.One Real.Two] (char literals ignored)", got)
	}
}

func TestScanImportNotAtLineStart(t *testing.T) {
	// `import` appearing mid-line (not in statement position) is not an import
	// statement. Real Elm forbids this, but the scanner must not be fooled.
	src := "module M exposing (..)\n" +
		"x = 1\n" +
		"    import_helper = 2\n"
	if got := impModules(src); len(got) != 0 {
		t.Errorf("imports = %v, want none", got)
	}
}

func TestScanPortAndEffectNotConfusedWithIdentifiers(t *testing.T) {
	// A bare `port` declaration or an `effect`-prefixed identifier at line start is
	// NOT a module header and must be skipped without capturing a module.
	src := "port module M exposing (..)\n" +
		"port sendData : String -> Cmd msg\n" +
		"import Real.One\n"
	res := scan([]byte(src))
	if res.module != "M" {
		t.Errorf("module = %q, want M", res.module)
	}
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One"}) {
		t.Errorf("imports = %v, want [Real.One]", got)
	}
}

func TestScanLineNumbers(t *testing.T) {
	src := "module M exposing (..)\n" +
		"\n" +
		"-- a comment\n" +
		"import A.B\n" +
		"\n" +
		"import C.D\n"
	res := scan([]byte(src))
	if len(res.imports) != 2 {
		t.Fatalf("got %d imports", len(res.imports))
	}
	if res.imports[0].Line != 4 {
		t.Errorf("first import line = %d, want 4", res.imports[0].Line)
	}
	if res.imports[1].Line != 6 {
		t.Errorf("second import line = %d, want 6", res.imports[1].Line)
	}
}

func TestScanBlockCommentBetweenKeywordAndName(t *testing.T) {
	// An Elm block comment may sit between the `import` keyword and the module
	// name; the name is still captured.
	src := "module M exposing (..)\nimport {- note -} Real.One\n"
	if got := impModules(src); !reflect.DeepEqual(got, []string{"Real.One"}) {
		t.Errorf("imports = %v, want [Real.One]", got)
	}
}
