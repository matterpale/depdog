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
	err := Graph(&buf, "m", graphViews(), []core.Violation{{FromPackage: "m/a", ImportPath: "m/b"}},
		GraphOptions{Format: "dot", Level: "component"})
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

func TestGraphPackageDOTClustersAndShortens(t *testing.T) {
	var buf bytes.Buffer
	if err := Graph(&buf, "m", graphViews(), nil, GraphOptions{Format: "dot", Level: "package"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`subgraph "cluster_a"`, `label="a";`, // clustered by component
		`"m/a" [label="a"];`, // full-path id, module-relative label
		`"m/a" -> "m/b";`,    // edges reference ids
	} {
		if !strings.Contains(out, want) {
			t.Errorf("package dot missing %q\n%s", want, out)
		}
	}
}

func TestGraphMermaidPackage(t *testing.T) {
	var buf bytes.Buffer
	if err := Graph(&buf, "m", graphViews(), nil, GraphOptions{Format: "mermaid", Level: "package"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "flowchart LR") || !strings.Contains(out, "-->") {
		t.Errorf("mermaid output malformed:\n%s", out)
	}
	if !strings.Contains(out, `["a"]`) {
		t.Errorf("mermaid should use module-relative labels:\n%s", out)
	}
}

func TestGraphViolationsOnly(t *testing.T) {
	// a -> b violates; a -> c is clean. Only the violation edge and its
	// endpoints survive; the clean-only node c is dropped.
	views := []core.PackageView{
		{ImportPath: "m/a", Component: "a", Imports: []core.ImportView{
			{Path: "m/b", Class: core.ClassInModule, Component: "b"},
			{Path: "m/c", Class: core.ClassInModule, Component: "c"},
		}},
		{ImportPath: "m/b", Component: "b"},
		{ImportPath: "m/c", Component: "c"},
	}
	var buf bytes.Buffer
	err := Graph(&buf, "m", views, []core.Violation{{FromPackage: "m/a", ImportPath: "m/b"}},
		GraphOptions{Format: "dot", Level: "component", ViolationsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"a" -> "b"`) {
		t.Errorf("violation edge should be present:\n%s", out)
	}
	if strings.Contains(out, `-> "c"`) || strings.Contains(out, `"c";`) {
		t.Errorf("clean-only node c should be dropped:\n%s", out)
	}
}

func TestGraphErrors(t *testing.T) {
	if err := Graph(&bytes.Buffer{}, "m", nil, nil, GraphOptions{Format: "svg", Level: "component"}); err == nil || !strings.Contains(err.Error(), "mermaid") {
		t.Errorf("bad format should error listing formats, got %v", err)
	}
	if err := Graph(&bytes.Buffer{}, "m", nil, nil, GraphOptions{Format: "dot", Level: "galaxy"}); err == nil || !strings.Contains(err.Error(), "component") {
		t.Errorf("bad level should error listing levels, got %v", err)
	}
}
