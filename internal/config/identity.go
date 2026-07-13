package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

// UnitIdentities reads the import-path identities the unit rooted at dir
// exposes — the names other units' source would use to import it: the go.mod
// module path, the package.json "name", the Cargo.toml package name, and the
// pyproject.toml project name. Missing or unparseable marker files contribute
// nothing (identity detection degrades, it never fails a check). The result is
// sorted and de-duplicated.
func UnitIdentities(dir string) []string {
	var ids []string
	if id := goModIdentity(filepath.Join(dir, "go.mod")); id != "" {
		ids = append(ids, id)
	}
	if id := packageJSONIdentity(filepath.Join(dir, "package.json")); id != "" {
		ids = append(ids, id)
	}
	if id := tomlNameIdentity(filepath.Join(dir, "Cargo.toml"), "package"); id != "" {
		ids = append(ids, id)
	}
	if id := tomlNameIdentity(filepath.Join(dir, "pyproject.toml"), "project"); id != "" {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return dedupe(ids)
}

func goModIdentity(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return modfile.ModulePath(data)
}

func packageJSONIdentity(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pkg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	return pkg.Name
}

// tomlNameIdentity extracts `name = "..."` from the given [section] of a TOML
// file. A hand-rolled line scan is deliberate: it covers the conventional
// Cargo.toml/pyproject.toml layouts without pulling a TOML dependency into the
// binary, and an exotic file it cannot read simply contributes no identity.
func tomlNameIdentity(path, section string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inSection = line == "["+section+"]"
			continue
		}
		if !inSection || !strings.HasPrefix(line, "name") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "name"))
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(rest, "="))
		if i := strings.Index(val, "#"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		val = strings.Trim(val, `"'`)
		return val
	}
	return ""
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}
