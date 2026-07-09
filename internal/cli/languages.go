package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/lang"
	"github.com/matterpale/depdog/internal/lang/golang"
	"github.com/matterpale/depdog/internal/lang/java"
	"github.com/matterpale/depdog/internal/lang/python"
	"github.com/matterpale/depdog/internal/lang/ruby"
	"github.com/matterpale/depdog/internal/lang/rust"
	"github.com/matterpale/depdog/internal/lang/typescript"
)

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

// resolveProject picks the adapter (honoring --lang, else auto-detect), resolves
// the project root, and locates the sibling depdog.yaml. It is the discovery
// path for check/config/tui when no explicit --config is given.
func resolveProject(startDir, langFlag string) (a lang.Adapter, root, cfgPath string, err error) {
	if a, err = pickAdapter(startDir, langFlag); err != nil {
		return lang.Adapter{}, "", "", err
	}
	if root, err = adapterRoot(a, startDir); err != nil {
		return lang.Adapter{}, "", "", err
	}
	cfgPath = filepath.Join(root, config.DefaultName)
	if !fileExists(cfgPath) {
		return lang.Adapter{}, "", "", fmt.Errorf("no %s in %s — run `depdog init` to create one", config.DefaultName, root)
	}
	return a, root, cfgPath, nil
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

func ambiguityError(dir string, matched []lang.Adapter) error {
	names := make([]string, len(matched))
	for i, a := range matched {
		names[i] = a.Name
	}
	return fmt.Errorf("ambiguous project language: %s matches %s — pass --lang (one of: %s) to choose the adapter",
		dir, strings.Join(names, " and "), strings.Join(languageNames(), ", "))
}

func noProjectError(startDir string) error {
	kinds := make([]string, len(languages))
	for i, a := range languages {
		kinds[i] = fmt.Sprintf("%s (%s)", a.Name, strings.Join(a.Markers, "/"))
	}
	return fmt.Errorf("no project root found from %s upward — depdog runs inside one of: %s",
		startDir, strings.Join(kinds, ", "))
}
