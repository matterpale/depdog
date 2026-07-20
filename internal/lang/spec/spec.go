// Package spec is depdog's declarative language-adapter engine. A Spec declares
// a language's import syntax and module-resolution rules as data; the engine
// drives a generic comment/string-aware lexer (lexer.go), an import-surface
// extractor (surfaces.go), and a common-case resolver (resolve.go) to produce
// the same language-neutral *core.Graph a hand-written adapter does. The engine
// is additive: the nine hand-written adapters are unchanged, and a language
// whose scanning or resolution cannot meet their correctness bar declaratively
// stays hand-written.
//
// This file defines the Spec type and its YAML loader/validator. The wire format
// is documented by schema/adapter.schema.json, which this type mirrors field for
// field; keep the two in sync.
package spec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is one declarative language adapter. Its zero value is not usable; load
// one with Load or LoadFile, which validate it.
type Spec struct {
	// Name is the adapter identifier — the --lang value and the label in errors.
	Name string `yaml:"name"`
	// Markers are the project-root marker files in priority order. An entry
	// containing '*' is a glob matched against a directory's entries.
	Markers []string `yaml:"markers"`
	// Extensions are the source-file extensions to scan, including the dot.
	Extensions []string `yaml:"extensions"`
	// SkipDirs are directory names pruned from the file walk, in addition to
	// dotdirs (which are always pruned).
	SkipDirs []string `yaml:"skipDirs"`

	Comments Comments     `yaml:"comments"`
	Strings  []StringForm `yaml:"strings"`
	Imports  []Surface    `yaml:"imports"`
	Provides *Surface     `yaml:"provides"`
	Resolve  Resolve      `yaml:"resolve"`
	Stdlib   Stdlib       `yaml:"stdlib"`
	Tests    Tests        `yaml:"tests"`
	Module   Module       `yaml:"module"`
}

// Comments declares a language's comment syntaxes.
type Comments struct {
	// Line are line-comment prefixes, e.g. ["#"], ["//"].
	Line []string `yaml:"line"`
	// Block are block-comment forms.
	Block []BlockComment `yaml:"block"`
}

// BlockComment is one block-comment form.
type BlockComment struct {
	Open  string `yaml:"open"`
	Close string `yaml:"close"`
	// Nesting means an inner Open must be matched by an inner Close before the
	// outer comment ends (Rust /* */, Elm {- -}).
	Nesting bool `yaml:"nesting"`
	// LineAnchored means Open/Close are only recognised at the start of a line
	// (Ruby =begin/=end).
	LineAnchored bool `yaml:"lineAnchored"`
}

// StringForm is one string- or char-literal syntax.
type StringForm struct {
	// Kind selects the literal shape; empty means KindQuoted.
	Kind StringKind `yaml:"kind"`
	// Open is the opening delimiter (KindRawHash: the leading prefix, e.g. "r").
	Open string `yaml:"open"`
	// Close is the closing delimiter; empty defaults to Open.
	Close string `yaml:"close"`
	// Escape is the escape character; empty means no escapes.
	Escape string `yaml:"escape"`
	// Multiline allows the literal to span newlines.
	Multiline bool `yaml:"multiline"`
	// QuoteDoubling treats a doubled delimiter as a literal delimiter, not a
	// close (C# @"...""...").
	QuoteDoubling bool `yaml:"quoteDoubling"`
	// Quote is the body delimiter for KindRawHash / KindRawRun.
	Quote string `yaml:"quote"`
	// Hash is the repeatable level character for KindRawHash (e.g. "#").
	Hash string `yaml:"hash"`
	// MinRun is the minimum delimiter run length for KindRawRun (C#: 3).
	MinRun int `yaml:"minRun"`
}

// StringKind is the shape of a string/char literal.
type StringKind string

const (
	KindQuoted  StringKind = "quoted"   // "..." '...' """...""" with optional escapes
	KindChar    StringKind = "char"     // 'x' single-glyph char literal
	KindRawHash StringKind = "raw-hash" // Rust r#"..."# with a matched hash level
	KindRawRun  StringKind = "raw-run"  // C# raw string: a run of N>=MinRun quotes
)

// Surface is one import (or, for Spec.Provides, declaration) surface.
type Surface struct {
	// Keyword is the leading keyword, e.g. "require", "using", "namespace".
	Keyword string `yaml:"keyword"`
	// Capture is how the specifier after the keyword is read.
	Capture Capture `yaml:"capture"`
	// Kind is a label attached to captured refs, referenced by
	// Resolve.RelativeKinds. Empty means "plain".
	Kind string `yaml:"kind"`
	// Separator is the segment separator for CapturePathToken (".", "::").
	Separator string `yaml:"separator"`
	// Terminator is where a path-token statement ends.
	Terminator Terminator `yaml:"terminator"`
	// SkipTo is the delimiter skipped before reading a string for
	// CaptureSkipToString (Ruby autoload's ",").
	SkipTo string `yaml:"skipTo"`
	// PrefixKeywords are optional modifier words that may appear immediately
	// before the keyword (C# `global using`). When the current word is one of
	// these and the keyword follows, the surface still matches.
	PrefixKeywords []string `yaml:"prefixKeywords"`
	// SkipKeywords are optional modifier words that may appear immediately after
	// the keyword and are skipped before the specifier (C# `using static X`).
	SkipKeywords []string `yaml:"skipKeywords"`
	// Alias is a separator token (e.g. "=") for CapturePathToken: when it follows
	// the first token, the specifier is the token AFTER it (C# `using X = Y`
	// depends on Y, the alias target).
	Alias string `yaml:"alias"`
	// StrictTerminator, for CapturePathToken, rejects the match unless the
	// captured token is immediately followed by the terminator — so a C# using
	// *statement* (`using (res)`, `using var x = e`) is not read as a directive.
	StrictTerminator bool `yaml:"strictTerminator"`
}

// Capture is how a surface reads its specifier.
type Capture string

const (
	CaptureString       Capture = "string"         // require "x"
	CapturePathToken    Capture = "path-token"     // using a.b.c;
	CaptureSkipToString Capture = "skip-to-string" // autoload :C, "x"
)

// Terminator is where a path-token statement/declaration ends.
type Terminator string

const (
	TermNewline   Terminator = "newline"
	TermSemicolon Terminator = "semicolon"
	TermBrace     Terminator = "brace"
)

// Resolve declares how a captured specifier is classified and mapped to a node.
type Resolve struct {
	// Mode selects the resolution family; empty means ModePath.
	Mode Mode `yaml:"mode"`
	// Separator in the specifier that maps to a path separator; empty means "/".
	Separator string `yaml:"separator"`
	// RelativeKinds (path mode) are surface kinds resolved against the importing
	// file's directory.
	RelativeKinds []string `yaml:"relativeKinds"`
	// Roots (path mode) are project-relative directories a non-relative specifier
	// is resolved against, in order; empty means ["."].
	Roots []string `yaml:"roots"`
	// RootsIfExist (path mode) are extra roots used only when present on disk.
	RootsIfExist []string `yaml:"rootsIfExist"`
	// Extensions (path mode) are appended to an extension-less specifier; empty
	// means the top-level Extensions.
	Extensions []string `yaml:"extensions"`
	// IndexFiles (path mode) are basenames that make a directory importable.
	IndexFiles []string `yaml:"indexFiles"`
	// DropSelfEdges drops an in-module edge to the importing file's own dir.
	DropSelfEdges bool `yaml:"dropSelfEdges"`
}

// Mode is a resolution family.
type Mode string

const (
	ModePath      Mode = "path"       // specifier is a file path (Ruby, Lua)
	ModeNameIndex Mode = "name-index" // specifier is a declared name (C#, Elm)
)

// Stdlib is the standard-library classification table.
type Stdlib struct {
	// Match selects full-name or head-segment matching; empty means MatchFull.
	Match Match `yaml:"match"`
	// Separator for head matching (Ruby "/").
	Separator string `yaml:"separator"`
	// Modules is the inline std table.
	Modules []string `yaml:"modules"`
	// Prefixes are dotted namespace prefixes counted as std (C# System, Microsoft).
	Prefixes []string `yaml:"prefixes"`
	// Builtin references a std table bundled with depdog by name.
	Builtin string `yaml:"builtin"`
}

// Match is std-table matching granularity.
type Match string

const (
	MatchFull Match = "full" // match the whole specifier
	MatchHead Match = "head" // match the whole specifier, else the head segment
)

// Tests declares how test-only source is recognised.
type Tests struct {
	// StemSuffixes are file-name stem suffixes marking a test file (["_test"]).
	StemSuffixes []string `yaml:"stemSuffixes"`
	// Dirs are directory names anywhere in a path marking a test file (["spec"]).
	Dirs []string `yaml:"dirs"`
}

// Module declares how the graph's ModulePath is derived.
type Module struct {
	// Label is the fallback strategy; empty means LabelDirBasename.
	Label Label `yaml:"label"`
	// FromFile reads the module name from a manifest assignment.
	FromFile *ModuleFromFile `yaml:"fromFile"`
}

// Label is a ModulePath fallback strategy.
type Label string

const (
	LabelDirBasename Label = "dir-basename"
)

// ModuleFromFile reads a module name from `<recv>.<key> = "value"` in a manifest.
type ModuleFromFile struct {
	Glob          string `yaml:"glob"`
	Key           string `yaml:"key"`
	CommentPrefix string `yaml:"commentPrefix"`
}

// Load parses and validates a Spec from YAML bytes. Errors are human-actionable
// and name the offending field.
func Load(data []byte) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true) // reject unknown keys, mirroring the schema's additionalProperties:false
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parsing adapter spec: %w", err)
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// validate checks the required fields and enum values, returning the first
// problem as a human-actionable error naming the field.
func (s *Spec) validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("adapter spec: `name` is required (the --lang value, e.g. \"cs\")")
	}
	if len(s.Markers) == 0 {
		return fmt.Errorf("adapter spec %q: `markers` must list at least one project-root marker file", s.Name)
	}
	if len(s.Extensions) == 0 {
		return fmt.Errorf("adapter spec %q: `extensions` must list at least one source extension, e.g. [\".rb\"]", s.Name)
	}
	for i, e := range s.Extensions {
		if !strings.HasPrefix(e, ".") {
			return fmt.Errorf("adapter spec %q: extensions[%d] = %q must start with a dot", s.Name, i, e)
		}
	}
	if len(s.Imports) == 0 {
		return fmt.Errorf("adapter spec %q: `imports` must list at least one import surface", s.Name)
	}
	for i := range s.Imports {
		if err := s.Imports[i].validate(s.Name, fmt.Sprintf("imports[%d]", i)); err != nil {
			return err
		}
	}
	for i := range s.Strings {
		if err := s.Strings[i].validate(s.Name, i); err != nil {
			return err
		}
	}
	if s.Provides != nil {
		if err := s.Provides.validate(s.Name, "provides"); err != nil {
			return err
		}
		if s.Provides.Capture != CapturePathToken {
			return fmt.Errorf("adapter spec %q: provides.capture must be %q", s.Name, CapturePathToken)
		}
	}
	if err := s.Resolve.validate(s.Name); err != nil {
		return err
	}
	if s.Stdlib.Match != "" && s.Stdlib.Match != MatchFull && s.Stdlib.Match != MatchHead {
		return fmt.Errorf("adapter spec %q: stdlib.match = %q must be %q or %q", s.Name, s.Stdlib.Match, MatchFull, MatchHead)
	}
	if s.Stdlib.Match == MatchHead && s.Stdlib.Separator == "" {
		return fmt.Errorf("adapter spec %q: stdlib.match=head needs stdlib.separator", s.Name)
	}
	if s.Module.Label != "" && s.Module.Label != LabelDirBasename {
		return fmt.Errorf("adapter spec %q: module.label = %q must be %q", s.Name, s.Module.Label, LabelDirBasename)
	}
	if ff := s.Module.FromFile; ff != nil && (ff.Glob == "" || ff.Key == "") {
		return fmt.Errorf("adapter spec %q: module.fromFile needs both `glob` and `key`", s.Name)
	}
	return nil
}

func (surf *Surface) validate(name, where string) error {
	if strings.TrimSpace(surf.Keyword) == "" {
		return fmt.Errorf("adapter spec %q: %s.keyword is required", name, where)
	}
	switch surf.Capture {
	case CaptureString, CapturePathToken, CaptureSkipToString:
	case "":
		return fmt.Errorf("adapter spec %q: %s.capture is required (one of string, path-token, skip-to-string)", name, where)
	default:
		return fmt.Errorf("adapter spec %q: %s.capture = %q is not one of string, path-token, skip-to-string", name, where, surf.Capture)
	}
	if surf.Capture == CaptureSkipToString && surf.SkipTo == "" {
		return fmt.Errorf("adapter spec %q: %s uses capture=skip-to-string but sets no `skipTo` delimiter", name, where)
	}
	if surf.Capture == CapturePathToken && surf.Separator == "" {
		return fmt.Errorf("adapter spec %q: %s uses capture=path-token but sets no `separator`", name, where)
	}
	switch surf.Terminator {
	case "", TermNewline, TermSemicolon, TermBrace:
	default:
		return fmt.Errorf("adapter spec %q: %s.terminator = %q is not one of newline, semicolon, brace", name, where, surf.Terminator)
	}
	return nil
}

func (sf *StringForm) validate(name string, i int) error {
	switch sf.Kind {
	case "", KindQuoted, KindChar, KindRawHash, KindRawRun:
	default:
		return fmt.Errorf("adapter spec %q: strings[%d].kind = %q is not one of quoted, char, raw-hash, raw-run", name, i, sf.Kind)
	}
	switch sf.kind() {
	case KindRawRun:
		// A raw-run opens on a run of Quote (C# """), so it needs no Open prefix.
		if sf.Quote == "" {
			return fmt.Errorf("adapter spec %q: strings[%d] kind=raw-run needs `quote`", name, i)
		}
		if sf.MinRun < 1 {
			return fmt.Errorf("adapter spec %q: strings[%d] kind=raw-run needs minRun >= 1", name, i)
		}
	case KindRawHash:
		if sf.Open == "" {
			return fmt.Errorf("adapter spec %q: strings[%d].open (the raw-string prefix) is required", name, i)
		}
		if sf.Hash == "" || sf.Quote == "" {
			return fmt.Errorf("adapter spec %q: strings[%d] kind=raw-hash needs `hash` and `quote`", name, i)
		}
	default: // KindQuoted, KindChar
		if sf.Open == "" {
			return fmt.Errorf("adapter spec %q: strings[%d].open is required", name, i)
		}
	}
	return nil
}

func (r *Resolve) validate(name string) error {
	switch r.Mode {
	case "", ModePath, ModeNameIndex:
	default:
		return fmt.Errorf("adapter spec %q: resolve.mode = %q is not %q or %q", name, r.Mode, ModePath, ModeNameIndex)
	}
	return nil
}

// mode returns the effective resolution mode, defaulting to ModePath.
func (r *Resolve) mode() Mode {
	if r.Mode == "" {
		return ModePath
	}
	return r.Mode
}

// sep returns the effective specifier separator, defaulting to "/".
func (r *Resolve) sep() string {
	if r.Separator == "" {
		return "/"
	}
	return r.Separator
}

// kind returns the effective StringKind, defaulting to KindQuoted.
func (sf *StringForm) kind() StringKind {
	if sf.Kind == "" {
		return KindQuoted
	}
	return sf.Kind
}

// closeDelim returns the effective closing delimiter, defaulting to Open.
func (sf *StringForm) closeDelim() string {
	if sf.Close == "" {
		return sf.Open
	}
	return sf.Close
}

// kindOf returns a surface's effective kind label, defaulting to "plain".
func (surf *Surface) kindOf() string {
	if surf.Kind == "" {
		return "plain"
	}
	return surf.Kind
}
