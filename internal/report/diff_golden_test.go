package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// goldenDiff compares got against the named golden file under testdata,
// rewriting it when -update is set. The three diff renderers are goldened off a
// hand-built ArchDiff so no git or scan is needed — the render shape is pinned
// hermetically, independent of the engine's edge extraction (which diff_test.go
// covers).
func goldenDiff(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("output does not match %s\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// sampleDiff is the constructed ArchDiff all three golden renderers share: two
// added edges (one crossing the `adapters` boundary) and one removed edge, so a
// golden exercises the count line, both lists and a boundary callout.
func sampleDiff() ArchDiff {
	return ArchDiff{
		Added: []ComponentEdge{
			{From: "handler", To: "service"},
			{From: "handler", To: "repository", CrossesBoundary: true, Boundary: "adapters"},
		},
		Removed:           []ComponentEdge{{From: "service", To: "config"}},
		AddedCount:        2,
		RemovedCount:      1,
		BoundaryCrossings: 1,
	}
}

func TestDiffTextGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffText(&buf, sampleDiff(), "origin/main"); err != nil {
		t.Fatal(err)
	}
	goldenDiff(t, "diff_text.golden", buf.String())
}

func TestDiffGitHubGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffGitHub(&buf, sampleDiff(), "origin/main"); err != nil {
		t.Fatal(err)
	}
	goldenDiff(t, "diff_github.golden", buf.String())
}

func TestDiffJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffJSON(&buf, sampleDiff(), "origin/main"); err != nil {
		t.Fatal(err)
	}
	goldenDiff(t, "diff_json.golden", buf.String())
}

func TestDiffGitHubEmptyGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffGitHub(&buf, ArchDiff{}, "origin/main"); err != nil {
		t.Fatal(err)
	}
	goldenDiff(t, "diff_github_empty.golden", buf.String())
}

func TestDiffJSONEmptyGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffJSON(&buf, ArchDiff{}, "origin/main"); err != nil {
		t.Fatal(err)
	}
	goldenDiff(t, "diff_json_empty.golden", buf.String())
}

// TestDiffJSONShape asserts the structured delta's contract independently of the
// golden bytes: snake_case keys, [] (not null) for empty collections, and a
// boundary field that is present only when an edge crosses one — the guarantees
// tooling depends on.
func TestDiffJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffJSON(&buf, sampleDiff(), "origin/main"); err != nil {
		t.Fatal(err)
	}
	raw := buf.String()

	var parsed struct {
		Since   string           `json:"since"`
		Added   []map[string]any `json:"added"`
		Removed []map[string]any `json:"removed"`
		Stats   map[string]any   `json:"stats"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	if parsed.Since != "origin/main" {
		t.Errorf("since = %q, want origin/main", parsed.Since)
	}
	// snake_case stat keys.
	for _, k := range []string{"added", "removed", "boundary_crossings"} {
		if _, ok := parsed.Stats[k]; !ok {
			t.Errorf("stats missing snake_case key %q: %v", k, parsed.Stats)
		}
	}
	// snake_case edge key.
	if len(parsed.Added) != 2 {
		t.Fatalf("added = %d, want 2", len(parsed.Added))
	}
	if _, ok := parsed.Added[0]["crosses_boundary"]; !ok {
		t.Errorf("edge missing snake_case key crosses_boundary: %v", parsed.Added[0])
	}
	// Sorted by from then to: handler → repository (crossing) precedes
	// handler → service (non-crossing). The crossing edge carries boundary; the
	// non-crossing one omits it.
	crossing := parsed.Added[0] // handler → repository
	if crossing["boundary"] != "adapters" {
		t.Errorf("crossing edge boundary = %v, want adapters", crossing["boundary"])
	}
	nonCrossing := parsed.Added[1] // handler → service
	if _, ok := nonCrossing["boundary"]; ok {
		t.Errorf("non-crossing edge should omit boundary: %v", nonCrossing)
	}
}

// TestDiffJSONEmptyIsArrays guards the [] (not null) convention for an empty
// diff — the field most likely to regress to null.
func TestDiffJSONEmptyIsArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := DiffJSON(&buf, ArchDiff{}, "HEAD~1"); err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if strings.Contains(raw, "null") {
		t.Errorf("empty diff must encode collections as [], not null:\n%s", raw)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	for _, k := range []string{"added", "removed"} {
		if string(parsed[k]) != "[]" {
			t.Errorf("%s = %s, want []", k, parsed[k])
		}
	}
}

// TestDiffJSONDeterministic renders an unsorted ArchDiff twice and asserts the
// output is byte-identical and sorted (by from then to), so consumers can diff
// or cache the JSON safely.
func TestDiffJSONDeterministic(t *testing.T) {
	unsorted := ArchDiff{
		Added: []ComponentEdge{
			{From: "handler", To: "service"},
			{From: "app", To: "domain"},
			{From: "handler", To: "repository"},
		},
		AddedCount: 3,
	}
	var a, b bytes.Buffer
	if err := DiffJSON(&a, unsorted, "main"); err != nil {
		t.Fatal(err)
	}
	if err := DiffJSON(&b, unsorted, "main"); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("DiffJSON not deterministic:\n%s\n---\n%s", a.String(), b.String())
	}
	var parsed struct {
		Added []struct {
			From, To string
		} `json:"added"`
	}
	if err := json.Unmarshal(a.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// app → domain sorts before both handler edges; handler → repository before
	// handler → service.
	want := []string{"app→domain", "handler→repository", "handler→service"}
	for i, e := range parsed.Added {
		if got := e.From + "→" + e.To; got != want[i] {
			t.Errorf("added[%d] = %s, want %s (sorted)", i, got, want[i])
		}
	}
}

// TestDiffGitHubBacktickEscaping guards that a backtick in a component name
// cannot break out of an inline code span in the markdown comment.
func TestDiffGitHubBacktickEscaping(t *testing.T) {
	var buf bytes.Buffer
	d := ArchDiff{
		Added:      []ComponentEdge{{From: "we`ird", To: "b"}},
		AddedCount: 1,
	}
	if err := DiffGitHub(&buf, d, "ma`in"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "we`ird") || strings.Contains(buf.String(), "ma`in") {
		t.Errorf("backticks in values must be neutralised inside code spans:\n%s", buf.String())
	}
}
