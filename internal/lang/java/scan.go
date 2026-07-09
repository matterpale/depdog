package java

import "strings"

// scan is a comment/string-aware pass over Java source that extracts the file's
// package declaration and its import statements:
//
//	package a.b.c;
//	import a.b.C;
//	import static a.b.C.member;
//	import a.b.*;                 (on-demand: the imported package is a.b)
//
// It is deliberately NOT a naive line regex — an import-looking substring inside
// a comment or a string literal (including text blocks) is never mistaken for an
// import, which is the single most important correctness property. The scanner
// tracks Java's lexical states (// line comments, /* */ block comments, "…"
// strings with escapes, '…' char literals, and """…""" text blocks) so a
// `import x.Y;` sitting inside a string produces no edge.

// scanResult holds one file's package declaration and its imports, in source
// order.
type scanResult struct {
	pkg     string      // dotted package from `package a.b.c;`, "" for the default package
	imports []importRef // every import statement, in source order
}

// importRef is one captured import statement and where it was found. Pkg is the
// package the imported symbol lives in (the import path with its trailing class
// segment removed for a class import; the full path for a wildcard). Static is
// true for `import static`; used only to shape the display specifier.
type importRef struct {
	Pkg     string // dotted package of the imported symbol, e.g. "a.b"
	Display string // specifier shown in reports, e.g. "a.b.C" or "a.b.*"
	Static  bool
	Line    int // 1-based line of the `import` keyword
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
	// last statement boundary (`;`, `{`, `}`) or the file start. `package` and
	// `import` keywords are only recognised there — an `import` used as an
	// identifier mid-expression is never an import statement in Java.
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
			// A newline alone is not a statement boundary in Java (statements end
			// at `;`/`{`/`}`), but leading indentation before a keyword still
			// counts as statement start, so preserve atStmtStart across newlines.
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipTextBlock()
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

// handlePackage records the file's `package a.b.c;` declaration.
func (s *scanner) handlePackage() {
	s.skipInlineSpaceAndComments()
	if name := s.readDotted(); name != "" {
		s.pkg = name
	}
	s.skipToSemicolon()
}

// handleImport records one `import [static] a.b.C;` or `import a.b.*;` edge.
// The imported package is the dotted path with its final class segment removed
// (a wildcard keeps the whole path as the package).
func (s *scanner) handleImport(line int) {
	s.skipInlineSpaceAndComments()
	static := false
	if s.peekWord() == "static" {
		static = true
		s.pos += len("static")
		s.skipInlineSpaceAndComments()
	}
	path := s.readDotted()
	// A trailing `.*` wildcard: readDotted stops before the `.`, which we pick
	// up here so the whole path becomes the package.
	wildcard := false
	s.skipInlineSpaceAndComments()
	if s.pos < len(s.src) && s.src[s.pos] == '.' {
		save := s.pos
		s.pos++
		s.skipInlineSpaceAndComments()
		if s.pos < len(s.src) && s.src[s.pos] == '*' {
			wildcard = true
			s.pos++
		} else {
			s.pos = save
		}
	}
	s.skipToSemicolon()

	if path == "" {
		return
	}
	if wildcard {
		s.out = append(s.out, importRef{Pkg: path, Display: path + ".*", Static: static, Line: line})
		return
	}
	pkg, ok := packageOf(path)
	if !ok {
		// No dot: an import of a top-level (default-package) type. Its package is
		// the default package (""); attribute the edge to that path directly.
		s.out = append(s.out, importRef{Pkg: "", Display: path, Static: static, Line: line})
		return
	}
	s.out = append(s.out, importRef{Pkg: pkg, Display: path, Static: static, Line: line})
}

// packageOf splits a dotted class reference into its package (everything before
// the last dot). ok is false when there is no dot (a default-package type).
func packageOf(path string) (pkg string, ok bool) {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return "", false
	}
	return path[:i], true
}

// readDotted reads a dotted identifier chain (a, a.b, a.b.C) starting at pos,
// tolerating inline whitespace/comments around the dots the way Java's grammar
// permits. Returns "" if there is no identifier.
func (s *scanner) readDotted() string {
	var b strings.Builder
	first := true
	for {
		// save marks the position we rewind to when the segment does not
		// continue (a trailing `.` before `*`, or a non-identifier). Keeping the
		// scanner pointed at that `.` lets handleImport pick up a `.*` wildcard.
		save := s.pos
		if !first {
			s.skipInlineSpaceAndComments()
			if s.pos >= len(s.src) || s.src[s.pos] != '.' {
				s.pos = save
				break
			}
			s.pos++ // consume '.'
			s.skipInlineSpaceAndComments()
		}
		word := s.peekWord()
		if word == "" {
			s.pos = save // rewind over any consumed '.'/space so `.*` survives
			break
		}
		s.pos += len(word)
		if !first {
			b.WriteByte('.')
		}
		b.WriteString(word)
		first = false
	}
	return b.String()
}

func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// skipBlockComment consumes a `/* ... */` comment (Java block comments do not
// nest).
func (s *scanner) skipBlockComment() {
	s.pos += 2
	for s.pos < len(s.src) {
		if s.src[s.pos] == '*' && s.peek(1) == '/' {
			s.pos += 2
			return
		}
		if s.src[s.pos] == '\n' {
			s.line++
		}
		s.pos++
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

// skipTextBlock consumes a `"""..."""` text block (Java 15+), honoring escapes.
func (s *scanner) skipTextBlock() {
	s.pos += 3 // opening """
	for s.pos < len(s.src) {
		switch {
		case s.src[s.pos] == '\\':
			s.pos += 2
		case s.src[s.pos] == '\n':
			s.line++
			s.pos++
		case s.src[s.pos] == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.pos += 3
			return
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

// skipToSemicolon advances past the rest of a statement up to and including its
// terminating `;`, honoring comments and string/char literals so a `;` inside
// one does not end the statement early.
func (s *scanner) skipToSemicolon() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipTextBlock()
		case c == '"':
			s.skipString()
		case c == '\'':
			s.skipChar()
		case c == ';':
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

// skipInlineSpaceAndComments skips spaces/tabs/newlines and //, /* */ comments
// (Java allows comments between the dots of a qualified name).
func (s *scanner) skipInlineSpaceAndComments() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '\n':
			s.line++
			s.pos++
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
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

// isWordStart reports whether c can begin a Java identifier. `$` is a legal
// identifier character in Java; bytes >= 0x80 cover Unicode identifier parts.
func isWordStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
