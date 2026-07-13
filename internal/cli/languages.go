package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/lang"
	"github.com/matterpale/depdog/internal/lang/elm"
	"github.com/matterpale/depdog/internal/lang/golang"
	"github.com/matterpale/depdog/internal/lang/java"
	"github.com/matterpale/depdog/internal/lang/kotlin"
	"github.com/matterpale/depdog/internal/lang/python"
	"github.com/matterpale/depdog/internal/lang/ruby"
	"github.com/matterpale/depdog/internal/lang/rust"
	"github.com/matterpale/depdog/internal/lang/scala"
	"github.com/matterpale/depdog/internal/lang/typescript"
)

// errResolution marks the two project-resolution failures — no project root
// found, or no depdog.yaml at a resolved root — so the polyglot fallback (D1)
// can tell them apart from genuine evaluation errors (parse failures,
// violations). A bare `depdog check` that fails to resolve a single project
// falls back to unit discovery; a config that fails to *parse* does not.
var errResolution = errors.New("project resolution failed")

// languages is the registry of supported language adapters — the single place
// multi-language support is wired. To add a language, implement lang.Loader in
// internal/lang/<name> and add one entry here (see lang.Adapter); detection, the
// --lang flag, dispatch and every error message read from this slice.
var languages = []lang.Adapter{
	{
		Name:    "go",
		Markers: []string{"go.mod"},
		Root:    config.ModuleRoot, // enforces the single-module (no go.work) rule
		New:     func(root string) lang.Loader { return &golang.Loader{Dir: root} },
	},
	{
		Name:    "ts",
		Markers: []string{"tsconfig.json", "package.json"},
		New:     func(root string) lang.Loader { return &typescript.Loader{Dir: root} },
	},
	{
		Name:    "py",
		Markers: []string{"pyproject.toml", "setup.py", "setup.cfg"},
		New:     func(root string) lang.Loader { return &python.Loader{Dir: root} },
	},
	{
		Name:    "rb",
		Markers: []string{"Gemfile", ".ruby-version", "Rakefile"},
		New:     func(root string) lang.Loader { return &ruby.Loader{Dir: root} },
	},
	{
		Name:    "rs",
		Markers: []string{"Cargo.toml"},
		New:     func(root string) lang.Loader { return &rust.Loader{Dir: root} },
	},
	{
		Name:    "java",
		Markers: []string{"pom.xml", "build.gradle", "build.gradle.kts"},
		New:     func(root string) lang.Loader { return &java.Loader{Dir: root} },
	},
	{
		// Kotlin projects build with Gradle's Kotlin DSL. The markers are the
		// Kotlin-DSL scripts only: build.gradle.kts / settings.gradle.kts. The
		// generic build.gradle and pom.xml are deliberately NOT listed here even
		// though the loader accepts them as roots — they are shared with the Java
		// adapter, and claiming them would make every plain-Maven/Groovy-Gradle
		// project ambiguous between java and kt. A Kotlin project that roots only
		// on such a shared marker is selected with --lang kt.
		Name:    "kt",
		Markers: []string{"build.gradle.kts", "settings.gradle.kts"},
		New:     func(root string) lang.Loader { return &kotlin.Loader{Dir: root} },
	},
	{
		// Scala projects build with sbt (build.sbt) or Mill (build.sc). Both
		// markers are Scala-specific and not shared with any other adapter, so no
		// ambiguity carve-out is needed.
		Name:    "scala",
		Markers: []string{"build.sbt", "build.sc"},
		New:     func(root string) lang.Loader { return &scala.Loader{Dir: root} },
	},
	{
		// Elm projects are rooted by elm.json. The marker is Elm-specific and not
		// shared with any other adapter, so no ambiguity carve-out is needed.
		Name:    "elm",
		Markers: []string{"elm.json"},
		New:     func(root string) lang.Loader { return &elm.Loader{Dir: root} },
	},
}

// languageNames lists the registered --lang values, for flag help and errors.
func languageNames() []string {
	names := make([]string, len(languages))
	for i, a := range languages {
		names[i] = a.Name
	}
	return names
}

// adapterByName returns the adapter registered under a --lang value.
func adapterByName(name string) (lang.Adapter, bool) {
	for _, a := range languages {
		if a.Name == name {
			return a, true
		}
	}
	return lang.Adapter{}, false
}

// pickAdapter chooses the adapter: an explicit --lang value when set (validated
// against the registry), else auto-detection walking up from startDir.
func pickAdapter(startDir, langFlag string) (lang.Adapter, error) {
	if langFlag != "" {
		a, ok := adapterByName(langFlag)
		if !ok {
			return lang.Adapter{}, unknownLangError(langFlag)
		}
		return a, nil
	}
	a, _, err := detectLanguage(startDir)
	return a, err
}

// detectLanguage walks up from startDir and returns the adapter whose marker
// sits in the nearest directory, plus that directory. Two adapters matching the
// same directory is an ambiguity (reported, never guessed); no marker anywhere
// is an error naming every language's markers.
func detectLanguage(startDir string) (lang.Adapter, string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return lang.Adapter{}, "", err
	}
	for d := abs; ; {
		var matched []lang.Adapter
		for _, a := range languages {
			if hasAnyMarker(d, a.Markers) {
				matched = append(matched, a)
			}
		}
		if len(matched) > 1 {
			return lang.Adapter{}, "", ambiguityError(d, matched)
		}
		if len(matched) == 1 {
			return matched[0], d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return lang.Adapter{}, "", noProjectError(abs)
}

// resolveProject picks the adapter (honoring --lang, then the config's optional
// `lang:` key, else auto-detect), resolves the project root, and locates the
// sibling depdog.yaml. It is the discovery path for check/config/tui when no
// explicit --config is given.
//
// The `lang:` key resolves the shared-marker ambiguity (e.g. a Java/Kotlin
// project rooted only on build.gradle) without --lang on every invocation: the
// nearest depdog.yaml walking up from startDir is peeked for its `lang:` value,
// which — when set and no --lang overrides it — pins the adapter before
// auto-detection could error on the ambiguity.
func resolveProject(startDir, langFlag string) (a lang.Adapter, root, cfgPath string, err error) {
	effLang := langFlag
	if effLang == "" {
		effLang = nearestConfigLang(startDir)
	}
	if a, err = pickAdapter(startDir, effLang); err != nil {
		return lang.Adapter{}, "", "", withUnitHint(startDir, err)
	}
	if root, err = adapterRoot(a, startDir); err != nil {
		return lang.Adapter{}, "", "", withUnitHint(startDir, err)
	}
	cfgPath = filepath.Join(root, config.DefaultName)
	if !fileExists(cfgPath) {
		return lang.Adapter{}, "", "", withUnitHint(startDir, fmt.Errorf("%w: no %s in %s — run `depdog init` to create one",
			errResolution, config.DefaultName, root))
	}
	return a, root, cfgPath, nil
}

// withUnitHint appends a one-line hint to a single-project resolution failure
// when depdog.yaml units exist below startDir, so the single-unit commands
// (explain/graph/config/baseline/tui) teach `--all` at the moment of confusion.
// It only augments errResolution errors — the two "no project" shapes the
// single-project path can hit — leaving ambiguity and other errors untouched,
// and never changes the exit code (still 2). The hint is a no-op when discovery
// finds nothing, and for `check` the errResolution fallback fans out over the
// same units before this error is ever surfaced, so the hint is seen only by
// the commands that stay single-unit.
func withUnitHint(startDir string, err error) error {
	if !errors.Is(err, errResolution) {
		return err
	}
	units, _, derr := config.DiscoverUnits(startDir, registryMarkers())
	if derr != nil || len(units) == 0 {
		return err
	}
	return fmt.Errorf("%w\n\nfound %d %s under this tree — cd into a unit (%s) or run `depdog check --all`",
		err, len(units), config.DefaultName, unitHintDirs(units))
}

// unitHintDirs renders a couple of discovered unit directories for the hint,
// eliding the rest with an ellipsis so the line stays short regardless of how
// many units the tree holds.
func unitHintDirs(units []config.Unit) string {
	const show = 2
	names := make([]string, 0, show+1)
	for i, u := range units {
		if i >= show {
			names = append(names, "…")
			break
		}
		rel := u.Rel
		if rel == "." {
			rel = "the repo root"
		} else {
			rel += "/"
		}
		names = append(names, rel)
	}
	return strings.Join(names, ", ")
}

// nearestConfigLang walks up from startDir to the nearest depdog.yaml and
// returns its `lang:` value (or "" if none is found or none is set). The value
// is not validated here — pickAdapter rejects an unknown name.
func nearestConfigLang(startDir string) string {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for d := abs; ; {
		cfg := filepath.Join(d, config.DefaultName)
		if fileExists(cfg) {
			return config.PeekLang(cfg)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// adapterForUnit resolves the adapter for a discovered unit rooted at dir,
// following the D7 order: an explicit `lang:` config value (validated against
// the registry), else auto-detection walking up from dir. Polyglot fan-out
// never forwards a --lang flag (--lang with --all is a usage error), so the
// resolution is exactly the config key or auto-detect.
func adapterForUnit(dir, cfgLang string) (lang.Adapter, error) {
	if cfgLang != "" {
		a, ok := adapterByName(cfgLang)
		if !ok {
			return lang.Adapter{}, fmt.Errorf("%s: %w", dir, unknownLangError(cfgLang))
		}
		return a, nil
	}
	a, _, err := detectLanguage(dir)
	if err != nil {
		// Under --all, --lang is a usage error, so the single-project
		// "pass --lang" guidance would point at a forbidden action. Redirect
		// an ambiguous unit to the `lang:` config key (D7) instead.
		var amb *ambiguousLangError
		if errors.As(err, &amb) {
			return lang.Adapter{}, fmt.Errorf("ambiguous project language: %s matches %s — add `lang: <one of: %s>` to this unit's depdog.yaml (--lang is not available under --all)",
				amb.dir, strings.Join(amb.names, " and "), strings.Join(amb.names, ", "))
		}
		return lang.Adapter{}, err
	}
	return a, nil
}

// registryMarkers returns the distinct marker file names across every adapter,
// for the discovery walk (DiscoverUnits takes markers as data so internal/config
// need not depend on this registry). Order is unspecified; DiscoverUnits builds
// a set.
func registryMarkers() []string {
	seen := make(map[string]bool)
	var out []string
	for _, a := range languages {
		for _, m := range a.Markers {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// adapterRoot resolves an adapter's project root: its custom Root when set (e.g.
// Go's workspace refusal), otherwise the nearest ancestor holding a marker.
func adapterRoot(a lang.Adapter, startDir string) (string, error) {
	if a.Root != nil {
		return a.Root(startDir)
	}
	return rootByMarkers(startDir, a.Markers)
}

// rootByMarkers walks up from startDir and returns the nearest directory holding
// a marker, trying markers in priority order — an earlier marker found anywhere
// beats a later one found nearer (e.g. tsconfig.json over package.json).
func rootByMarkers(startDir string, markers []string) (string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	found := make([]string, len(markers)) // nearest dir seen for each marker
	for d := abs; ; {
		for i, m := range markers {
			if found[i] == "" && fileExists(filepath.Join(d, m)) {
				found[i] = d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	for i := range markers {
		if found[i] != "" {
			return found[i], nil
		}
	}
	return "", fmt.Errorf("no %s found from %s upward", strings.Join(markers, " or "), abs)
}

func hasAnyMarker(dir string, markers []string) bool {
	for _, m := range markers {
		if fileExists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func unknownLangError(name string) error {
	return fmt.Errorf("unknown --lang %q (one of: %s)", name, strings.Join(languageNames(), ", "))
}

// ambiguousLangError reports a directory whose markers match more than one
// adapter. It carries the matched names so callers can tailor the remediation:
// single-project runs suggest --lang, whereas a --all fan-out unit suggests the
// `lang:` config key (since --lang is a usage error under --all).
type ambiguousLangError struct {
	dir   string
	names []string
}

func (e *ambiguousLangError) Error() string {
	return fmt.Sprintf("ambiguous project language: %s matches %s — pass --lang (one of: %s) to choose the adapter",
		e.dir, strings.Join(e.names, " and "), strings.Join(languageNames(), ", "))
}

func ambiguityError(dir string, matched []lang.Adapter) error {
	names := make([]string, len(matched))
	for i, a := range matched {
		names[i] = a.Name
	}
	return &ambiguousLangError{dir: dir, names: names}
}

func noProjectError(startDir string) error {
	kinds := make([]string, len(languages))
	for i, a := range languages {
		kinds[i] = fmt.Sprintf("%s (%s)", a.Name, strings.Join(a.Markers, "/"))
	}
	return fmt.Errorf("%w: no project root found from %s upward — depdog runs inside one of: %s",
		errResolution, startDir, strings.Join(kinds, ", "))
}
