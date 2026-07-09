package kotlin

import "strings"

// scan is a comment/string-aware pass over Kotlin source that extracts the
// file's package declaration and its import statements:
//
//	package a.b.c
//	import a.b.C
//	import a.b.*                  (on-demand: the imported package is a.b)
//	import a.b.C as D             (aliased: the `as D` is not part of the import)
//
// Unlike Java, Kotlin `package`/`import` statements are terminated by the end of
// the line, not a `;` (a trailing `;` is tolerated but optional). Like the Java
// scanner, this is deliberately NOT a naive line regex — an import-looking
// substring inside a comment or a string literal (including Kotlin's `"""…"""`
// raw strings) is never mistaken for an import, which is the single most
// important correctness property. The scanner tracks Kotlin's lexical states
// (// line comments, /* */ nestable block comments, "…" strings with escapes,
// '…' char literals, and """…""" raw strings) so an `import x.Y` sitting inside
// a string produces no edge.

// scanResult holds one file's package declaration and its imports, in source
// order.
type scanResult struct {
	pkg     string      // dotted package from `package a.b.c`, "" for the default package
	imports []importRef // every import statement, in source order
}

// importRef is one captured import statement and where it was found. Pkg is the
// package the imported symbol lives in (the import path with its trailing class
// segment removed for a class import; the full path for a wildcard).
type importRef struct {
	Pkg     string // dotted package of the imported symbol, e.g. "a.b"
	Display string // specifier shown in reports, e.g. "a.b.C" or "a.b.*"
	Line    int    // 1-based line of the `import` keyword
}

// scan returns a file's package declaration and imports.
func scan(src []byte) scanResult {
	s := &scanner{src: src, line: 1}
	s.run()
	return scanResult{pkg: s.pkg, imports: s.out}
}

type scanner struct {
	src  []byte
	pos  int
	line int
	pkg  string
	out  []importRef
	// atStmtStart is true when only whitespace/comments have been seen since the
	// last statement boundary (a newline, `;`, `{`, `}`) or the file start.
	// `package` and `import` keywords are only recognised there — an `import`
	// used as an identifier mid-expression is never an import statement.
	atStmtStart bool
}

func (s *scanner) run() {
	s.atStmtStart = true
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
			// A newline is a statement boundary in Kotlin: `package`/`import`
			// statements end at end of line, so a keyword at the start of the
			// next line is a fresh statement.
			s.atStmtStart = true
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipRawString()
			s.atStmtStart = false
		case c == '"':
			s.skipString()
			s.atStmtStart = false
		case c == '\'':
			s.skipChar()
			s.atStmtStart = false
		case c == ';' || c == '{' || c == '}':
			s.pos++
			s.atStmtStart = true
		case isWordStart(c):
			if s.atStmtStart && s.tryKeyword() {
				continue
			}
			s.skipWord()
			s.atStmtStart = false
		default:
			s.pos++
			s.atStmtStart = false
		}
	}
}

// tryKeyword is called at a word start in statement position. It handles the
// `package` and `import` surfaces. Returns true (having consumed the statement)
// when it matched; false otherwise (leaving s.pos unchanged so run() skips the
// word normally).
func (s *scanner) tryKeyword() bool {
	switch s.peekWord() {
	case "package":
		s.pos += len("package")
		s.handlePackage()
		return true
	case "import":
		s.pos += len("import")
		s.handleImport(s.line)
		return true
	}
	return false
}

// handlePackage records the file's `package a.b.c` declaration.
func (s *scanner) handlePackage() {
	s.skipInlineSpace()
	if name := s.readDotted(); name != "" {
		s.pkg = name
	}
	s.skipToLineEnd()
}

// handleImport records one `import a.b.C`, `import a.b.*`, or `import a.b.C as D`
// edge. The imported package is the dotted path with its final class segment
// removed (a wildcard keeps the whole path as the package); a trailing `as D`
// rename is recognised and dropped so it never contaminates the specifier.
func (s *scanner) handleImport(line int) {
	s.skipInlineSpace()
	path := s.readDotted()
	// A trailing `.*` wildcard: readDotted stops before the `.`, which we pick
	// up here so the whole path becomes the package.
	wildcard := false
	s.skipInlineSpace()
	if s.pos < len(s.src) && s.src[s.pos] == '.' {
		save := s.pos
		s.pos++
		s.skipInlineSpace()
		if s.pos < len(s.src) && s.src[s.pos] == '*' {
			wildcard = true
			s.pos++
		} else {
			s.pos = save
		}
	}
	// A trailing `as Alias` rename affects only the local name; the imported
	// symbol (and thus its package) is unchanged, so consume and discard it.
	s.skipInlineSpace()
	if s.peekWord() == "as" {
		s.pos += len("as")
		s.skipInlineSpace()
		s.skipWord() // the alias identifier
	}
	s.skipToLineEnd()

	if path == "" {
		return
	}
	if wildcard {
		s.out = append(s.out, importRef{Pkg: path, Display: path + ".*", Line: line})
		return
	}
	pkg, ok := packageOf(path)
	if !ok {
		// No dot: an import of a top-level (default-package) symbol. Its package
		// is the default package (""); attribute the edge to that path directly.
		s.out = append(s.out, importRef{Pkg: "", Display: path, Line: line})
		return
	}
	s.out = append(s.out, importRef{Pkg: pkg, Display: path, Line: line})
}

// packageOf splits a dotted class reference into its package (everything before
// the last dot). ok is false when there is no dot (a default-package symbol).
func packageOf(path string) (pkg string, ok bool) {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return "", false
	}
	return path[:i], true
}

// readDotted reads a dotted identifier chain (a, a.b, a.b.C) starting at pos,
// tolerating inline whitespace/comments around the dots. Backtick-quoted
// identifiers (`fun`) are accepted as segments. Returns "" if there is no
// identifier.
func (s *scanner) readDotted() string {
	var b strings.Builder
	first := true
	for {
		// save marks the position we rewind to when the segment does not
		// continue (a trailing `.` before `*`, or a non-identifier). Keeping the
		// scanner pointed at that `.` lets handleImport pick up a `.*` wildcard.
		save := s.pos
		if !first {
			s.skipInlineSpace()
			if s.pos >= len(s.src) || s.src[s.pos] != '.' {
				s.pos = save
				break
			}
			s.pos++ // consume '.'
			s.skipInlineSpace()
		}
		word := s.readSegment()
		if word == "" {
			s.pos = save // rewind over any consumed '.'/space so `.*` survives
			break
		}
		if !first {
			b.WriteByte('.')
		}
		b.WriteString(word)
		first = false
	}
	return b.String()
}

// readSegment reads one identifier segment, consuming it from the input. A
// Kotlin identifier may be backtick-quoted (`is`, `object`); the backticks are
// stripped from the returned segment but consumed from the source.
func (s *scanner) readSegment() string {
	if s.pos < len(s.src) && s.src[s.pos] == '`' {
		start := s.pos + 1
		p := start
		for p < len(s.src) && s.src[p] != '`' && s.src[p] != '\n' {
			p++
		}
		if p < len(s.src) && s.src[p] == '`' {
			seg := string(s.src[start:p])
			s.pos = p + 1
			return seg
		}
		return "" // unterminated backtick: not a usable segment
	}
	word := s.peekWord()
	s.pos += len(word)
	return word
}

func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// skipBlockComment consumes a `/* ... */` comment. Kotlin block comments nest,
// so track depth.
func (s *scanner) skipBlockComment() {
	s.pos += 2
	depth := 1
	for s.pos < len(s.src) && depth > 0 {
		switch {
		case s.src[s.pos] == '/' && s.peek(1) == '*':
			depth++
			s.pos += 2
		case s.src[s.pos] == '*' && s.peek(1) == '/':
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

// skipRawString consumes a `"""..."""` raw string (Kotlin). Raw strings do not
// process escapes; they end at the next `"""`.
func (s *scanner) skipRawString() {
	s.pos += 3 // opening """
	for s.pos < len(s.src) {
		switch {
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

// skipChar consumes a `'x'` char literal, honoring escapes.
func (s *scanner) skipChar() {
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			s.pos += 2
		case '\n':
			s.line++
			s.pos++
			return
		case '\'':
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

// skipToLineEnd advances past the rest of a `package`/`import` statement up to
// (but not consuming) the terminating newline, honoring comments and string/char
// literals so a `\n` inside one does not end line tracking early. A trailing
// `;` is consumed if present.
func (s *scanner) skipToLineEnd() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			return
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipRawString()
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

// skipInlineSpace skips spaces/tabs and `/* */` block comments, but NOT
// newlines. Kotlin `package`/`import` statements are terminated by end of line,
// so a qualified name never continues across a newline — stopping at `\n` keeps
// one statement from wrongly absorbing the next line. Block comments may sit
// between the dots of a name; a `//` line comment ends the statement, so it is
// left for skipToLineEnd rather than skipped here.
func (s *scanner) skipInlineSpace() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '/' && s.peek(1) == '*':
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

// isWordStart reports whether c can begin a Kotlin identifier. Bytes >= 0x80
// cover Unicode identifier parts.
func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
