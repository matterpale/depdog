package python

// scan is a comment/string-aware pass over Python source that extracts the
// module references from the two import surfaces:
//
//	import a.b [as c][, d.e ...]
//	from [.][.]pkg import x [, y ...]  (also `from . import x`)
//
// It is deliberately NOT a naive line regex — an import-looking substring
// inside a comment or a string literal (including triple-quoted strings) is
// never mistaken for an import, which is the single most important correctness
// property. The scanner tracks Python's lexical states (line comment,
// single/double quoted strings with escapes, and triple-quoted strings) so a
// `from x import y` sitting inside a docstring produces no edge.

// importRef is one captured module reference and where it was found. Level is
// the number of leading dots for a relative import (0 for absolute). Module is
// the dotted name after the dots (may be empty for `from . import x`).
type importRef struct {
	Module string // dotted module path, e.g. "a.b" (without leading dots)
	Level  int    // count of leading dots: 0 absolute, 1 `.`, 2 `..`, ...
	Line   int    // 1-based line of the statement's keyword
}

// scan returns every import reference in src, in source order.
func scan(src []byte) []importRef {
	s := &scanner{src: src, line: 1}
	s.run()
	return s.out
}

type scanner struct {
	src  []byte
	pos  int
	line int
	// atLineStart is true when only whitespace has been seen since the last
	// newline. Import statements are only recognised at the start of a logical
	// line (after optional indentation), which is where Python allows them.
	atLineStart bool
	out         []importRef
}

func (s *scanner) run() {
	s.atLineStart = true
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
			s.atLineStart = true
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '#':
			s.skipLineComment()
		case c == '"' || c == '\'':
			s.skipString()
			s.atLineStart = false
		case isWordStart(c):
			if s.atLineStart && s.tryImport() {
				continue
			}
			s.skipWord()
			s.atLineStart = false
		default:
			s.pos++
			s.atLineStart = false
		}
	}
}

// tryImport is called with s.pos at a word start that begins a logical line. It
// handles both `import ...` and `from ... import ...`. Returns true (having
// consumed through the statement) when it matched an import surface; false when
// the word was some other identifier (leaving s.pos unchanged so run() can skip
// it normally).
func (s *scanner) tryImport() bool {
	switch word := s.peekWord(); word {
	case "import":
		s.pos += len(word)
		s.handleImport(s.line)
		return true
	case "from":
		s.pos += len(word)
		s.handleFrom(s.line)
		return true
	}
	return false
}

// handleImport parses the comma-separated dotted names of a plain
// `import a.b [as c], d.e` statement. Only the module path is captured; the
// `as` alias and trailing names past a line continuation are handled.
func (s *scanner) handleImport(line int) {
	for {
		s.skipInlineSpace()
		mod := s.readDotted()
		if mod != "" {
			s.out = append(s.out, importRef{Module: mod, Level: 0, Line: line})
		}
		// Skip an optional `as alias`.
		s.skipInlineSpace()
		if s.peekWord() == "as" {
			s.pos += len("as")
			s.skipInlineSpace()
			s.skipWord()
			s.skipInlineSpace()
		}
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
			continue
		}
		return
	}
}

// handleFrom parses `from [.]*[pkg] import ...`. It captures the leading dot
// level and the (possibly empty) dotted package, then consumes to the end of
// the import clause so its names are not rescanned as new statements.
func (s *scanner) handleFrom(line int) {
	s.skipInlineSpace()
	level := 0
	for s.pos < len(s.src) && s.src[s.pos] == '.' {
		level++
		s.pos++
	}
	mod := s.readDotted()
	s.out = append(s.out, importRef{Module: mod, Level: level, Line: line})
	// Consume the rest of the `import (...)` clause so the imported symbol
	// names are not misread. Handle a parenthesised, multi-line list and a
	// backslash line continuation.
	s.skipImportClause()
}

// skipImportClause advances past everything up to the logical end of a `from`
// import: `import a, b`, `import (a,\n b)`, or a `\`-continued list.
func (s *scanner) skipImportClause() {
	depth := 0
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '(':
			depth++
			s.pos++
		case c == ')':
			if depth > 0 {
				depth--
			}
			s.pos++
		case c == '#':
			s.skipLineComment()
			if depth == 0 {
				return
			}
		case c == '\\' && s.peek(1) == '\n':
			s.line++
			s.pos += 2
		case c == '\n':
			s.line++
			s.pos++
			if depth == 0 {
				return // logical line ends at newline outside parentheses
			}
		default:
			s.pos++
		}
	}
}

// readDotted reads a dotted identifier chain (a, a.b, a.b.c) starting at pos.
// Returns "" if there is no identifier. Interior whitespace is not allowed
// between the dots and names (Python forbids it), so this stops at the first
// non-identifier, non-dot byte.
func (s *scanner) readDotted() string {
	startPos := s.pos
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if isWordPart(c) || c == '.' {
			s.pos++
			continue
		}
		break
	}
	return string(s.src[startPos:s.pos])
}

func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// skipString consumes a string literal starting at the opening quote. It
// handles triple-quoted strings, escape sequences, and raw single-line strings.
func (s *scanner) skipString() {
	quote := s.src[s.pos]
	// Triple-quoted?
	if s.peek(1) == quote && s.peek(2) == quote {
		s.pos += 3
		for s.pos < len(s.src) {
			c := s.src[s.pos]
			switch {
			case c == '\\':
				// Count the escaped byte's newline if it is one, then skip both.
				if s.peek(1) == '\n' {
					s.line++
				}
				s.pos += 2
				if s.pos > len(s.src) {
					s.pos = len(s.src)
				}
			case c == '\n':
				s.line++
				s.pos++
			case c == quote && s.peek(1) == quote && s.peek(2) == quote:
				s.pos += 3
				return
			default:
				s.pos++
			}
		}
		return
	}
	// Single-line string.
	s.pos++
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			s.pos += 2
		case '\n':
			// Unterminated string literal at EOL; bail so line tracking stays sane.
			s.line++
			s.pos++
			return
		case quote:
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

// skipWord advances past a run of identifier bytes.
func (s *scanner) skipWord() {
	for s.pos < len(s.src) && isWordPart(s.src[s.pos]) {
		s.pos++
	}
}

// peekWord returns the identifier at pos without advancing.
func (s *scanner) peekWord() string {
	p := s.pos
	for p < len(s.src) && isWordPart(s.src[p]) {
		p++
	}
	return string(s.src[s.pos:p])
}

// skipInlineSpace skips spaces/tabs and backslash line-continuations but not a
// bare newline.
func (s *scanner) skipInlineSpace() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '\\' && s.peek(1) == '\n':
			s.line++
			s.pos += 2
		default:
			return
		}
	}
}

func (s *scanner) peek(n int) byte {
	if s.pos+n < len(s.src) {
		return s.src[s.pos+n]
	}
	return 0
}

func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
