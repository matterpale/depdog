package elm

// scan is a comment/string-aware pass over Elm source that extracts the file's
// module declaration and its import statements:
//
//	module Foo.Bar exposing (..)        (plain module)
//	port module Foo.Bar exposing (..)   (a module with ports)
//	effect module Foo.Bar where { .. }  (a low-level effect module)
//	import Foo.Bar
//	import Foo.Bar exposing (..)
//	import Foo.Bar exposing (a, b)
//	import Foo.Bar as FB
//	import Foo.Bar as FB exposing (x, y)
//
// For every import only the fully-qualified module name (Foo.Bar) is captured;
// the `as` alias and the `exposing (...)` tail are irrelevant to resolution and
// are skipped.
//
// Like the Scala/Kotlin scanners, this is deliberately NOT a naive line regex —
// an import-looking substring inside a comment or a string literal is never
// mistaken for an import, which is the single most important correctness
// property. The scanner tracks Elm's lexical states:
//
//   - `--` line comments
//   - `{- -}` block comments, which NEST in Elm (an inner `{-` must be matched by
//     an inner `-}` before the outer comment closes)
//   - `"..."` strings with escapes
//   - `"""..."""` triple-quoted multiline strings
//   - `'x'` char literals with escapes
//
// so an `import X` sitting inside any of these produces no edge.

// scanResult holds one file's module declaration and its imports, in source
// order.
type scanResult struct {
	module  string      // dotted module name from the `module`/`port module`/`effect module` header, "" if absent
	imports []importRef // every import, in source order
}

// importRef is one captured import and where it was found.
type importRef struct {
	Module string // fully-qualified dotted module name, e.g. "Foo.Bar"
	Line   int    // 1-based line of the `import` keyword
}

// scan returns a file's module declaration and imports.
func scan(src []byte) scanResult {
	s := &scanner{src: src, line: 1}
	s.run()
	return scanResult{module: s.module, imports: s.out}
}

type scanner struct {
	src    []byte
	pos    int
	line   int
	module string
	out    []importRef
	// moduleDone is set once the module header has been captured, so a stray
	// later `module` keyword cannot overwrite it.
	moduleDone bool
	// atLineStart is true when only whitespace/comments have been seen since the
	// last newline. Elm's `module`/`import` keywords are only recognised at the
	// start of a logical line (column 1 in practice), so an `import`/`module`
	// used as an identifier mid-line is never a statement.
	atLineStart bool
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
		case c == '-' && s.peek(1) == '-':
			s.skipLineComment()
		case c == '{' && s.peek(1) == '-':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipTripleString()
			s.atLineStart = false
		case c == '"':
			s.skipString()
			s.atLineStart = false
		case c == '\'':
			s.skipChar()
			s.atLineStart = false
		case isWordStart(c):
			if s.atLineStart && s.tryKeyword() {
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

// tryKeyword is called at a word start in line-start position. It handles the
// `module`, `port module`, `effect module`, and `import` surfaces. Returns true
// (having consumed the statement's keyword prefix and recorded the reference)
// when it matched; false otherwise (leaving s.pos unchanged so run() skips the
// word normally).
func (s *scanner) tryKeyword() bool {
	switch s.peekWord() {
	case "module":
		s.pos += len("module")
		s.handleModule()
		return true
	case "port", "effect":
		// `port module Foo` / `effect module Foo` — only when the very next word is
		// `module` (a bare `port` is a port declaration, `effect` an identifier).
		save := s.pos
		s.skipWord()
		s.skipInlineSpace()
		if s.peekWord() == "module" {
			s.pos += len("module")
			s.handleModule()
			return true
		}
		s.pos = save
		return false
	case "import":
		s.pos += len("import")
		s.handleImport(s.line)
		return true
	}
	return false
}

// handleModule records the file's `module Foo.Bar ...` header. Only the first
// module clause is captured. The rest of the header (`exposing (...)` /
// `where {...}`) is irrelevant, so consume to end of the logical line.
func (s *scanner) handleModule() {
	if s.moduleDone {
		s.skipToLineEnd()
		return
	}
	s.skipInlineSpace()
	if name := s.readDotted(); name != "" {
		s.module = name
		s.moduleDone = true
	}
	s.skipToLineEnd()
}

// handleImport records one `import Foo.Bar [as X] [exposing (...)]` statement.
// Only the module name is captured; the `as`/`exposing` tail is skipped.
func (s *scanner) handleImport(line int) {
	s.skipInlineSpace()
	name := s.readDotted()
	s.skipToLineEnd()
	if name != "" {
		s.out = append(s.out, importRef{Module: name, Line: line})
	}
}

// readDotted reads a dotted module name (Foo, Foo.Bar, Foo.Bar.Baz) starting at
// pos. Elm forbids whitespace inside a qualified name, so this stops at the
// first non-identifier, non-dot byte. Returns "" if there is no identifier.
func (s *scanner) readDotted() string {
	start := s.pos
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if isWordPart(c) || c == '.' {
			s.pos++
			continue
		}
		break
	}
	return string(s.src[start:s.pos])
}

func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// skipBlockComment consumes a `{- ... -}` comment. Elm block comments NEST, so
// track depth: an inner `{-` must be closed by an inner `-}` before the outer
// comment ends.
func (s *scanner) skipBlockComment() {
	s.pos += 2 // opening {-
	depth := 1
	for s.pos < len(s.src) && depth > 0 {
		switch {
		case s.src[s.pos] == '{' && s.peek(1) == '-':
			depth++
			s.pos += 2
		case s.src[s.pos] == '-' && s.peek(1) == '}':
			depth--
			s.pos += 2
		case s.src[s.pos] == '\n':
			s.line++
			s.pos++
		default:
			s.pos++
		}
	}
}

// skipString consumes a `"..."` string literal, honoring escapes.
func (s *scanner) skipString() {
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			s.pos += 2
		case '\n':
			// Unterminated string at EOL; bail so line tracking stays sane.
			s.line++
			s.pos++
			return
		case '"':
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

// skipTripleString consumes a `"""..."""` triple-quoted multiline string. Elm
// triple strings honor escapes and may span lines; they end at the next `"""`.
func (s *scanner) skipTripleString() {
	s.pos += 3 // opening """
	for s.pos < len(s.src) {
		switch {
		case s.src[s.pos] == '\\':
			// An escaped byte (possibly an escaped `"`) can't close the string.
			if s.peek(1) == '\n' {
				s.line++
			}
			s.pos += 2
		case s.src[s.pos] == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.pos += 3
			return
		case s.src[s.pos] == '\n':
			s.line++
			s.pos++
		default:
			s.pos++
		}
	}
}

// skipChar consumes a `'x'` char literal, honoring an escape (`'\n'`, `'\”`).
func (s *scanner) skipChar() {
	s.pos++ // opening tick
	if s.pos < len(s.src) && s.src[s.pos] == '\\' {
		s.pos += 2 // the escaped byte
	} else if s.pos < len(s.src) {
		s.pos++ // the single glyph
	}
	if s.pos < len(s.src) && s.src[s.pos] == '\'' {
		s.pos++ // closing tick
	}
}

// skipToLineEnd advances past the rest of a `module`/`import` statement up to
// (but not consuming) the terminating newline, honoring comments and
// string/char literals so a `\n` inside one does not end line tracking early.
func (s *scanner) skipToLineEnd() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			return
		case c == '-' && s.peek(1) == '-':
			s.skipLineComment()
		case c == '{' && s.peek(1) == '-':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipTripleString()
		case c == '"':
			s.skipString()
		case c == '\'':
			s.skipChar()
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

// skipInlineSpace skips spaces/tabs (and `{- -}` block comments, which may sit
// between the `module`/`import` keyword and the name) but NOT a bare newline —
// an Elm import ends at the newline, so a qualified name never continues across
// one.
func (s *scanner) skipInlineSpace() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '{' && s.peek(1) == '-':
			s.skipBlockComment()
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

// isWordStart reports whether c can begin an Elm identifier. Bytes >= 0x80 cover
// Unicode identifier parts.
func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
