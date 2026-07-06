package core

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// MatchPattern reports whether the module-relative package dir matches the
// pattern. Patterns are slash-separated; "**" matches any number of
// segments including zero, every other segment follows path.Match syntax.
// "internal/**" therefore matches "internal" itself and everything below.
func MatchPattern(pattern, dir string) (bool, error) {
	return matchSegments(splitPath(pattern), splitPath(dir))
}

// ValidatePattern rejects malformed patterns up front so matching never
// fails mid-evaluation.
func ValidatePattern(pattern string) error {
	if pattern == "" {
		return errors.New("empty pattern")
	}
	if strings.HasPrefix(pattern, "/") || strings.Contains(pattern, `\`) {
		return fmt.Errorf("pattern %q must be relative to the module root and use forward slashes", pattern)
	}
	for _, seg := range splitPath(pattern) {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, "probe"); err != nil {
			return fmt.Errorf("pattern %q, segment %q: %w", pattern, seg, err)
		}
	}
	return nil
}

func splitPath(p string) []string {
	if p == "" || p == "." {
		return nil
	}
	return strings.Split(p, "/")
}

func matchSegments(pat, dir []string) (bool, error) {
	if len(pat) == 0 {
		return len(dir) == 0, nil
	}
	if pat[0] == "**" {
		ok, err := matchSegments(pat[1:], dir)
		if ok || err != nil {
			return ok, err
		}
		if len(dir) > 0 {
			return matchSegments(pat, dir[1:])
		}
		return false, nil
	}
	if len(dir) == 0 {
		return false, nil
	}
	ok, err := path.Match(pat[0], dir[0])
	if err != nil || !ok {
		return false, err
	}
	return matchSegments(pat[1:], dir[1:])
}

// specificity orders overlapping patterns: more literal segments win, then
// more non-** segments. Equal specificity across components is ambiguous.
type specificity struct {
	literals int
	segments int
}

func patternSpecificity(pattern string) specificity {
	var s specificity
	for _, seg := range splitPath(pattern) {
		if seg == "**" {
			continue
		}
		s.segments++
		if !strings.ContainsAny(seg, "*?[") {
			s.literals++
		}
	}
	return s
}

func (a specificity) compare(b specificity) int {
	if a.literals != b.literals {
		if a.literals > b.literals {
			return 1
		}
		return -1
	}
	if a.segments != b.segments {
		if a.segments > b.segments {
			return 1
		}
		return -1
	}
	return 0
}
