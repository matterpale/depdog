package spec

import "strings"

// lexer is the generic, spec-driven equivalent of a hand-written adapter's
// comment/string-aware scanner (cf. ruby/scan.go, rust/scan.go, python/scan.go).
// It walks source per a Spec's `comments` and `strings` declarations and invokes
// a callback at each word start that sits in *code* — never inside a comment or a
// string literal. That single property is the whole correctness ballgame: a
// keyword-looking substring inside a comment or string must produce no import.
//
// The lexer owns the cursor (pos/line/atLineStart); the callback (import-surface
// extraction, surfaces.go) may consume a construct by advancing l.pos and
// returning true, exactly as the hand-written tryRequire/tryItem do.
type lexer struct {
	spec *Spec
	src  []byte
	pos  int
	line int
	// atLineStart is true when only whitespace has been seen since the last
	// newline. It gates line-anchored block comments (Ruby =begin/=end) and is
	// exposed to the callback so a surface can require line-start position.
	atLineStart bool
}

func newLexer(sp *Spec, src []byte) *lexer {
	return &lexer{spec: sp, src: src, line: 1, atLineStart: true}
}

// run drives the scan. At each code-position word start it calls onWord; if
// onWord returns true it consumed a construct (and advanced l.pos), so run
// continues from the new position; if false, run skips the identifier. Comments
// and strings are consumed by run itself, so onWord only ever fires in code.
func (l *lexer) run(onWord func(l *lexer) bool) {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.pos++
			l.atLineStart = true
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			l.pos++
		case l.atLineStart && l.tryLineAnchoredBlock():
			// consumed; atLineStart handled inside
		case l.tryLineComment():
			// consumed to end of line; the newline is left for the loop
		case l.tryBlockComment():
			// consumed; atLineStart set inside
		case l.tryString():
			l.atLineStart = false
		case isWordStart(c):
			if onWord(l) {
				continue
			}
			l.skipWord()
			l.atLineStart = false
		default:
			l.pos++
			l.atLineStart = false
		}
	}
}

// tryLineAnchoredBlock consumes a line-anchored block comment (Ruby =begin/=end)
// when one opens at pos. Only called when atLineStart.
func (l *lexer) tryLineAnchoredBlock() bool {
	for i := range l.spec.Comments.Block {
		b := &l.spec.Comments.Block[i]
		if b.LineAnchored && l.has(b.Open) {
			l.skipLineAnchoredBlock(b.Close)
			return true
		}
	}
	return false
}

// tryLineComment consumes a line comment when one opens at pos, advancing to end
// of line but leaving the newline for run (so line/atLineStart update in one
// place).
func (l *lexer) tryLineComment() bool {
	for _, prefix := range l.spec.Comments.Line {
		if prefix != "" && l.has(prefix) {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
			return true
		}
	}
	return false
}

// tryBlockComment consumes a non-line-anchored block comment (Rust /* */, Elm
// {- -}, C# /* */) when one opens at pos, honoring nesting when declared.
func (l *lexer) tryBlockComment() bool {
	for i := range l.spec.Comments.Block {
		b := &l.spec.Comments.Block[i]
		if !b.LineAnchored && l.has(b.Open) {
			l.skipBlockComment(b.Open, b.Close, b.Nesting)
			l.atLineStart = false
			return true
		}
	}
	return false
}

// skipLineAnchoredBlock consumes from the current (opening) line through the line
// that starts with close, inclusive — matching Ruby's =begin/=end lexer. An
// unterminated block runs to EOF.
func (l *lexer) skipLineAnchoredBlock(close string) {
	for l.pos < len(l.src) {
		lineStart := l.pos
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
		isEnd := hasPrefixAt(l.src, lineStart, close)
		if l.pos < len(l.src) { // consume the newline
			l.line++
			l.pos++
		}
		if isEnd {
			l.atLineStart = true
			return
		}
	}
}

// skipBlockComment consumes a block comment whose open has already been matched
// at pos. With nesting, an inner open must be matched by an inner close before
// the outer comment ends. An unterminated comment runs to EOF.
func (l *lexer) skipBlockComment(open, close string, nesting bool) {
	l.pos += len(open)
	depth := 1
	for l.pos < len(l.src) && depth > 0 {
		switch {
		case nesting && l.has(open):
			depth++
			l.pos += len(open)
		case l.has(close):
			depth--
			l.pos += len(close)
		case l.src[l.pos] == '\n':
			l.line++
			l.pos++
		default:
			l.pos++
		}
	}
}

// tryString consumes a string/char literal when one of the spec's string forms
// opens at pos. Forms are tried in spec order, so a more specific opener (a
// triple quote, a C# raw-run) must be listed before a shorter one it contains.
func (l *lexer) tryString() bool {
	for i := range l.spec.Strings {
		if l.skipStringForm(&l.spec.Strings[i]) {
			return true
		}
	}
	return false
}

func (l *lexer) skipStringForm(sf *StringForm) bool {
	switch sf.kind() {
	case KindQuoted:
		if !l.has(sf.Open) {
			return false
		}
		l.skipQuoted(sf)
		return true
	case KindChar:
		if !l.has(sf.Open) {
			return false
		}
		l.skipCharOrLifetime(sf)
		return true
	case KindRawHash:
		return l.skipRawHash(sf)
	case KindRawRun:
		return l.skipRawRun(sf)
	}
	return false
}

// skipQuoted consumes a quoted string (normal, triple-quoted, or verbatim). It
// honors an optional escape character, multiline spanning, and C#-style quote
// doubling (a doubled delimiter is a literal delimiter, not a close).
func (l *lexer) skipQuoted(sf *StringForm) {
	closeD := sf.closeDelim()
	l.pos += len(sf.Open)
	hasEsc := sf.Escape != ""
	var esc byte
	if hasEsc {
		esc = sf.Escape[0]
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case hasEsc && c == esc:
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
				l.line++
			}
			l.pos += 2
			if l.pos > len(l.src) {
				l.pos = len(l.src)
			}
		case l.has(closeD):
			if sf.QuoteDoubling && len(closeD) == 1 && l.pos+1 < len(l.src) && l.src[l.pos+1] == closeD[0] {
				l.pos += 2 // a doubled delimiter is a literal delimiter
				continue
			}
			l.pos += len(closeD)
			return
		case c == '\n':
			l.line++
			l.pos++
			if !sf.Multiline {
				return // unterminated single-line string at EOL; bail
			}
		default:
			l.pos++
		}
	}
}

// skipCharOrLifetime consumes a 'x' char literal or (Rust) a 'lifetime label.
// Both begin with a single quote; a char literal is a (possibly escaped) glyph
// then a closing quote, while a lifetime is a bare identifier with no closing
// quote. In languages without lifetimes (C#, Elm) every char literal closes, so
// the lifetime branch is simply never taken.
func (l *lexer) skipCharOrLifetime(sf *StringForm) {
	q := sf.Open[0]
	l.pos += len(sf.Open) // opening quote
	if l.pos >= len(l.src) {
		return
	}
	if sf.Escape != "" && l.src[l.pos] == sf.Escape[0] {
		l.pos += 2
		for l.pos < len(l.src) && l.src[l.pos] != q && l.src[l.pos] != '\n' {
			l.pos++
		}
		if l.pos < len(l.src) && l.src[l.pos] == q {
			l.pos++
		}
		return
	}
	if l.peek(1) == q {
		l.pos += 2 // the glyph and its closing quote
		return
	}
	l.skipWord() // lifetime label
}

// skipRawHash consumes a Rust-style raw string r"..."/r#"..."#/r##"..."##, whose
// closing delimiter is a quote followed by the same number of hashes as the
// opener. Returns false when pos is not actually a raw-string opener (e.g. the
// identifier "real").
func (l *lexer) skipRawHash(sf *StringForm) bool {
	if !l.has(sf.Open) {
		return false
	}
	q := sf.Quote[0]
	hash := sf.Hash[0]
	p := l.pos + len(sf.Open)
	hashes := 0
	for p < len(l.src) && l.src[p] == hash {
		hashes++
		p++
	}
	if p >= len(l.src) || l.src[p] != q {
		return false // prefix was an identifier, not a raw string
	}
	p++ // opening quote
	closing := string(q) + strings.Repeat(string(hash), hashes)
	for p < len(l.src) {
		if l.src[p] == '\n' {
			l.line++
			p++
			continue
		}
		if l.src[p] == q && hasPrefixAt(l.src, p, closing) {
			l.pos = p + len(closing)
			return true
		}
		p++
	}
	l.pos = len(l.src)
	return true
}

// skipRawRun consumes a raw string literal that opens on a run of N>=MinRun
// quotes and closes on a run of at least N quotes (C# 11 """…"""). An optional
// Open prefix precedes the run so interpolated raw strings are distinguished
// ($"""…""", $$"""…"""). Returns false when the prefix is absent or the run at
// pos is too short to open one.
func (l *lexer) skipRawRun(sf *StringForm) bool {
	start := l.pos
	if sf.Open != "" {
		if !l.has(sf.Open) {
			return false
		}
		start += len(sf.Open)
	}
	q := sf.Quote[0]
	if start >= len(l.src) || l.src[start] != q {
		return false
	}
	n := 0
	for start+n < len(l.src) && l.src[start+n] == q {
		n++
	}
	if n < sf.MinRun {
		return false
	}
	p := start + n
	for p < len(l.src) {
		if l.src[p] == '\n' {
			l.line++
			p++
			continue
		}
		if l.src[p] == q {
			run := 0
			for p+run < len(l.src) && l.src[p+run] == q {
				run++
			}
			if run >= n {
				l.pos = p + run
				return true
			}
			p += run
			continue
		}
		p++
	}
	l.pos = len(l.src)
	return true
}

// skipWord advances past a run of identifier bytes.
func (l *lexer) skipWord() {
	for l.pos < len(l.src) && isWordPart(l.src[l.pos]) {
		l.pos++
	}
}

// peekWord returns the identifier at pos without advancing.
func (l *lexer) peekWord() string {
	p := l.pos
	for p < len(l.src) && isWordPart(l.src[p]) {
		p++
	}
	return string(l.src[l.pos:p])
}

func (l *lexer) peek(n int) byte {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *lexer) has(prefix string) bool {
	return hasPrefixAt(l.src, l.pos, prefix)
}

// hasPrefixAt reports whether src has prefix starting at index i.
func hasPrefixAt(src []byte, i int, prefix string) bool {
	if prefix == "" || i+len(prefix) > len(src) {
		return false
	}
	for j := 0; j < len(prefix); j++ {
		if src[i+j] != prefix[j] {
			return false
		}
	}
	return true
}

func isWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isWordPart(c byte) bool {
	return isWordStart(c) || (c >= '0' && c <= '9')
}
