package scala

import (
	"os"
	"path/filepath"
	"strings"
)

// projectName derives the label used as the graph's ModulePath and the prefix of
// every node's display ImportPath. It reads a project name from build.sbt
// (`name := "…"`) — the usual sbt setting — falling back to the root directory's
// basename (which also covers Mill `build.sc`, whose module names are code, not a
// simple assignable string). The reads are tiny hand-rolled scans — enough to
// find one value without pulling in an sbt/Scala parser, keeping the adapter
// std-lib only.
func projectName(root string) string {
	if name := nameFromSbt(filepath.Join(root, "build.sbt")); name != "" {
		return name
	}
	return filepath.Base(root)
}

// nameFromSbt scans a build.sbt for a top-level `name := "…"` setting. Returns ""
// if the file is absent or has no such line. Lines inside `//` comments are
// skipped; a `name := ` inside a block comment is rare and not worth a full
// parser.
func nameFromSbt(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "//") {
			continue
		}
		// Match `name := "…"` (sbt's most common project-name setting). Guard on the
		// `:=` operator so `moduleName`/`organizationName` don't false-match.
		rest, ok := strings.CutPrefix(line, "name")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest, ok = strings.CutPrefix(rest, ":=")
		if !ok {
			continue
		}
		if name := firstStringLiteral(strings.TrimSpace(rest)); name != "" {
			return name
		}
	}
	return ""
}

// firstStringLiteral returns the contents of the first double-quoted string in s,
// or "" if there is none.
func firstStringLiteral(s string) string {
	open := strings.IndexByte(s, '"')
	if open < 0 {
		return ""
	}
	rest := s[open+1:]
	close := strings.IndexByte(rest, '"')
	if close < 0 {
		return ""
	}
	return rest[:close]
}
