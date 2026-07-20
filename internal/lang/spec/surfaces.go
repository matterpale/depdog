package spec

import "strings"

// surfaces.go turns the lexer's code-position word starts into import references.
// It is the spec-driven equivalent of a hand-written adapter's tryRequire/tryItem:
// at each word start the extractor checks whether the word begins one of the
// spec's import surfaces and, if so, reads and *consumes* the specifier so it is
// not re-scanned (the consumption the lexer alone does not do).
//
// The captured vocabulary — deliberately small, covering the common surfaces, not
// a full parser:
//
//   - CaptureString      require "x"                (a quoted-string argument)
//   - CaptureSkipToString autoload :C, "x"          (skip a delimiter, then a string)
//   - CapturePathToken    using a.b.c; / import Foo  (a separated identifier chain)
//
// plus the provides declaration surface (namespace Foo.Bar) that feeds name-index
// resolution. Exotic surfaces — Rust's brace-grouped `use a::{b, c}`, Python/TS
// `from X import Y` — stay hand-written (D3); the engine is additive, so that costs
// nothing.

// ref is one captured import (or declared name) and where it was found — the
// engine analogue of a hand-written adapter's importRef.
type ref struct {
	Specifier string // the specifier as written: a feature/path/module name
	Kind      string // the surface's kind label ("plain", "relative", ...)
	Line      int    // 1-based line of the surface keyword
}

// extraction is one source file's scan result: its import references and, for
// name-index resolution, the names the file declares via the provides surface.
type extraction struct {
	imports  []ref
	provides []ref
}

// extract scans src per sp and returns its import references and declared names,
// in source order.
func extract(sp *Spec, src []byte) extraction {
	e := &extractor{spec: sp}
	newLexer(sp, src).run(e.onWord)
	return extraction{imports: e.imports, provides: e.provides}
}

type extractor struct {
	spec     *Spec
	imports  []ref
	provides []ref
}

// onWord is the lexer callback. It fires only at code-position word starts, so a
// keyword inside a comment or string is never seen here. Returning true means the
// surface (keyword + specifier) was consumed; false leaves the word for the lexer
// to skip.
func (e *extractor) onWord(l *lexer) bool {
	word := l.peekWord()
	if p := e.spec.Provides; p != nil && word == p.Keyword {
		return e.matchProvides(l, p, word)
	}
	for i := range e.spec.Imports {
		if surf := &e.spec.Imports[i]; word == surf.Keyword {
			return e.matchImport(l, surf, word)
		}
	}
	return false
}

// matchImport consumes an import surface whose keyword matched. Like the
// hand-written scanners, it returns true (the keyword is consumed) even when no
// specifier is captured — a dynamic argument such as `require File.join(...)` is
// correctly ignored, not re-interpreted.
func (e *extractor) matchImport(l *lexer, surf *Surface, word string) bool {
	line := l.line
	l.pos += len(word)
	if spec, ok := e.capture(l, surf); ok {
		e.imports = append(e.imports, ref{Specifier: spec, Kind: surf.kindOf(), Line: line})
	}
	return true
}

// matchProvides consumes a declaration surface (namespace Foo.Bar), recording the
// declared name. The trailing `{` or `;` is left for the lexer so the block body
// is scanned normally.
func (e *extractor) matchProvides(l *lexer, surf *Surface, word string) bool {
	line := l.line
	l.pos += len(word)
	if name, ok := e.capturePathToken(l, surf.Separator); ok {
		e.provides = append(e.provides, ref{Specifier: name, Kind: surf.kindOf(), Line: line})
	}
	return true
}

// capture reads the specifier after a consumed keyword per the surface's capture
// mechanism. ok is false when there is no static specifier to record.
func (e *extractor) capture(l *lexer, surf *Surface) (string, bool) {
	switch surf.Capture {
	case CaptureString:
		return e.captureString(l)
	case CaptureSkipToString:
		return e.captureSkipToString(l, surf.SkipTo)
	case CapturePathToken:
		spec, ok := e.capturePathToken(l, surf.Separator)
		if ok {
			l.skipToTerminator(surf.Terminator)
		}
		return spec, ok
	}
	return "", false
}

// captureString reads `[(] "arg"` — an optional opening paren then a single
// string literal (require "x", require("x")). It returns ("", false) when the
// token after the keyword is not a string (a dynamic argument), leaving that
// token unconsumed, exactly like Ruby's readStringArg.
func (e *extractor) captureString(l *lexer) (string, bool) {
	l.skipInlineSpace()
	if l.pos < len(l.src) && l.src[l.pos] == '(' {
		l.pos++
		l.skipInlineSpace()
	}
	return e.readStringLiteral(l)
}

// captureSkipToString reads `[(] ... <skipTo> "arg"` — skip an opening paren and
// everything up to the skipTo delimiter, then a string (Ruby autoload :C, "x").
// Bails if the line ends before the delimiter or the argument is not a string.
func (e *extractor) captureSkipToString(l *lexer, skipTo string) (string, bool) {
	l.skipInlineSpace()
	if l.pos < len(l.src) && l.src[l.pos] == '(' {
		l.pos++
		l.skipInlineSpace()
	}
	delim := byte(0)
	if skipTo != "" {
		delim = skipTo[0]
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\n' {
			return "", false // no argument on this line
		}
		l.pos++
		if c == delim {
			break
		}
	}
	l.skipInlineSpace()
	return e.readStringLiteral(l)
}

// capturePathToken reads a separated identifier chain (a.b.c, a::b::c, Foo.Bar),
// tolerating whitespace around the separator. Returns the chain re-joined with the
// separator, or ("", false) when no identifier follows.
func (e *extractor) capturePathToken(l *lexer, sep string) (string, bool) {
	l.skipInlineSpace()
	var segs []string
	for {
		w := l.peekWord()
		if w == "" {
			break
		}
		l.pos += len(w)
		segs = append(segs, w)
		l.skipInlineSpace()
		if sep != "" && l.has(sep) {
			l.pos += len(sep)
			l.skipInlineSpace()
			continue
		}
		break
	}
	if len(segs) == 0 {
		return "", false
	}
	return strings.Join(segs, sep), true
}

// readStringLiteral reads a quoted-string literal at pos and returns its contents
// (escapes minimally unwrapped: a backslash-escaped byte contributes the byte).
// It matches the first KindQuoted string form whose opener sits at pos. A
// specifier string does not span lines, so an embedded newline bails (unterminated).
func (e *extractor) readStringLiteral(l *lexer) (string, bool) {
	for i := range e.spec.Strings {
		sf := &e.spec.Strings[i]
		if sf.kind() == KindQuoted && l.has(sf.Open) {
			return l.readQuotedContents(sf)
		}
	}
	return "", false
}

// readQuotedContents consumes a quoted literal whose opener sits at pos and
// returns its unwrapped contents. Used only for specifier arguments (single line).
func (l *lexer) readQuotedContents(sf *StringForm) (string, bool) {
	closeD := sf.closeDelim()
	l.pos += len(sf.Open)
	hasEsc := sf.Escape != ""
	var esc byte
	if hasEsc {
		esc = sf.Escape[0]
	}
	var buf []byte
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case hasEsc && c == esc:
			if l.pos+1 < len(l.src) {
				buf = append(buf, l.src[l.pos+1])
				l.pos += 2
			} else {
				l.pos++
			}
		case l.has(closeD):
			l.pos += len(closeD)
			return string(buf), true
		case c == '\n':
			l.line++
			l.pos++
			return string(buf), false // unterminated at end of line
		default:
			buf = append(buf, c)
			l.pos++
		}
	}
	return string(buf), false
}

// skipToTerminator consumes the rest of a path-token statement so its tail (an
// `as` rename, an `exposing`/`;`, imported symbol names) is not re-scanned. A
// brace terminator (a block-scoped declaration) is left for the lexer.
func (l *lexer) skipToTerminator(t Terminator) {
	switch t {
	case TermSemicolon:
		for l.pos < len(l.src) {
			switch l.src[l.pos] {
			case '\n':
				l.line++
				l.pos++
			case ';':
				l.pos++
				return
			default:
				l.pos++
			}
		}
	case TermBrace:
		// leave the '{' or ';' for run(): the block body is scanned normally.
	default: // TermNewline / unset: stop before the newline (left for run()).
		for l.pos < len(l.src) && l.src[l.pos] != '\n' {
			l.pos++
		}
	}
}

// skipInlineSpace skips spaces and tabs but not a newline.
func (l *lexer) skipInlineSpace() {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\r', '\f':
			l.pos++
		default:
			return
		}
	}
}
