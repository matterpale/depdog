package spec

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func csharpSpec(t *testing.T) *Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "csharp.yaml"))
	if err != nil {
		t.Fatalf("reading csharp.yaml: %v", err)
	}
	sp, err := Load(data)
	if err != nil {
		t.Fatalf("Load(csharp.yaml): %v", err)
	}
	return sp
}

// TestCsharpLexerHidesUsings proves the C# string/comment forms — including
// verbatim (@"..""..), interpolated ($"..."), raw ("""..."""), and char literals
// — hide a `using` so it is never offered as a code word.
func TestCsharpLexerHidesUsings(t *testing.T) {
	sp := csharpSpec(t)
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"line comment", "// using Fake.X;\nusing Real.X;\n", []string{"using@2"}},
		{"block comment", "/* using Fake.X; */\nusing Real.X;\n", []string{"using@2"}},
		{"normal string", "var s = \"using Fake;\";\nusing Real.X;\n", []string{"using@2"}},
		{"verbatim string with doubled quote", "var s = @\"using \"\"Fake\"\";\";\nusing Real.X;\n", []string{"using@2"}},
		{"interpolated string", "var s = $\"using {x} Fake\";\nusing Real.X;\n", []string{"using@2"}},
		{"raw string spans lines", "var s = \"\"\"\nusing Fake.X;\n\"\"\";\nusing Real.X;\n", []string{"using@4"}},
		{"char literal holding a quote", "var c = '\"';\nusing Real.X;\n", []string{"using@2"}},
		{"identifier prefixed with using", "usings.Add(x);\nusing Real.X;\n", []string{"using@2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codeKeywordHits(sp, tc.src, "using")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCsharpUsingForms exercises every using directive shape plus the using
// *statement* forms that must NOT become edges.
func TestCsharpUsingForms(t *testing.T) {
	sp := csharpSpec(t)
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"plain using", "using System.Text;\n", []string{"plain:System.Text"}},
		{"deep namespace", "using System.Collections.Generic;\n", []string{"plain:System.Collections.Generic"}},
		{"global using", "global using System;\n", []string{"plain:System"}},
		{"using static", "using static System.Math;\n", []string{"plain:System.Math"}},
		{"using alias captures the target", "using Json = System.Text.Json;\n", []string{"plain:System.Text.Json"}},
		{"several usings", "using System;\nusing App.Svc;\n", []string{"plain:System", "plain:App.Svc"}},
		{"using resource statement is not a directive", "using (var x = Open()) { Work(); }\n", nil},
		{"using var declaration is not a directive", "using var f = Open();\n", nil},
		{"typed using declaration is not a directive", "using StreamReader r = new(path);\n", nil},
		{"using in a comment is hidden", "// using Fake;\nusing Real;\n", []string{"plain:Real"}},
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

func TestCsharpNamespaceProvides(t *testing.T) {
	sp := csharpSpec(t)
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"file-scoped namespace", "namespace App.Domain;\nusing System;\n", []string{"App.Domain"}},
		{"block-scoped namespace", "namespace App.Domain\n{\n    using System;\n}\n", []string{"App.Domain"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := provideSpecs(extract(sp, []byte(tc.src)))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("provideSpecs = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCsharpNameIndexGraph proves the name-index resolver: a `using` is in-module
// when a project file declares that namespace, std under System, else external.
func TestCsharpNameIndexGraph(t *testing.T) {
	root := setupProject(t, map[string]string{
		"App.csproj":      "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>\n",
		"Domain/Order.cs": "namespace App.Domain;\nusing System;\n",
		"Services/OrderService.cs": "namespace App.Services;\n" +
			"using App.Domain;\nusing System.Text;\nusing Newtonsoft.Json;\n",
		"Services/Helper.cs": "namespace App.Services;\n",
	})
	g, err := (&Loader{Spec: csharpSpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if g.ModulePath != filepath.Base(root) {
		t.Errorf("ModulePath = %q, want dir basename %q", g.ModulePath, filepath.Base(root))
	}

	if imp := findImport(findPkg(g, "Domain"), "System"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("Domain->System should be std, got %+v", imp)
	}
	svc := findPkg(g, "Services")
	if imp := findImport(svc, "App.Domain"); imp == nil || imp.Class != core.ClassInModule || imp.RelDir != "Domain" {
		t.Errorf("Services->App.Domain should be in-module Domain, got %+v", imp)
	}
	if imp := findImport(svc, "System.Text"); imp == nil || imp.Class != core.ClassStd {
		t.Errorf("Services->System.Text should be std, got %+v", imp)
	}
	if imp := findImport(svc, "Newtonsoft.Json"); imp == nil || imp.Class != core.ClassExternal {
		t.Errorf("Services->Newtonsoft.Json should be external, got %+v", imp)
	}
}

// TestCsharpNamespaceSpanningDirs pins the deterministic choice when a namespace
// is declared in more than one directory: the edge points to the first (sorted)
// declaring directory.
func TestCsharpNamespaceSpanningDirs(t *testing.T) {
	root := setupProject(t, map[string]string{
		"App.csproj": "<Project></Project>\n",
		"A/Foo.cs":   "namespace Shared;\n",
		"B/Bar.cs":   "namespace Shared;\n",
		"C/Use.cs":   "namespace App;\nusing Shared;\n",
	})
	g, err := (&Loader{Spec: csharpSpec(t), Dir: root}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	imp := findImport(findPkg(g, "C"), "Shared")
	if imp == nil || imp.Class != core.ClassInModule {
		t.Fatalf("C->Shared should be in-module, got %+v", imp)
	}
	if imp.RelDir != "A" {
		t.Errorf("Shared spans A and B; edge should point at the first sorted dir A, got %q", imp.RelDir)
	}
}
