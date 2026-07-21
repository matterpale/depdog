package spec

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"embed"
)

// builtinFS holds the declarative adapter specs depdog ships embedded in the
// binary. Adding a built-in language is dropping a <name>.yaml here.
//
//go:embed builtin/*.yaml
var builtinFS embed.FS

// Builtins loads and validates every embedded built-in adapter spec, sorted by
// name. These are the declarative adapters depdog ships (e.g. C#). A user spec of
// the same name overrides one at the registry level, not here.
func Builtins() ([]*Spec, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, err
	}
	var out []*Spec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := builtinFS.ReadFile(path.Join("builtin", e.Name()))
		if err != nil {
			return nil, err
		}
		sp, err := Load(data)
		if err != nil {
			return nil, fmt.Errorf("built-in adapter %s: %w", e.Name(), err)
		}
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
