package config

import "testing"

// FuzzParse checks that Parse never panics on arbitrary input and that any
// config it accepts is well-formed (a non-nil rule set with at least one
// component, which the rest of the engine relies on).
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		valid,
		"version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: deny\n",
		"version: 1\ncomponents:\n  a: cmd/**\nrules:\n  a: { deny: [external] }\n",
		"version: 1\ncomponents: {a: [\"x/**\"]}\npolicy: allow\noptions: {test_files: relaxed, skip: [\"y/**\"]}",
		"version: 2\ncomponents: {a: [\"x/**\"]}\npolicy: deny",
		"version: 1\ncomponents: {std: [\"x/**\"]}\npolicy: deny",
		"version: 1\ncomponents: {a: [\"x/[bad/**\"]}\npolicy: deny",
		"not: yaml: at: all",
		"version: 1\ncomponents: {a: [\"x/**\"]}\nrulez: {}",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		rs, err := Parse(data)
		if err != nil {
			return
		}
		if rs == nil {
			t.Fatalf("Parse returned a nil rule set with no error for %q", data)
		}
		if len(rs.Components) == 0 {
			t.Fatalf("Parse accepted a config with no components: %q", data)
		}
	})
}
