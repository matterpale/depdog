package python

import (
	"os"
	"path/filepath"
	"strings"
)

// projectName derives the module label used as the graph's ModulePath and the
// prefix of every node's display ImportPath. It reads the project `name` from
// pyproject.toml ([project] or [tool.poetry]) or setup.cfg ([metadata]),
// falling back to the root directory's basename. The TOML/INI reads are a tiny
// hand-rolled key lookup — enough to find one `name = "..."` line without
// pulling in a parser, keeping the adapter std-lib only.
func projectName(root string) string {
	fallback := filepath.Base(root)

	if name := nameFromPyproject(filepath.Join(root, "pyproject.toml")); name != "" {
		return name
	}
	if name := nameFromSetupCfg(filepath.Join(root, "setup.cfg")); name != "" {
		return name
	}
	return fallback
}

// nameFromPyproject scans pyproject.toml for a `name = "..."` key inside the
// [project] table (PEP 621) or, failing that, [tool.poetry]. Returns "" if the
// file is absent or has no usable name.
func nameFromPyproject(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var section string
	var projectName, poetryName string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripTOMLComment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, val, ok := splitKeyValue(line)
		if !ok || key != "name" {
			continue
		}
		switch section {
		case "project":
			projectName = unquote(val)
		case "tool.poetry":
			poetryName = unquote(val)
		}
	}
	if projectName != "" {
		return projectName
	}
	return poetryName
}

// nameFromSetupCfg scans setup.cfg for `name = ...` inside the [metadata]
// section.
func nameFromSetupCfg(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var section string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "metadata" {
			continue
		}
		key, val, ok := splitKeyValue(line)
		if ok && key == "name" {
			return unquote(strings.TrimSpace(val))
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
