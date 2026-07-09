package rust

// scan is a comment/string-aware pass over Rust source that extracts the module
// references from the import surfaces:
//
//	use path::to::item;                 (also `pub use`, `pub(crate) use`)
//	use path::{a, b::c, self};          (grouped, nested)
//	mod name;                           (a child module declaration; `mod x {}` is not an edge)
//	extern crate name;                  (a 2015-edition external crate binding)
//
// It is deliberately NOT a naive line regex — a use-looking substring inside a
// comment or a string literal (including raw and byte strings) is never mistaken
// for an import, which is the single most important correctness property. The
// scanner tracks Rust's lexical states (// line comments, /* */ nestable block
// comments, "..." strings with escapes, r#"..."# raw strings, and char/byte
// literals) so a `use x::y;` sitting inside a string produces no edge.

// importRef is one captured module reference and where it was found. Path is the
// double-colon path as written (e.g. "crate::domain::order", "std::fmt",
// "super::x", "self::y") with the crate-root token preserved.
type importRef struct {
	Path string // "::"-joined path, e.g. "crate::a::b" or "std::io"
	Line int    // 1-based line of the statement keyword
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
	// atItemStart is true when only whitespace/comments have been seen since the
	// last statement terminator (`;`, `{`, `}`) or newline-preceding boundary.
	// use/mod/extern items are only recognised there, which avoids treating a
	// path expression `foo::use_something` as an item keyword.
	atItemStart bool
	out         []importRef
}

func (s *scanner) run() {
	s.atItemStart = true
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '"':
			s.skipString()
			s.atItemStart = false
		case c == 'r' && (s.peek(1) == '"' || s.peek(1) == '#'):
			if s.tryRawString() {
				s.atItemStart = false
			} else {
				s.skipWord()
				s.atItemStart = false
			}
		case c == 'b' && (s.peek(1) == '"' || s.peek(1) == '\'' || s.peek(1) == 'r'):
			if s.tryByteLiteral() {
				s.atItemStart = false
			} else {
				s.skipWord()
				s.atItemStart = false
			}
		case c == '\'':
			s.skipCharOrLifetime()
			s.atItemStart = false
		case c == ';' || c == '{' || c == '}':
			s.pos++
			s.atItemStart = true
		case isWordStart(c):
			if s.atItemStart && s.tryItem() {
				continue
			}
			s.skipWord()
			s.atItemStart = false
		default:
			s.pos++
			s.atItemStart = false
		}
	}
}

// tryItem is called at a word start that begins an item position. It handles the
// three import surfaces plus the `pub`/`pub(...)` visibility prefix. Returns true
// (having consumed the item) when it matched an import surface; false otherwise
// (leaving s.pos unchanged so run() can skip the word normally).
func (s *scanner) tryItem() bool {
	save := s.pos
	word := s.peekWord()
	if word == "pub" {
		// Consume `pub` and an optional `(crate)` / `(super)` / `(in path)`
		// restriction, then look at the next word.
		s.pos += len(word)
		s.skipInlineSpace()
		if s.pos < len(s.src) && s.src[s.pos] == '(' {
			s.skipParens()
			s.skipInlineSpace()
		}
		word = s.peekWord()
	}
	switch word {
	case "use":
		s.pos += len(word)
		s.handleUse(s.line)
		return true
	case "mod":
		s.pos += len(word)
		s.handleMod(s.line)
		return true
	case "extern":
		if s.handleExtern() {
			return true
		}
	}
	// Not an import item; rewind so run() skips the original word.
	s.pos = save
	return false
}

// handleUse parses everything up to the terminating `;` of a `use` item,
// expanding grouped and nested braces into one importRef per leaf path.
func (s *scanner) handleUse(line int) {
	s.skipInlineSpaceAndComments()
	paths := s.readUseTree("")
	for _, p := range paths {
		s.out = append(s.out, importRef{Path: p, Line: line})
	}
	s.skipToSemicolon()
}

// readUseTree reads a use-tree at the current position, prefixed by the
// accumulated `prefix` path, and returns the flattened leaf paths. It recurses
// into `{ ... }` groups. It stops at the `;` or the closing `}` of an enclosing
// group (leaving that delimiter for the caller).
func (s *scanner) readUseTree(prefix string) []string {
	var out []string
	for {
		s.skipInlineSpaceAndComments()
		if s.pos >= len(s.src) {
			return out
		}
		switch s.src[s.pos] {
		case '{':
			s.pos++
			out = append(out, s.readUseGroup(prefix)...)
			s.skipInlineSpaceAndComments()
		case ';', '}':
			return out
		default:
			seg := s.readPathSegments()
			if seg == "" {
				// Unexpected token; bail to the terminator.
				return out
			}
			full := join(prefix, seg)
			// After a path, we may hit `::{`, a rename (`as x`), a comma, or the end.
			s.skipInlineSpaceAndComments()
			if s.pos+1 < len(s.src) && s.src[s.pos] == ':' && s.src[s.pos+1] == ':' &&
				s.pos+2 < len(s.src) && s.src[s.pos+2] == '{' {
				s.pos += 2 // consume `::`, leave `{` for the group
				s.pos++    // consume `{`
				out = append(out, s.readUseGroup(full)...)
			} else {
				out = append(out, full)
				s.skipRename()
			}
			s.skipInlineSpaceAndComments()
			if s.pos < len(s.src) && s.src[s.pos] == ',' {
				s.pos++
				continue
			}
			return out
		}
	}
}

// readUseGroup reads the comma-separated members of a `{ ... }` use-group whose
// opening brace has already been consumed, each prefixed by `prefix`. It
// consumes the closing `}`.
func (s *scanner) readUseGroup(prefix string) []string {
	var out []string
	for {
		s.skipInlineSpaceAndComments()
		if s.pos >= len(s.src) {
			return out
		}
		if s.src[s.pos] == '}' {
			s.pos++
			return out
		}
		if s.src[s.pos] == ',' {
			s.pos++
			continue
		}
		out = append(out, s.readUseTree(prefix)...)
		s.skipInlineSpaceAndComments()
		if s.pos < len(s.src) && s.src[s.pos] == ',' {
			s.pos++
			continue
		}
		if s.pos < len(s.src) && s.src[s.pos] == '}' {
			s.pos++
			return out
		}
		// Guard against non-advancing loops on malformed input.
		if s.pos >= len(s.src) {
			return out
		}
	}
}

// readPathSegments reads a `a::b::c` chain (no braces), stopping before a
// trailing `::{` group, a comma, an `as` rename, or the item terminator. Leading
// `::` (absolute path) is preserved as an empty head segment via the token.
func (s *scanner) readPathSegments() string {
	var segs []string
	for {
		s.skipInlineSpaceAndComments()
		w := s.peekWord()
		if w == "" {
			// A leading `::` (crate-external absolute) or `*` glob.
			if s.pos+1 < len(s.src) && s.src[s.pos] == ':' && s.src[s.pos+1] == ':' {
				s.pos += 2
				continue
			}
			break
		}
		if w == "as" {
			break // a rename follows the path, not part of it
		}
		s.pos += len(w)
		segs = append(segs, w)
		s.skipInlineSpaceAndComments()
		// A `::` continues the path unless it introduces a `{` group.
		if s.pos+1 < len(s.src) && s.src[s.pos] == ':' && s.src[s.pos+1] == ':' {
			if s.pos+2 < len(s.src) && s.src[s.pos+2] == '{' {
				break // leave `::{` for the caller
			}
			if s.pos+2 < len(s.src) && s.src[s.pos+2] == '*' {
				s.pos += 3 // `::*` glob: the path is the segments so far
				break
			}
			s.pos += 2
			continue
		}
		break
	}
	return joinSegs(segs)
}

// skipRename consumes an optional `as <ident>` following a use path.
func (s *scanner) skipRename() {
	s.skipInlineSpaceAndComments()
	if s.peekWord() == "as" {
		s.pos += len("as")
		s.skipInlineSpaceAndComments()
		s.skipWord()
	}
}

// handleMod records a `mod name;` child-module declaration as an in-crate edge
// to the child module. An inline `mod name { ... }` is not an edge (its body is
// scanned normally), so we only emit when the declaration ends in `;`.
func (s *scanner) handleMod(line int) {
	s.skipInlineSpaceAndComments()
	name := s.peekWord()
	if name == "" {
		return
	}
	s.pos += len(name)
	s.skipInlineSpaceAndComments()
	if s.pos < len(s.src) && s.src[s.pos] == ';' {
		s.out = append(s.out, importRef{Path: modToken + "::" + name, Line: line})
		// leave the `;` for run() to reset atItemStart.
	}
	// For `mod name { ... }` the `{` resets atItemStart and the body is scanned.
}

// handleExtern parses `extern crate name;`, recording an external-crate edge. A
// non-`crate` extern (e.g. `extern "C" { ... }`) is not an import; returns false
// so tryItem rewinds.
func (s *scanner) handleExtern() bool {
	save := s.pos
	s.pos += len("extern")
	s.skipInlineSpaceAndComments()
	if s.peekWord() != "crate" {
		s.pos = save
		return false
	}
	line := s.line
	s.pos += len("crate")
	s.skipInlineSpaceAndComments()
	name := s.peekWord()
	if name == "" {
		s.pos = save
		return false
	}
	s.pos += len(name)
	s.out = append(s.out, importRef{Path: name, Line: line})
	s.skipToSemicolon()
	return true
}

// skipToSemicolon advances past the next top-level `;` (used to finish an item),
// respecting brace nesting so a `{}` group inside a use item does not terminate
// early.
func (s *scanner) skipToSemicolon() {
	depth := 0
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
		case c == '{':
			depth++
			s.pos++
		case c == '}':
			if depth > 0 {
				depth--
			}
			s.pos++
		case c == ';' && depth == 0:
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// skipBlockComment consumes a `/* ... */` comment, honoring Rust's nesting.
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

// skipString consumes a normal `"..."` string with backslash escapes.
func (s *scanner) skipString() {
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			if s.peek(1) == '\n' {
				s.line++
			}
			s.pos += 2
		case '\n':
			s.line++
			s.pos++
		case '"':
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

// tryRawString consumes a raw string r"...", r#"..."#, r##"..."##, ... starting
// at the leading `r`. Returns false if it is not actually a raw string.
func (s *scanner) tryRawString() bool {
	p := s.pos + 1 // past 'r'
	hashes := 0
	for p < len(s.src) && s.src[p] == '#' {
		hashes++
		p++
	}
	if p >= len(s.src) || s.src[p] != '"' {
		return false
	}
	p++ // opening quote
	closing := "\"" + repeatHash(hashes)
	for p < len(s.src) {
		if s.src[p] == '\n' {
			s.line++
			p++
			continue
		}
		if s.src[p] == '"' && matchAt(s.src, p, closing) {
			p += len(closing)
			s.pos = p
			return true
		}
		p++
	}
	s.pos = len(s.src)
	return true
}

// tryByteLiteral consumes b"...", br"..."/br#"..."#, and b'...' byte literals
// starting at the leading `b`. Returns false if it is not one.
func (s *scanner) tryByteLiteral() bool {
	switch s.peek(1) {
	case '"':
		s.pos++ // 'b'
		s.skipString()
		return true
	case '\'':
		s.pos++ // 'b'
		s.skipCharOrLifetime()
		return true
	case 'r':
		s.pos++ // 'b', leaving raw string starting at 'r'
		return s.tryRawString()
	}
	return false
}

// skipCharOrLifetime consumes a `'a'` char literal or a `'lifetime` label. Both
// begin with a single quote; a char literal is a (possibly escaped) char then a
// closing quote, while a lifetime is a bare identifier with no closing quote.
func (s *scanner) skipCharOrLifetime() {
	s.pos++ // opening quote
	if s.pos >= len(s.src) {
		return
	}
	if s.src[s.pos] == '\\' {
		// Escaped char literal: skip the escape and find the closing quote.
		s.pos += 2
		for s.pos < len(s.src) && s.src[s.pos] != '\'' && s.src[s.pos] != '\n' {
			s.pos++
		}
		if s.pos < len(s.src) && s.src[s.pos] == '\'' {
			s.pos++
		}
		return
	}
	// A single char followed by a closing quote is a char literal; otherwise a
	// lifetime label (identifier, no closing quote).
	if s.peek(1) == '\'' {
		s.pos += 2 // the char and its closing quote
		return
	}
	// Lifetime: consume the identifier.
	s.skipWord()
}

// skipParens consumes a balanced `( ... )` starting at the opening paren.
func (s *scanner) skipParens() {
	depth := 0
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '(':
			depth++
			s.pos++
		case ')':
			depth--
			s.pos++
			if depth == 0 {
				return
			}
		case '\n':
			s.line++
			s.pos++
		default:
			s.pos++
		}
	}
}

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

// skipInlineSpace skips spaces/tabs/newlines (Rust is free-form; a use item may
// span lines).
func (s *scanner) skipInlineSpace() {
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case ' ', '\t', '\r', '\f':
			s.pos++
		case '\n':
			s.line++
			s.pos++
		default:
			return
		}
	}
}

// skipInlineSpaceAndComments skips whitespace and // or /* */ comments that may
// appear between path segments of a multi-line use item.
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

func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}

// join concatenates a path prefix and a segment chain with `::`.
func join(prefix, seg string) string {
	if prefix == "" {
		return seg
	}
	if seg == "" {
		return prefix
	}
	return prefix + "::" + seg
}

// joinSegs joins path segments with `::`.
func joinSegs(segs []string) string {
	out := ""
	for i, s := range segs {
		if i == 0 {
			out = s
			continue
		}
		out += "::" + s
	}
	return out
}

// repeatHash returns n `#` characters.
func repeatHash(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '#'
	}
	return string(out)
}

// matchAt reports whether src at position p begins with want.
func matchAt(src []byte, p int, want string) bool {
	if p+len(want) > len(src) {
		return false
	}
	for i := 0; i < len(want); i++ {
		if src[p+i] != want[i] {
			return false
		}
	}
	return true
}
