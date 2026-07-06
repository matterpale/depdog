package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/matterpale/depdog/internal/core"
)

func graphViews() []core.PackageView {
	return []core.PackageView{
		{ImportPath: "m/a", Component: "a", Imports: []core.ImportView{
			{Path: "fmt", Class: core.ClassStd},
			{Path: "m/b", Class: core.ClassInModule, Component: "b"},
		}},
		{ImportPath: "m/b", Component: "b"},
	}
}

func TestGraphDOTHighlightsViolations(t *testing.T) {
	var buf bytes.Buffer
	err := Graph(&buf, graphViews(), []core.Violation{{FromPackage: "m/a", ImportPath: "m/b"}}, "dot", "component")
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"digraph depdog", `"a" -> "b" [color="red"`, `"b";`} {
		if !strings.Contains(out, want) {
			t.Errorf("dot missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "fmt") {
		t.Errorf("std-lib edges should be omitted:\n%s", out)
	}
}

func TestGraphMermaidPackage(t *testing.T) {
	var buf bytes.Buffer
	if err := Graph(&buf, graphViews(), nil, "mermaid", "package"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "flowchart LR") || !strings.Contains(out, "-->") {
		t.Errorf("mermaid output malformed:\n%s", out)
	}
}

func TestGraphErrors(t *testing.T) {
	if err := Graph(&bytes.Buffer{}, nil, nil, "svg", "component"); err == nil || !strings.Contains(err.Error(), "mermaid") {
		t.Errorf("bad format should error listing formats, got %v", err)
	}
	if err := Graph(&bytes.Buffer{}, nil, nil, "dot", "galaxy"); err == nil || !strings.Contains(err.Error(), "component") {
		t.Errorf("bad level should error listing levels, got %v", err)
	}
}
