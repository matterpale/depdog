package typescript

// scan is a comment/string/template-literal-aware single pass over TS/JS
// source that extracts module specifiers from the four import surfaces:
// static import, re-export, dynamic import(), and CommonJS require(). It is
// deliberately NOT a naive line regex — a specifier-looking substring inside a
// comment, string, or template literal is never mistaken for an import, which
// is the single most important correctness property (see docs).

// kind labels which import surface produced a specifier.
type kind int

const (
	kindImport  kind = iota // import ... from '...'  or side-effect import '...'
	kindExport              // export ... from '...'
	kindDynamic             // import('...')
	kindRequire             // require('...')
)

// specifier is one captured module specifier and where it was found.
type specifier struct {
	Raw  string // the string-literal argument, unescaped of the surrounding quotes only
	Line int    // 1-based line of the specifier
	Kind kind
}

// scanner walks the byte stream tracking lexical state so that import surfaces
// are only matched in code (never inside comments/strings/templates).
type scanner struct {
	src  []byte
	pos  int
	line int
	// template stack: each entry is the brace depth at which a `${` opened an
	// interpolation. When the matching `}` is hit we pop back into template
	// state. A non-empty stack means we may be inside interpolation code.
	tmplBraceDepth []int
	braceDepth     int
	out            []specifier
}

func scan(src []byte) []specifier {
	s := &scanner{src: src, line: 1}
	s.run()
	return s.out
}

func (s *scanner) run() {
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
		case c == '\'' || c == '"':
			s.skipString(c)
		case c == '`':
			s.skipTemplate()
		case c == '{':
			s.braceDepth++
			s.pos++
		case c == '}':
			s.braceDepth--
			// Closing an interpolation returns us to the enclosing template.
			if n := len(s.tmplBraceDepth); n > 0 && s.braceDepth == s.tmplBraceDepth[n-1] {
				s.tmplBraceDepth = s.tmplBraceDepth[:n-1]
				s.pos++ // consume '}'
				s.continueTemplate()
				continue
			}
			s.pos++
		default:
			if !s.tryImportSurface() {
				s.pos++
			}
		}
	}
}

func (s *scanner) peek(n int) byte {
	if s.pos+n < len(s.src) {
		return s.src[s.pos+n]
	}
	return 0
}

func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

func (s *scanner) skipBlockComment() {
	s.pos += 2 // consume "/*"
	for s.pos < len(s.src) {
		if s.src[s.pos] == '\n' {
			s.line++
		}
		if s.src[s.pos] == '*' && s.peek(1) == '/' {
			s.pos += 2
			return
		}
		s.pos++
	}
}

// skipString consumes a single- or double-quoted string, honoring backslash
// escapes. The scanner is positioned on the opening quote.
func (s *scanner) skipString(quote byte) {
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch c {
		case '\\':
			s.pos += 2 // skip the escape and the escaped char
			continue
		case '\n':
			s.line++ // unterminated, but stay robust
			s.pos++
		case quote:
			s.pos++
			return
		default:
			s.pos++
		}
	}
}

// skipTemplate consumes a template literal starting at the backtick. On `${`
// it hands control back to run() as interpolation code (pushing a brace-depth
// marker); continueTemplate() resumes the literal after the matching `}`.
func (s *scanner) skipTemplate() {
	s.pos++ // opening backtick
	s.templateBody()
}

func (s *scanner) continueTemplate() {
	// Called right after consuming the interpolation-closing '}'.
	s.templateBody()
}

func (s *scanner) templateBody() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\\':
			s.pos += 2
		case c == '\n':
			s.line++
			s.pos++
		case c == '`':
			s.pos++
			return
		case c == '$' && s.peek(1) == '{':
			// Enter interpolation: it is code. Record the brace depth so the
			// matching '}' pops us back here.
			s.tmplBraceDepth = append(s.tmplBraceDepth, s.braceDepth)
			s.braceDepth++
			s.pos += 2 // consume "${"
			return     // back to run() in code state
		default:
			s.pos++
		}
	}
}

// tryImportSurface attempts to match one of the import surfaces at the current
// position. It only fires on an identifier boundary so it never matches inside
// a larger identifier. Returns true (and advances) if it consumed a surface.
func (s *scanner) tryImportSurface() bool {
	c := s.src[s.pos]
	if !isWordStart(c) {
		return false
	}
	// Must be at a word boundary: the previous byte must not be part of an
	// identifier (so "foo.import" or "reimport" don't match).
	if s.pos > 0 && isWordPart(s.src[s.pos-1]) {
		return false
	}
	word := s.readWord()
	switch word {
	case "import":
		return s.handleImportKeyword()
	case "export":
		return s.handleExportKeyword()
	case "require":
		return s.handleCall(kindRequire)
	}
	// Advance past the word so we don't rescan it byte by byte.
	s.pos += len(word)
	return true
}

// handleImportKeyword handles both `import(...)` (dynamic) and
// `import ... from '...'` / side-effect `import '...'`. The scanner is
// positioned at the start of the "import" word.
func (s *scanner) handleImportKeyword() bool {
	after := s.pos + len("import")
	// import( => dynamic import. s.pos is still at the "import" word so
	// handleCall can measure the keyword itself.
	if next := skipSpacesAt(s.src, after); next < len(s.src) && s.src[next] == '(' {
		return s.handleCall(kindDynamic)
	}
	// Otherwise a static import: scan forward until we hit the specifier,
	// which is either right after `from` or (side-effect import) the first
	// string literal before a statement terminator. We consume the "import"
	// word and let the from-scanner take over.
	s.pos = after
	s.consumeFromSpecifier(kindImport)
	return true
}

func (s *scanner) handleExportKeyword() bool {
	after := s.pos + len("export")
	s.pos = after
	s.consumeFromSpecifier(kindExport)
	return true
}

// consumeFromSpecifier scans an import/export statement body looking for the
// `from '...'` clause (or, for a side-effect import, a leading string
// literal). It stops at the statement terminator (`;` or newline reaching a
// non-continuation) or when it captures the specifier. It is careful to run
// the same comment/string state machine locally so a `from` inside a string
// isn't matched, and template-literal / brace tracking stays consistent with
// run().
func (s *scanner) consumeFromSpecifier(k kind) {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
		case c == ';':
			s.pos++
			return
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '\'' || c == '"':
			// A string literal in import position is the side-effect specifier
			// (`import './x'`) OR the specifier following `from`. Either way it
			// is the specifier we want; capture and finish the statement.
			line := s.line
			raw := s.captureStringLiteral(c)
			s.out = append(s.out, specifier{Raw: raw, Line: line, Kind: k})
			s.skipToStatementEnd()
			return
		default:
			s.pos++
		}
	}
}

// handleCall handles `require(...)` and dynamic `import(...)`. The scanner is
// positioned at the start of the keyword ("require") or at the "import" word
// (for dynamic, s.pos already points at "import"). We find the '(' and, if the
// first non-space argument is a string literal, capture it.
func (s *scanner) handleCall(k kind) bool {
	// Advance past the keyword word.
	kw := "require"
	if k == kindDynamic {
		kw = "import"
	}
	p := skipSpacesAt(s.src, s.pos+len(kw))
	if p >= len(s.src) || s.src[p] != '(' {
		// Not actually a call; consume the word and move on.
		s.pos += len(kw)
		return true
	}
	p = skipSpacesAt(s.src, p+1) // just after '('
	if p < len(s.src) && (s.src[p] == '\'' || s.src[p] == '"') {
		s.pos = p
		line := s.line
		raw := s.captureStringLiteral(s.src[p])
		s.out = append(s.out, specifier{Raw: raw, Line: line, Kind: k})
		return true
	}
	// Non-literal argument (variable, expression, template) — not statically
	// resolvable. Consume the keyword and continue normal scanning.
	s.pos += len(kw)
	return true
}

// captureStringLiteral reads a quoted string starting at the opening quote and
// returns its contents (unescaping only the closing-quote logic). The scanner
// is advanced past the closing quote.
func (s *scanner) captureStringLiteral(quote byte) string {
	s.pos++ // opening quote
	start := s.pos
	var buf []byte
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch c {
		case '\\':
			// Preserve the escaped character verbatim (specifiers rarely have
			// escapes; keeping the raw char is fine for resolution).
			if buf == nil {
				buf = append(buf, s.src[start:s.pos]...)
			}
			if s.pos+1 < len(s.src) {
				buf = append(buf, s.src[s.pos+1])
				s.pos += 2
				continue
			}
			s.pos++
		case quote:
			s.pos++ // closing quote
			if buf != nil {
				return string(buf)
			}
			return string(s.src[start : s.pos-1])
		case '\n':
			s.line++
			if buf != nil {
				buf = append(buf, c)
			}
			s.pos++
		default:
			if buf != nil {
				buf = append(buf, c)
			}
			s.pos++
		}
	}
	if buf != nil {
		return string(buf)
	}
	return string(s.src[start:])
}

// skipToStatementEnd advances to just past the next `;` or to end of line,
// keeping comment/string state so trailing content doesn't spuriously match.
func (s *scanner) skipToStatementEnd() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ';':
			s.pos++
			return
		case c == '\n':
			s.line++
			s.pos++
			return
		case c == '/' && s.peek(1) == '/':
			s.skipLineComment()
			return
		case c == '/' && s.peek(1) == '*':
			s.skipBlockComment()
		case c == '\'' || c == '"':
			s.skipString(c)
		default:
			s.pos++
		}
	}
}

// readWord returns the identifier word starting at s.pos without advancing.
func (s *scanner) readWord() string {
	end := s.pos
	for end < len(s.src) && isWordPart(s.src[end]) {
		end++
	}
	return string(s.src[s.pos:end])
}

func skipSpacesAt(src []byte, p int) int {
	for p < len(src) {
		switch src[p] {
		case ' ', '\t', '\r', '\n':
			p++
		default:
			return p
		}
	}
	return p
}

func isWordStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
