package ruby

// scan is a comment/string-aware pass over Ruby source that extracts the
// feature references from the three import surfaces depdog cares about:
//
//	require "feature"          / require 'feature'
//	require_relative "../rel"  / require_relative 'rel'
//	autoload :Const, "feature" / autoload(:Const, "feature")
//
// It is deliberately NOT a naive line regex — a require-looking substring inside
// a comment or a string literal (including `=begin`/`=end` block comments and
// `%w`/heredoc-adjacent text) is never mistaken for a require, which is the
// single most important correctness property. The scanner tracks Ruby's lexical
// states (line comment, `=begin`/`=end` block comment, and single/double quoted
// strings with escapes) so a `require "x"` sitting inside a comment or string
// produces no edge.

// requireKind distinguishes the surface a reference came from: a relative
// require always resolves against the importing file's directory, while a plain
// require may be std, a gem, or (when it resolves on disk) in-module.
type requireKind int

const (
	kindRequire  requireKind = iota // require "x"
	kindRelative                    // require_relative "x"
	kindAutoload                    // autoload :C, "x"
)

// importRef is one captured feature reference and where it was found.
type importRef struct {
	Feature string      // the string argument, e.g. "net/http" or "../domain/order"
	Kind    requireKind // which require surface produced it
	Line    int         // 1-based line of the require keyword
}

// scan returns every require/require_relative/autoload reference in src, in
// source order.
func scan(src []byte) []importRef {
	s := &scanner{src: src, line: 1, atLineStart: true}
	s.run()
	return s.out
}

type scanner struct {
	src  []byte
	pos  int
	line int
	// atLineStart is true when only whitespace has been seen since the last
	// newline. `=begin`/`=end` block comment markers are only recognised at the
	// very start of a line, matching Ruby's lexer.
	atLineStart bool
	out         []importRef
}

func (s *scanner) run() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			s.line++
			s.pos++
			s.atLineStart = true
		case c == ' ' || c == '\t' || c == '\r' || c == '\f':
			s.pos++
		case s.atLineStart && s.atBlockComment():
			s.skipBlockComment()
		case c == '#':
			s.skipLineComment()
		case c == '"' || c == '\'':
			s.skipString()
			s.atLineStart = false
		case isWordStart(c):
			if s.tryRequire() {
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

// atBlockComment reports whether pos begins a `=begin` block-comment opener at
// the start of a line.
func (s *scanner) atBlockComment() bool {
	return s.hasPrefix("=begin")
}

// skipBlockComment consumes from a `=begin` line through the matching `=end`
// line (inclusive). An unterminated block runs to EOF.
func (s *scanner) skipBlockComment() {
	for s.pos < len(s.src) {
		lineStart := s.pos
		// Advance to end of this line.
		for s.pos < len(s.src) && s.src[s.pos] != '\n' {
			s.pos++
		}
		isEnd := hasPrefixAt(s.src, lineStart, "=end")
		if s.pos < len(s.src) { // consume the newline
			s.line++
			s.pos++
		}
		if isEnd {
			s.atLineStart = true
			return
		}
	}
}

// tryRequire is called with pos at a word start. If the word is one of the
// require surfaces it parses the reference (advancing past the argument) and
// returns true; otherwise it returns false, leaving pos unchanged so run() can
// skip the identifier normally.
func (s *scanner) tryRequire() bool {
	switch word := s.peekWord(); word {
	case "require", "require_relative":
		line := s.line
		s.pos += len(word)
		feature, ok := s.readStringArg()
		if ok {
			kind := kindRequire
			if word == "require_relative" {
				kind = kindRelative
			}
			s.out = append(s.out, importRef{Feature: feature, Kind: kind, Line: line})
		}
		s.atLineStart = false
		return true
	case "autoload":
		line := s.line
		s.pos += len(word)
		if feature, ok := s.readAutoloadArg(); ok {
			s.out = append(s.out, importRef{Feature: feature, Kind: kindAutoload, Line: line})
		}
		s.atLineStart = false
		return true
	}
	return false
}

// readStringArg skips optional spaces and an optional `(`, then reads a single
// string literal argument. Returns ("", false) when the token after the keyword
// is not a string (e.g. `require File.join(...)` — a dynamic require depdog
// cannot statically resolve, and correctly ignores).
func (s *scanner) readStringArg() (string, bool) {
	s.skipInlineSpace()
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		s.pos++
		s.skipInlineSpace()
	}
	if s.pos >= len(s.src) {
		return "", false
	}
	if c := s.src[s.pos]; c != '"' && c != '\'' {
		return "", false
	}
	return s.readStringLiteral()
}

// readAutoloadArg parses `autoload :Const, "feature"` (parentheses optional),
// skipping the symbol and comma to reach the string argument.
func (s *scanner) readAutoloadArg() (string, bool) {
	s.skipInlineSpace()
	if s.pos < len(s.src) && s.src[s.pos] == '(' {
		s.pos++
		s.skipInlineSpace()
	}
	// Skip the constant symbol (`:Name`) or bareword up to the comma.
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == ',' {
			s.pos++
			break
		}
		if c == '\n' {
			return "", false // no argument on this line
		}
		s.pos++
	}
	s.skipInlineSpace()
	if s.pos >= len(s.src) {
		return "", false
	}
	if c := s.src[s.pos]; c != '"' && c != '\'' {
		return "", false
	}
	return s.readStringLiteral()
}

// readStringLiteral reads a single- or double-quoted string starting at the
// opening quote and returns its contents (with escapes minimally handled).
func (s *scanner) readStringLiteral() (string, bool) {
	quote := s.src[s.pos]
	s.pos++
	var buf []byte
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch c {
		case '\\':
			if s.pos+1 < len(s.src) {
				buf = append(buf, s.src[s.pos+1])
				s.pos += 2
			} else {
				s.pos++
			}
		case quote:
			s.pos++
			return string(buf), true
		case '\n':
			// Unterminated string at EOL; bail so line tracking stays sane.
			s.line++
			s.pos++
			return string(buf), false
		default:
			buf = append(buf, c)
			s.pos++
		}
	}
	return string(buf), false
}

// skipLineComment consumes from `#` to end of line (the newline is left for
// run() so line tracking and atLineStart are handled in one place).
func (s *scanner) skipLineComment() {
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
}

// skipString consumes a string literal starting at the opening quote, tracking
// escapes and embedded newlines, without capturing it — used when a string is
// not a require argument.
func (s *scanner) skipString() {
	quote := s.src[s.pos]
	s.pos++
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; c {
		case '\\':
			if s.peek(1) == '\n' {
				s.line++
			}
			s.pos += 2
			if s.pos > len(s.src) {
				s.pos = len(s.src)
			}
		case '\n':
			s.line++
			s.pos++
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

// skipInlineSpace skips spaces and tabs but not a newline.
func (s *scanner) skipInlineSpace() {
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case ' ', '\t', '\r', '\f':
			s.pos++
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

func (s *scanner) hasPrefix(prefix string) bool {
	return hasPrefixAt(s.src, s.pos, prefix)
}

// hasPrefixAt reports whether src has prefix starting at index i.
func hasPrefixAt(src []byte, i int, prefix string) bool {
	if i+len(prefix) > len(src) {
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
