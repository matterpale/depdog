package config

import (
	"reflect"
	"strings"
	"testing"
)

// mergeInput is a hand-formatted config exercising everything a merge must
// preserve: head comments, aligned values, trailing comments, a comment block
// inside the components mapping, blank-line section breaks, and sections after
// components (policy, rules, options).
const mergeInput = `# my architecture — hands off, depdog
version: 1

components:
  main:    ["cmd/**"] # entrypoints
  domain:  ["internal/domain/**"]

  # data access
  storage: ["internal/repository/**"]

policy: deny

# who may import whom
rules:
  main:    { allow: ["*"] }
  domain:  { allow: [std] } # keep the core pure
  storage: { allow: [domain, std, external] }

options:
  test_files: hybrid
`

func TestMergeComponentsPreservesFormatting(t *testing.T) {
	add := []MergeComponent{
		// Deliberately unsorted: MergeComponents must sort by name.
		{Name: "util", Patterns: []string{"pkg/util/**"}, Comment: "proposed from directory scan — review this rule", Rule: "{ allow: [std, external] }"},
		{Name: "handler", Patterns: []string{"internal/handler/**"}, Comment: "proposed from directory scan — review this rule", Rule: "{ allow: [std, external] }"},
	}
	got, err := MergeComponents([]byte(mergeInput), add)
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := `# my architecture — hands off, depdog
version: 1

components:
  main:    ["cmd/**"] # entrypoints
  domain:  ["internal/domain/**"]

  # data access
  storage: ["internal/repository/**"]
  handler: ["internal/handler/**"] # proposed from directory scan — review this rule
  util:    ["pkg/util/**"] # proposed from directory scan — review this rule

policy: deny

# who may import whom
rules:
  main:    { allow: ["*"] }
  domain:  { allow: [std] } # keep the core pure
  storage: { allow: [domain, std, external] }
  handler: { allow: [std, external] }
  util:    { allow: [std, external] }

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

func TestMergeComponentsNoRulesSection(t *testing.T) {
	in := "version: 1\n\ncomponents:\n  app: [\"internal/app/**\"]\n\npolicy: deny\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}, Rule: "{ allow: [std, external] }"},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 1\n\ncomponents:\n  app: [\"internal/app/**\"]\n  web: [\"web/**\"]\n\npolicy: deny\n\nrules:\n  web: { allow: [std, external] }\n"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsEmptyRulesSection(t *testing.T) {
	// A bare "rules:" key (null value) gains its entries right below the key.
	in := "version: 1\ncomponents:\n  app: [\"internal/app/**\"]\nrules:\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}, Rule: "{ allow: [std] }"},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 1\ncomponents:\n  app: [\"internal/app/**\"]\n  web: [\"web/**\"]\nrules:\n  web: { allow: [std] }\n"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsBlockSequencePatterns(t *testing.T) {
	// Multi-line block-sequence patterns: the insertion must land after the
	// whole entry, not inside it.
	in := "version: 1\ncomponents:\n  app:\n    - \"internal/app/**\"\n    - \"internal/shared/**\"\npolicy: allow\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 1\ncomponents:\n  app:\n    - \"internal/app/**\"\n    - \"internal/shared/**\"\n  web: [\"web/**\"]\npolicy: allow\n"
	if string(got) != want {
		t.Errorf("merged output mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestMergeComponentsNoTrailingNewline(t *testing.T) {
	in := "version: 1\ncomponents:\n  app: [\"internal/app/**\"]"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "web", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	want := "version: 1\ncomponents:\n  app: [\"internal/app/**\"]\n  web: [\"web/**\"]"
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
			in:   "version: 1\ncomponents: { app: [\"internal/**\"] }\n",
			add:  web,
			want: "flow style",
		},
		{
			name: "flow rules",
			in:   "version: 1\ncomponents:\n  app: [\"internal/**\"]\nrules: {}\n",
			add:  []MergeComponent{{Name: "web", Patterns: []string{"web/**"}, Rule: "{ allow: [std] }"}},
			want: "flow style",
		},
		{
			name: "anchors",
			in:   "version: 1\ncomponents:\n  app: &a [\"internal/**\"]\n  app2: *a\n",
			add:  web,
			want: "anchors",
		},
		{
			name: "duplicate name",
			in:   "version: 1\ncomponents:\n  web: [\"web/**\"]\n",
			add:  web,
			want: "already",
		},
		{
			name: "no components mapping",
			in:   "version: 1\npolicy: deny\n",
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
	in := "version: 1\ncomponents:\n  app: [\"internal/app/**\"]\n"
	got, err := MergeComponents([]byte(in), []MergeComponent{
		{Name: "we b", Patterns: []string{"web/**"}},
	})
	if err != nil {
		t.Fatalf("MergeComponents: %v", err)
	}
	if !strings.Contains(string(got), `"we b": ["web/**"]`) {
		t.Errorf("a name unsafe as a bare key must be quoted:\n%s", got)
	}
	if _, err := Parse(got); err != nil {
		t.Fatalf("merged config does not parse: %v\n%s", err, got)
	}
}

func TestDeclaredNames(t *testing.T) {
	in := "version: 1\ncomponents:\n  b: [\"b/**\"]\n  a: [\"a/**\"]\ngroups:\n  edges: [a, b]\n"
	got, err := DeclaredNames([]byte(in))
	if err != nil {
		t.Fatalf("DeclaredNames: %v", err)
	}
	if want := []string{"a", "b", "edges"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DeclaredNames = %v, want %v", got, want)
	}
}
