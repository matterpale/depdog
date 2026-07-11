package scala

import (
	"reflect"
	"testing"
)

// impPaths returns the display specifiers of a scan, in source order.
func impPaths(src string) []string {
	res := scan([]byte(src))
	out := make([]string, 0, len(res.imports))
	for _, ref := range res.imports {
		out = append(out, ref.Display)
	}
	return out
}

func TestScanPackageDeclaration(t *testing.T) {
	res := scan([]byte("package com.example.domain\n\nclass Order\n"))
	if res.pkg != "com.example.domain" {
		t.Errorf("pkg = %q, want com.example.domain", res.pkg)
	}
}

func TestScanPackageWithTrailingSemicolon(t *testing.T) {
	// A trailing `;` is tolerated (optional in Scala) and not part of the name.
	res := scan([]byte("package com.example.domain;\n"))
	if res.pkg != "com.example.domain" {
		t.Errorf("pkg = %q, want com.example.domain", res.pkg)
	}
}

func TestScanEmptyPackage(t *testing.T) {
	res := scan([]byte("import scala.collection.mutable.Map\n\nclass Top\n"))
	if res.pkg != "" {
		t.Errorf("pkg = %q, want empty (empty package)", res.pkg)
	}
}

func TestScanPackageObjectIgnored(t *testing.T) {
	// `package object foo` declares an object, not a package path; the file has no
	// leading package clause in this case.
	res := scan([]byte("package object util {\n  val x = 1\n}\n"))
	if res.pkg != "" {
		t.Errorf("pkg = %q, want empty (package object is not a package path)", res.pkg)
	}
}

func TestScanImportSurfaces(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "single import",
			src:  "package p\nimport a.b.C\n",
			want: []string{"a.b.C"},
		},
		{
			name: "scala 2 wildcard",
			src:  "package p\nimport a.b._\n",
			want: []string{"a.b._"},
		},
		{
			name: "scala 3 wildcard",
			src:  "package p\nimport a.b.*\n",
			want: []string{"a.b.*"},
		},
		{
			name: "given import",
			src:  "package p\nimport a.b.given\n",
			want: []string{"a.b.given"},
		},
		{
			name: "selector group",
			src:  "package p\nimport a.b.{C, D}\n",
			want: []string{"a.b.C", "a.b.D"},
		},
		{
			name: "renamed selector drops the alias",
			src:  "package p\nimport a.b.{C => E}\n",
			want: []string{"a.b.C"},
		},
		{
			name: "selector group with rename and plain",
			src:  "package p\nimport a.b.{C => E, D}\n",
			want: []string{"a.b.C", "a.b.D"},
		},
		{
			name: "selector group with wildcard catch-all",
			src:  "package p\nimport a.b.{C, _}\n",
			want: []string{"a.b.C", "a.b._"},
		},
		{
			name: "given selector in a group",
			src:  "package p\nimport a.b.{given, C}\n",
			want: []string{"a.b.given", "a.b.C"},
		},
		{
			name: "given by type in a group",
			src:  "package p\nimport a.b.{given Ordering}\n",
			want: []string{"a.b.given"},
		},
		{
			name: "multiple imports in source order",
			src:  "package p\nimport z.Z\nimport a.A\n",
			want: []string{"z.Z", "a.A"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := impPaths(tc.src); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("imports = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScanImportPackageResolution(t *testing.T) {
	// The captured Pkg field drops the trailing symbol segment; a wildcard/given
	// keeps the whole path.
	tests := []struct {
		src     string
		wantPkg string
	}{
		{"import a.b.C", "a.b"},
		{"import a.b._", "a.b"},
		{"import a.b.*", "a.b"},
		{"import a.b.given", "a.b"},
		{"import a.b.{C => D}", "a.b"},
	}
	for _, tc := range tests {
		res := scan([]byte("package p\n" + tc.src + "\n"))
		if len(res.imports) != 1 {
			t.Fatalf("%q: got %d imports", tc.src, len(res.imports))
		}
		if res.imports[0].Pkg != tc.wantPkg {
			t.Errorf("%q: Pkg = %q, want %q", tc.src, res.imports[0].Pkg, tc.wantPkg)
		}
	}
}

func TestScanSelectorGroupPackages(t *testing.T) {
	// Every member of a selector group resolves to the group's package.
	res := scan([]byte("package p\nimport a.b.{C, D, E}\n"))
	if len(res.imports) != 3 {
		t.Fatalf("got %d imports, want 3", len(res.imports))
	}
	for _, ref := range res.imports {
		if ref.Pkg != "a.b" {
			t.Errorf("selector %q: Pkg = %q, want a.b", ref.Display, ref.Pkg)
		}
	}
}

func TestScanLineNumbers(t *testing.T) {
	src := "package p\n\n// a comment\nimport a.B\n\nimport c.D\n"
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

func TestScanIgnoresComments(t *testing.T) {
	src := `package p
// import fake.Line
/* import fake.Block
   import fake.Block2 */
import real.Class
/** import fake.Doc */
`
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.Class"}) {
		t.Errorf("imports = %v, want [real.Class] (comments ignored)", got)
	}
}

func TestScanIgnoresNestedBlockComment(t *testing.T) {
	// Scala block comments nest: the inner close must not end the outer one.
	src := `package p
/* outer /* inner import fake.Nested */ still commented import fake.Also */
import real.One
`
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.One"}) {
		t.Errorf("imports = %v, want [real.One] (nested block comment ignored)", got)
	}
}

func TestScanIgnoresStrings(t *testing.T) {
	src := `package p
import real.One
class X {
    val s = "import fake.FromString"
    val c = ';'
    val t = "not; a; statement; import fake.Two"
}
import real.Two
`
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.One", "real.Two"}) {
		t.Errorf("imports = %v, want [real.One real.Two] (strings/chars ignored)", got)
	}
}

func TestScanIgnoresInterpolatedString(t *testing.T) {
	// s"…" / f"…" interpolated strings must not be scanned for imports.
	src := "package p\n" +
		"import real.One\n" +
		"class X {\n" +
		"    val name = \"deps\"\n" +
		"    val s = s\"import fake.FromInterp $name here\"\n" +
		"}\n" +
		"import real.Two\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.One", "real.Two"}) {
		t.Errorf("imports = %v, want [real.One real.Two] (interpolated string ignored)", got)
	}
}

func TestScanIgnoresTripleQuotedString(t *testing.T) {
	// Scala `"""..."""` triple-quoted strings must not be scanned for imports.
	src := "package p\n" +
		"import real.One\n" +
		"class X {\n" +
		"    val block = \"\"\"\n" +
		"        import fake.FromTriple\n" +
		"        still \"inside\" the block\n" +
		"        \"\"\"\n" +
		"}\n" +
		"import real.Two\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.One", "real.Two"}) {
		t.Errorf("imports = %v, want [real.One real.Two] (triple-quoted string ignored)", got)
	}
}

func TestScanImportNotAtStatementStart(t *testing.T) {
	// `import` as part of an identifier mid-expression must not be captured as a
	// statement.
	src := "package p\nclass X { val import_count = 3 }\n"
	if got := impPaths(src); len(got) != 0 {
		t.Errorf("imports = %v, want none", got)
	}
}

func TestScanCommentBetweenDots(t *testing.T) {
	// Scala allows block comments between the dotted segments of a name.
	src := "package p\nimport a . /* c */ b . C\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.b.C"}) {
		t.Errorf("imports = %v, want [a.b.C]", got)
	}
}

func TestScanImportDoesNotCrossNewline(t *testing.T) {
	// Scala imports end at the newline: a bare identifier on the next line is not
	// absorbed into the previous import's dotted chain.
	src := "package p\nimport a.b.C\nData\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.b.C"}) {
		t.Errorf("imports = %v, want [a.b.C] (import must not cross the newline)", got)
	}
}

func TestScanBacktickIdentifier(t *testing.T) {
	// A backtick-quoted segment (a keyword used as a name) is a valid identifier;
	// the backticks are dropped from the captured specifier.
	src := "package p\nimport a.`type`.C\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.type.C"}) {
		t.Errorf("imports = %v, want [a.type.C]", got)
	}
}

func TestScanSelectorGroupSpanningLines(t *testing.T) {
	// A selector group may span multiple lines; all members are captured.
	src := "package p\nimport a.b.{\n  C,\n  D => E,\n  F\n}\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.b.C", "a.b.D", "a.b.F"}) {
		t.Errorf("imports = %v, want [a.b.C a.b.D a.b.F]", got)
	}
}

func TestScanSymbolLiteralNotMisread(t *testing.T) {
	// A lone tick (e.g. a legacy symbol literal 'name) must not swallow the rest
	// of the file or a following import.
	src := "package p\nval sym = 'foo\nimport real.One\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.One"}) {
		t.Errorf("imports = %v, want [real.One] (symbol literal handled)", got)
	}
}
