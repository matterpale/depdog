package config

import (
	"reflect"
	"strings"
	"testing"
)

// mergeInput is a hand-formatted v2 config exercising everything a merge must
// preserve: head comments, aligned values, trailing comments, a comment block
// inside the components mapping, blank-line section breaks, and sections after
// components (policy, options).
const mergeInput = `# my architecture — hands off, depdog
version: 2

components:
  main:    { path: "cmd/**", allow: ["*"] } # entrypoints
  domain:  { path: "internal/domain/**", allow: [std] }

  # data access
  storage: { path: "internal/repository/**", allow: [domain, std, external] }

default: deny

options:
  test_files: hybrid
`

func TestMergeComponentsPreservesFormatting(t *testing.T) {
	add := []MergeComponent{
		// Deliberately unsorted: MergeComponents must sort by name.
		{Name: "util", Patterns: []string{"pkg/util/**"}, Comment: "proposed from directory scan — review this rule", Rule: "allow: [std, external]"},
		{Name: "handler", Patterns: []string{"internal/handler/**"}, Comment: "proposed from directory scan — review this rule", Rule: "allow: [std, external]"},
	}
	got, err := MergeComponents([]byte(mergeInput), add)
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := `# my architecture — hands off, depdog
version: 2

components:
  main:    { path: "cmd/**", allow: ["*"] } # entrypoints
  domain:  { path: "internal/domain/**", allow: [std] }

  # data access
  storage: { path: "internal/repository/**", allow: [domain, std, external] }
  handler: { path: "internal/handler/**", allow: [std, external] } # proposed from directory scan — review this rule
  util:    { path: "pkg/util/**", allow: [std, external] } # proposed from directory scan — review this rule

default: deny

options:
  test_files: hybrid
`
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsWithRule(t *testing.T) {
	in := "version: 2\n\ncomponents:\n  app: { path: \"internal/app/**\", allow: [std] }\n\ndefault: deny\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}, Rule: "allow: [std, external]"},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 2\n\ncomponents:\n  app: { path: \"internal/app/**\", allow: [std] }\n  web: { path: \"web/**\", allow: [std, external] }\n\ndefault: deny\n"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsNoRule(t *testing.T) {
	// A component with no rule renders just its path.
	in := "version: 2\ncomponents:\n  app: { path: \"internal/app/**\" }\ndefault: allow\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 2\ncomponents:\n  app: { path: \"internal/app/**\" }\n  web: { path: \"web/**\" }\ndefault: allow\n"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsBlockEntryPatterns(t *testing.T) {
	// A block-mapping entry whose path is a multi-line sequence: the insertion
	// must land after the whole entry, not inside it.
	in := "version: 2\ncomponents:\n  app:\n    path:\n      - \"internal/app/**\"\n      - \"internal/shared/**\"\ndefault: allow\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 2\ncomponents:\n  app:\n    path:\n      - \"internal/app/**\"\n      - \"internal/shared/**\"\n  web: { path: \"web/**\" }\ndefault: allow\n"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsNoTrailingNewline(t *testing.T) {
	in := "version: 2\ncomponents:\n  app: { path: \"internal/app/**\" }"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 2\ncomponents:\n  app: { path: \"internal/app/**\" }\n  web: { path: \"web/**\" }"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsNothingToAdd(t *testing.T) {
	got, err := MergeComponents([]byte(mergeInput), nil)
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	if string(got) != mergeInput {
		t.Errorf("merging nothing must not change the file:\n%s", got)
	}
}

func TestMergeComponentsRefusals(t *testing.T) {
	web := []MergeComponent{{Name: "web", Patterns: []string{"web/**"}}}
	cases := []struct {
		name string
		in   string
		add  []MergeComponent
		want string // substring of the error
	}{
		{
			name: "flow components",
			in:   "version: 2\ncomponents: { app: { path: \"internal/**\" } }\n",
			add:  web,
			want: "flow style",
		},
		{
			name: "anchors",
			in:   "version: 2\ncomponents:\n  app: &a { path: \"internal/**\" }\n  app2: *a\n",
			add:  web,
			want: "anchors",
		},
		{
			name: "duplicate name",
			in:   "version: 2\ncomponents:\n  web: { path: \"web/**\" }\n",
			add:  web,
			want: "already",
		},
		{
			name: "no components mapping",
			in:   "version: 2\ndefault: deny\n",
			add:  web,
			want: "components",
		},
		{
			name: "not a mapping",
			in:   "- just\n- a\n- list\n",
			add:  web,
			want: "mapping",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeComponents([]byte(tc.in), tc.add)
			if err == nil {
				t.Fatal("want an error, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestMergeComponentsQuotesOddNames(t *testing.T) {
	in := "version: 2\ncomponents:\n  app: { path: \"internal/app/**\" }\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "we b", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	if !strings.Contains(string(got), `"we b": { path: "web/**" }`) {
		t.Errorf("a name unsafe as a bare key must be quoted:\n%s", got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestDeclaredNames(t *testing.T) {
	in := "version: 2\ncomponents:\n  b: { path: \"b/**\" }\n  a: { path: \"a/**\" }\ngroups:\n  edges: [a, b]\n"
	got, err := DeclaredNames([]byte(in))
	if err != nil {
		t.Fatalf("DeclaredNames: %v", err)
	}
	if want := []string{"a", "b", "edges"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DeclaredNames = %v, want %v", got, want)
	}
}
