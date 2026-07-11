package scala

import "strings"

// scan is a comment/string-aware pass over Scala source that extracts the file's
// package declaration and its import statements:
//
//	package a.b.c                (leading single-line clause)
//	import a.b.C
//	import a.b.{C, D}            (selector group)
//	import a.b.{C => E}          (renamed selector; the local name E is dropped)
//	import a.b._                 (Scala 2 wildcard: the imported package is a.b)
//	import a.b.*                 (Scala 3 wildcard)
//	import a.b.given             (Scala 3 given import: the package is a.b)
//
// Like the Kotlin/Java scanners, this is deliberately NOT a naive line regex — an
// import-looking substring inside a comment or a string literal is never mistaken
// for an import, which is the single most important correctness property. The
// scanner tracks Scala's lexical states (// line comments, /* */ block comments —
// which NEST in Scala — "…" strings with escapes, '…' char literals, s"…" /
// f"…" / raw"…" interpolated strings, and """…""" triple-quoted strings) so an
// `import x.Y` sitting inside a string produces no edge.
//
// A chained/nested clause of the form `package a { package b { ... } }` is only
// partially supported: the leading `package a` name is captured, but the inner
// `package b` blocks are treated as ordinary statements and their names are not
// folded into a compound package. This mirrors the common single-file layout;
// the nested form is rare in practice.

// scanResult holds one file's package declaration and its imports, in source
// order.
type scanResult struct {
	pkg     string      // dotted package from `package a.b.c`, "" for the empty package
	imports []importRef // every import selector, in source order
}

// importRef is one captured import selector and where it was found. Pkg is the
// package the imported symbol lives in (the import path with its trailing symbol
// segment removed for a single import; the full path for a wildcard/given).
type importRef struct {
	Pkg     string // dotted package of the imported symbol, e.g. "a.b"
	Display string // specifier shown in reports, e.g. "a.b.C" or "a.b._"
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
	// pkgDone is set once the leading package clause has been captured; a later
	// `package` keyword (e.g. an inner `package object` or a nested package block)
	// is then ignored so it does not overwrite the file's leading package.
	pkgDone bool
	// atStmtStart is true when only whitespace/comments have been seen since the
	// last statement boundary (a newline, `;`, `{`, `}`) or the file start.
	// `package` and `import` keywords are only recognised there — an `import` used
	// as an identifier mid-expression is never an import statement.
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
			// A newline is a statement boundary in Scala: `package`/`import`
			// statements end at end of line (Scala's optional-semicolon rule), so a
			// keyword at the start of the next line is a fresh statement.
			s.atStmtStart = true
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '"' && s.peek(1) == '"' && s.peek(2) == '"':
			s.skipTripleString()
			s.atStmtStart = false
		case c == '"':
			s.skipString()
			s.atStmtStart = false
		case c == '\'':
			s.skipCharOrSymbol()
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

// handlePackage records the file's leading `package a.b.c` declaration. Only the
// first package clause is captured; a later `package` keyword (a `package object`
// or a nested package block) is skipped, keeping the file's declared package the
// leading one. A `package object foo` declaration is not a plain package name, so
// it is ignored for package attribution.
func (s *scanner) handlePackage() {
	if s.pkgDone {
		s.skipToLineEnd()
		return
	}
	s.skipInlineSpace()
	// `package object foo` declares an object, not a package path — do not treat
	// `object` as a package segment.
	if s.peekWord() == "object" {
		s.pkgDone = true
		s.skipToLineEnd()
		return
	}
	if name := s.readDotted(); name != "" {
		s.pkg = name
		s.pkgDone = true
	}
	s.skipToLineEnd()
}

// handleImport records the import selectors of one statement. It handles every
// Scala import form:
//
//	import a.b.C            -> edge to package a.b, display a.b.C
//	import a.b.{C, D}       -> edges to a.b, display a.b.C and a.b.D
//	import a.b.{C => E}     -> edge to a.b, display a.b.C (the rename is dropped)
//	import a.b._            -> edge to a.b, display a.b._   (Scala 2 wildcard)
//	import a.b.*            -> edge to a.b, display a.b.*    (Scala 3 wildcard)
//	import a.b.given        -> edge to a.b, display a.b.given
func (s *scanner) handleImport(line int) {
	s.skipInlineSpace()
	path := s.readDotted()

	// After the dotted prefix, the import may continue with `.{...}`, `._`, `.*`,
	// or `.given`. readDotted already consumed simple segments; a trailing dot
	// before one of these tokens was left unconsumed so we can dispatch here.
	s.skipInlineSpace()
	if s.pos < len(s.src) && s.src[s.pos] == '.' {
		save := s.pos
		s.pos++ // consume '.'
		s.skipInlineSpace()
		switch {
		case s.pos < len(s.src) && s.src[s.pos] == '{':
			s.emitSelectors(path, line)
			s.skipToLineEnd()
			return
		case s.pos < len(s.src) && s.src[s.pos] == '_':
			s.pos++
			s.emit(importRef{Pkg: path, Display: path + "._", Line: line})
			s.skipToLineEnd()
			return
		case s.pos < len(s.src) && s.src[s.pos] == '*':
			s.pos++
			s.emit(importRef{Pkg: path, Display: path + ".*", Line: line})
			s.skipToLineEnd()
			return
		case s.peekWord() == "given":
			s.pos += len("given")
			s.emit(importRef{Pkg: path, Display: path + ".given", Line: line})
			s.skipToLineEnd()
			return
		default:
			s.pos = save // not a special trailer: rewind and treat as a plain import
		}
	}

	s.skipToLineEnd()

	if path == "" {
		return
	}
	// A single import `import a.b.C`: the package is a.b, the symbol is C. An
	// `import a.b.C => E` rename at the top level is invalid Scala outside a
	// selector group, so no rename handling is needed here.
	s.emitSingle(path, line)
}

// emitSingle records a single `import a.b.C` selector: the imported package is
// the dotted path with its final segment removed.
func (s *scanner) emitSingle(path string, line int) {
	pkg, ok := packageOf(path)
	if !ok {
		// No dot: an import of a top-level (empty-package) symbol. Its package is
		// the empty package (""); attribute the edge to that path directly.
		s.emit(importRef{Pkg: "", Display: path, Line: line})
		return
	}
	s.emit(importRef{Pkg: pkg, Display: path, Line: line})
}

// emitSelectors handles a selector group `import a.b.{C, D => E, given F, _}`.
// Every selected symbol lives in package `path`. A `=> local` rename is dropped
// (the imported symbol, and thus the package, is unchanged). The catch-all
// members `_`, `*`, and `given` inside a group each contribute a wildcard/given
// edge to `path`.
func (s *scanner) emitSelectors(path string, line int) {
	s.pos++ // consume '{'
	for s.pos < len(s.src) {
		s.skipSelectorSpace()
		if s.pos >= len(s.src) {
			return
		}
		c := s.src[s.pos]
		if c == '}' {
			s.pos++
			return
		}
		if c == ',' {
			s.pos++
			continue
		}
		// A selector is a name (possibly `_`, `*`, or the soft keyword `given`),
		// optionally followed by `=> local`.
		if c == '_' {
			s.pos++
			s.emit(importRef{Pkg: path, Display: path + "._", Line: line})
			continue
		}
		if c == '*' {
			s.pos++
			s.emit(importRef{Pkg: path, Display: path + ".*", Line: line})
			continue
		}
		name := s.readSegment()
		if name == "" {
			// Not an identifier and not a separator: advance to avoid a stall.
			s.pos++
			continue
		}
		// `given` (with no following type) imports all givens of the package; a
		// bare `given` selector maps to the given-import edge.
		if name == "given" {
			s.skipSelectorSpace()
			// `given SomeType` selects one given by type; the type name is part of
			// the selector but the package is still `path`. Consume the optional
			// type name so it is not mistaken for a fresh selector.
			if s.pos < len(s.src) && isWordStart(s.src[s.pos]) {
				s.readSegment()
			}
			s.emit(importRef{Pkg: path, Display: path + ".given", Line: line})
			continue
		}
		// A trailing `=> local` rename: consume and discard — the imported symbol
		// (and its package) is unchanged.
		s.skipSelectorSpace()
		if s.pos+1 < len(s.src) && s.src[s.pos] == '=' && s.src[s.pos+1] == '>' {
			s.pos += 2
			s.skipSelectorSpace()
			s.readSegment() // the local alias (may be `_` to hide; still a segment)
		}
		s.emit(importRef{Pkg: path, Display: path + "." + name, Line: line})
	}
}

// emit appends one import edge unless its path is empty.
func (s *scanner) emit(ref importRef) {
	if ref.Display == "" {
		return
	}
	s.out = append(s.out, ref)
}

// packageOf splits a dotted reference into its package (everything before the
// last dot). ok is false when there is no dot (an empty-package symbol).
func packageOf(path string) (pkg string, ok bool) {
	i := strings.LastIndexByte(path, '.')
	if i < 0 {
		return "", false
	}
	return path[:i], true
}

// readDotted reads a dotted identifier chain (a, a.b, a.b.C) starting at pos,
// tolerating inline whitespace/comments around the dots. Backtick-quoted
// identifiers (`type`) are accepted as segments. It stops before a trailing dot
// whose following token is not a plain identifier (so a `.{`, `._`, `.*`, or
// `.given` trailer survives for handleImport to dispatch). Returns "" if there is
// no identifier.
func (s *scanner) readDotted() string {
	var b strings.Builder
	first := true
	for {
		save := s.pos
		if !first {
			s.skipInlineSpace()
			if s.pos >= len(s.src) || s.src[s.pos] != '.' {
				s.pos = save
				break
			}
			s.pos++ // consume '.'
			s.skipInlineSpace()
			// Do not consume the dot when the next token is a special trailer; leave
			// it for handleImport to see the `.` again.
			if s.pos < len(s.src) {
				nc := s.src[s.pos]
				if nc == '{' || nc == '_' || nc == '*' || s.peekWord() == "given" {
					s.pos = save
					break
				}
			}
		}
		word := s.readSegment()
		if word == "" {
			s.pos = save // rewind over any consumed '.'/space so the trailer survives
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

// readSegment reads one identifier segment, consuming it from the input. A Scala
// identifier may be backtick-quoted (`type`, `match`); the backticks are stripped
// from the returned segment but consumed from the source.
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

// skipBlockComment consumes a `/* ... */` comment. Scala block comments nest, so
// track depth.
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

// skipString consumes a `"..."` string literal, honoring escapes. Interpolated
// strings (s"…", f"…") share the same delimiters at this level; a `$`-expression
// inside is skipped as ordinary bytes, which is safe because an `import` token
// there is never at statement start.
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

// skipTripleString consumes a `"""..."""` triple-quoted string (Scala). Triple
// strings do not process escapes; they end at the next `"""`.
func (s *scanner) skipTripleString() {
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

// skipCharOrSymbol consumes a `'x'` char literal (honoring escapes) or a Scala
// symbol literal / quoted-identifier tick. A char literal is exactly one glyph
// (or an escape) followed by a closing `'`; anything else is a lone tick (a
// symbol like 'foo or a syntactic marker) which we simply step over.
func (s *scanner) skipCharOrSymbol() {
	// Look ahead for a well-formed char literal: 'c' or '\x'.
	if s.peek(1) == '\\' {
		// Escaped char: '\n', '\t', etc. Consume up to the closing quote.
		s.pos += 2 // ' and backslash
		if s.pos < len(s.src) {
			s.pos++ // the escaped char
		}
		if s.pos < len(s.src) && s.src[s.pos] == '\'' {
			s.pos++ // closing quote
		}
		return
	}
	if s.peek(2) == '\'' {
		// Simple char literal 'c'.
		s.pos += 3
		return
	}
	// A lone tick (symbol literal 'name or otherwise): step over just the tick so
	// the following identifier is scanned normally.
	s.pos++
}

// skipToLineEnd advances past the rest of a `package`/`import` statement up to
// (but not consuming) the terminating newline, honoring comments and string/char
// literals so a `\n` inside one does not end line tracking early. A trailing `;`
// is consumed if present.
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
			s.skipTripleString()
		case c == '"':
			s.skipString()
		case c == '\'':
			s.skipCharOrSymbol()
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

// skipInlineSpace skips spaces/tabs and `/* */` block comments, but NOT newlines.
// Scala `package`/`import` statements are terminated by end of line, so a
// qualified name never continues across a newline — stopping at `\n` keeps one
// statement from wrongly absorbing the next line. Block comments may sit between
// the dots of a name; a `//` line comment ends the statement, so it is left for
// skipToLineEnd rather than skipped here.
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

// skipSelectorSpace skips whitespace (including newlines — a selector group `{ }`
// may span lines) and block comments inside an import selector group.
func (s *scanner) skipSelectorSpace() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '\n':
			s.line++
			s.pos++
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
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

// isWordStart reports whether c can begin a Scala identifier. Bytes >= 0x80 cover
// Unicode identifier parts.
func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
