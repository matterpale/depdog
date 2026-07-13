package config

import "testing"

// FuzzParse checks that Parse never panics on arbitrary input and that any
// config it accepts is well-formed (a non-nil rule set with at least one
// component, which the rest of the engine relies on).
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		valid,
		"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny\n",
		"version: 2\ncomponents:\n  a: { path: cmd/**, deny: [external] }\ndefault: allow\n",
		"version: 2\ncomponents: {a: {path: \"x/**\"}}\ndefault: allow\noptions: {test_files: relaxed, skip: [\"y/**\"]}",
		"version: 1\ncomponents:\n  a: [\"x/**\"]\nrules:\n  a: { allow: [std] }\n", // legacy: exercises the migration error path
		"version: 2\ncomponents: {std: {path: \"x/**\"}}\ndefault: deny",
		"version: 2\ncomponents: {a: {path: \"x/[bad/**\"}}\ndefault: deny",
		"not: yaml: at: all",
		"version: 2\ncomponents: {a: {path: \"x/**\"}}\nrulez: {}",
		"version: 2\nlang: go\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny",       // lang: valid adapter name
		"version: 2\nlang: klingon\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny",  // lang: unknown adapter (config carries it; CLI rejects)
		"version: 2\nlang: [go, ts]\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny", // lang: non-scalar (must not panic)
		"version: 2\nlang: \"\"\ncomponents: {a: {path: \"x/**\"}}\ndefault: deny",     // lang: empty scalar
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
