package kotlin

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
	// A trailing `;` is tolerated (optional in Kotlin) and not part of the name.
	res := scan([]byte("package com.example.domain;\n"))
	if res.pkg != "com.example.domain" {
		t.Errorf("pkg = %q, want com.example.domain", res.pkg)
	}
}

func TestScanDefaultPackage(t *testing.T) {
	res := scan([]byte("import kotlin.collections.List\n\nclass Top\n"))
	if res.pkg != "" {
		t.Errorf("pkg = %q, want empty (default package)", res.pkg)
	}
}

func TestScanImportSurfaces(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "class import",
			src:  "package p\nimport a.b.C\n",
			want: []string{"a.b.C"},
		},
		{
			name: "wildcard import",
			src:  "package p\nimport a.b.*\n",
			want: []string{"a.b.*"},
		},
		{
			name: "aliased import keeps the imported symbol, drops the alias",
			src:  "package p\nimport a.b.C as D\n",
			want: []string{"a.b.C"},
		},
		{
			name: "function/member import",
			src:  "package p\nimport a.b.C.member\n",
			want: []string{"a.b.C.member"},
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
	// The captured Pkg field drops the trailing symbol segment; a wildcard keeps
	// the whole path.
	tests := []struct {
		src     string
		wantPkg string
	}{
		{"import a.b.C", "a.b"},
		{"import a.b.C as D", "a.b"},
		{"import a.b.*", "a.b"},
		{"import a.b.C.member", "a.b.C"},
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
	// Kotlin block comments nest: the inner close must not end the outer one.
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

func TestScanIgnoresRawString(t *testing.T) {
	// Kotlin `"""..."""` raw strings must not be scanned for imports.
	src := "package p\n" +
		"import real.One\n" +
		"class X {\n" +
		"    val block = \"\"\"\n" +
		"        import fake.FromRawString\n" +
		"        still \"inside\" the block\n" +
		"        \"\"\"\n" +
		"}\n" +
		"import real.Two\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"real.One", "real.Two"}) {
		t.Errorf("imports = %v, want [real.One real.Two] (raw string ignored)", got)
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
	// Kotlin allows block comments between the dotted segments of a name.
	src := "package p\nimport a . /* c */ b . C\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.b.C"}) {
		t.Errorf("imports = %v, want [a.b.C]", got)
	}
}

func TestScanImportDoesNotCrossNewline(t *testing.T) {
	// Kotlin imports end at the newline: a bare identifier on the next line is
	// not absorbed into the previous import's dotted chain.
	src := "package p\nimport a.b.C\nData\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.b.C"}) {
		t.Errorf("imports = %v, want [a.b.C] (import must not cross the newline)", got)
	}
}

func TestScanBacktickIdentifier(t *testing.T) {
	// A backtick-quoted segment (a soft/hard keyword used as a name) is a valid
	// identifier; the backticks are dropped from the captured specifier.
	src := "package p\nimport a.`fun`.C\n"
	if got := impPaths(src); !reflect.DeepEqual(got, []string{"a.fun.C"}) {
		t.Errorf("imports = %v, want [a.fun.C]", got)
	}
}
