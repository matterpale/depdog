package rust

import (
	"os"
	"path/filepath"
	"strings"
)

// crateName derives the label used as the graph's ModulePath and the prefix of
// every node's display ImportPath. It reads the crate `name` from the [package]
// table of Cargo.toml, falling back to the root directory's basename. The TOML
// read is a tiny hand-rolled section+key lookup — enough to find one
// `name = "..."` line without pulling in a parser, keeping the adapter std-lib
// only.
func crateName(root string) string {
	if name := nameFromCargoToml(filepath.Join(root, "Cargo.toml")); name != "" {
		return name
	}
	return filepath.Base(root)
}

// nameFromCargoToml scans Cargo.toml for a `name = "..."` key inside the
// [package] table. Returns "" if the file is absent or has no usable name (e.g.
// a virtual workspace manifest with only [workspace]).
func nameFromCargoToml(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var section string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripTOMLComment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "package" {
			continue
		}
		key, val, ok := splitKeyValue(line)
		if ok && key == "name" {
			return unquote(val)
		}
	}
	return ""
}

// stripTOMLComment removes an unquoted trailing `#` comment from a TOML line.
func stripTOMLComment(line string) string {
	inString := false
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString {
			if c == quote {
				inString = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString = true
			quote = c
		case '#':
			return line[:i]
		}
	}
	return line
}

// splitKeyValue splits `key = value` on the first `=`, trimming both sides.
func splitKeyValue(line string) (key, val string, ok bool) {
	i := strings.IndexByte(line, '=')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// unquote strips a single pair of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
