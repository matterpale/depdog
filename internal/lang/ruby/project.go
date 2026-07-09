package ruby

import (
	"os"
	"path/filepath"
	"strings"
)

// projectName derives the module label used as the graph's ModulePath and the
// prefix of every node's display ImportPath. It reads the gem name from a
// `*.gemspec` (`spec.name = "..."`, any receiver), falling back to the root
// directory's basename. The gemspec read is a tiny hand-rolled key lookup —
// enough to find one `name = "..."` assignment without evaluating Ruby, keeping
// the adapter std-lib only.
func projectName(root string) string {
	fallback := filepath.Base(root)

	if name := nameFromGemspec(root); name != "" {
		return name
	}
	return fallback
}

// nameFromGemspec finds the first *.gemspec in root and scans it for a
// `<recv>.name = "..."` assignment. Returns "" if none is found.
func nameFromGemspec(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var specs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".gemspec") {
			specs = append(specs, e.Name())
		}
	}
	// Deterministic: gemspecs are rare (usually exactly one), but sort so a
	// project with two produces a stable name.
	if len(specs) > 1 {
		sortStrings(specs)
	}
	for _, spec := range specs {
		if name := nameFromGemspecFile(filepath.Join(root, spec)); name != "" {
			return name
		}
	}
	return ""
}

// nameFromGemspecFile scans one gemspec for a `<recv>.name = "..."` line,
// ignoring comment lines.
func nameFromGemspecFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := splitAssign(line)
		if !ok {
			continue
		}
		// key looks like "spec.name" / "s.name" / "gem.name": accept any
		// receiver whose attribute is exactly `name`.
		if attr := lastSegment(key); attr == "name" {
			if v := unquote(val); v != "" {
				return v
			}
		}
	}
	return ""
}

// splitAssign splits `lhs = rhs` on the first top-level `=`, trimming both
// sides. It ignores `==` and `=>` so only real assignments match.
func splitAssign(line string) (lhs, rhs string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] != '=' {
			continue
		}
		// Skip `==`, `=>`, `<=`, `>=`, `!=`.
		if i+1 < len(line) && (line[i+1] == '=' || line[i+1] == '>') {
			i++
			continue
		}
		if i > 0 && (line[i-1] == '=' || line[i-1] == '<' || line[i-1] == '>' || line[i-1] == '!') {
			continue
		}
		return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
	}
	return "", "", false
}

// lastSegment returns the text after the final dot of a dotted key.
func lastSegment(key string) string {
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		return key[i+1:]
	}
	return key
}

// unquote strips a single pair of matching surrounding quotes and any trailing
// content (e.g. a `.freeze` chain or a comment) after the closing quote.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return ""
	}
	q := s[0]
	if q != '"' && q != '\'' {
		return ""
	}
	if end := strings.IndexByte(s[1:], q); end >= 0 {
		return s[1 : 1+end]
	}
	return ""
}

// sortStrings is a tiny insertion sort to avoid importing sort for one rare
// call in a file that otherwise needs only os/filepath/strings.
func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
