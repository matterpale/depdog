package kotlin

import (
	"os"
	"path/filepath"
	"strings"
)

// projectName derives the label used as the graph's ModulePath and the prefix
// of every node's display ImportPath. It reads a project name from
// build.gradle.kts / settings.gradle.kts (rootProject.name = "…") — the usual
// Kotlin build files — then build.gradle / settings.gradle, then pom.xml
// (<artifactId>), falling back to the root directory's basename. The reads are
// tiny hand-rolled scans — enough to find one value without pulling in a
// Kotlin/Groovy/XML parser, keeping the adapter std-lib only.
func projectName(root string) string {
	for _, marker := range []string{"settings.gradle.kts", "build.gradle.kts", "settings.gradle", "build.gradle"} {
		if name := rootProjectNameFromGradle(filepath.Join(root, marker)); name != "" {
			return name
		}
	}
	if name := artifactIDFromPom(filepath.Join(root, "pom.xml")); name != "" {
		return name
	}
	return filepath.Base(root)
}

// rootProjectNameFromGradle scans a Gradle build/settings script for a
// `rootProject.name = "…"` assignment. Returns "" if the file is absent or has
// no such line.
func rootProjectNameFromGradle(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "rootProject.name") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		if name := unquote(strings.TrimSpace(line[eq+1:])); name != "" {
			return name
		}
	}
	return ""
}

// artifactIDFromPom returns the top-level <artifactId> of a Maven pom.xml (some
// Kotlin projects build with Maven). It deliberately picks the first
// <artifactId> that is NOT nested inside a <parent>, <dependency>, <plugin>, or
// similar block so the project's own id is chosen over a parent/dependency
// coordinate. Returns "" if the file is absent or has no usable id.
func artifactIDFromPom(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	depth := 0 // nesting inside blocks that carry their own artifactId
	i := 0
	for i < len(text) {
		lt := strings.IndexByte(text[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		// An XML comment may contain the '>' that IndexByte would otherwise treat
		// as the tag end, so match its `-->` terminator explicitly first.
		if strings.HasPrefix(text[i:], "<!--") {
			end := strings.Index(text[i+len("<!--"):], "-->")
			if end < 0 {
				return "" // unterminated comment: give up, fall back to dir name
			}
			i += len("<!--") + end + len("-->")
			continue
		}
		gt := strings.IndexByte(text[i:], '>')
		if gt < 0 {
			break
		}
		tag := strings.TrimSpace(text[i+1 : i+gt])
		i += gt + 1

		switch {
		case strings.HasPrefix(tag, "/"):
			name := strings.TrimSpace(tag[1:])
			if isNestedArtifactBlock(name) && depth > 0 {
				depth--
			}
			continue
		case strings.HasSuffix(tag, "/"):
			// Self-closing element: no nesting change.
			continue
		}

		name := tagName(tag)
		if isNestedArtifactBlock(name) {
			depth++
			continue
		}
		if name == "artifactId" && depth == 0 {
			end := strings.IndexByte(text[i:], '<')
			if end < 0 {
				return ""
			}
			return strings.TrimSpace(text[i : i+end])
		}
	}
	return ""
}

// isNestedArtifactBlock reports whether an XML element introduces a scope that
// carries its own <artifactId> we must not mistake for the project's own id.
func isNestedArtifactBlock(name string) bool {
	switch name {
	case "parent", "dependency", "plugin", "exclusion", "extension":
		return true
	}
	return false
}

// tagName returns the element name from a start-tag body (drops attributes).
func tagName(tag string) string {
	if i := strings.IndexAny(tag, " \t\r\n"); i >= 0 {
		return tag[:i]
	}
	return tag
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
