package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
)

// evaluation bundles a check run's outputs and the inputs commands may still
// need: the config path (to locate the sibling baseline) and the graph/ruleset
// (for the TUI's package navigation).
type evaluation struct {
	Result     *core.Result
	Graph      *core.Graph
	Rules      *core.RuleSet
	ConfigPath string
}

// evaluateModule resolves the config (at configPath, or discovered by walking up
// to a language's project root), loads the project's import graph via the
// selected adapter and evaluates the rules. Shared by check, baseline and the
// TUI. The adapter is chosen from the languages registry — either an explicit
// --lang or auto-detection.
func evaluateModule(cmd *cobra.Command, configPath string, args []string) (*evaluation, error) {
	language, err := languageFlag(cmd)
	if err != nil {
		return nil, err
	}

	var (
		adapter lang.Adapter
		root    string
		cfgPath string
	)
	if configPath != "" {
		// An explicit --config skips discovery. The adapter follows the D7
		// order: --lang, else the config's `lang:` key, else auto-detect from
		// the config's directory.
		if cfgPath, err = filepath.Abs(configPath); err != nil {
			return nil, err
		}
		root = filepath.Dir(cfgPath)
		effLang := language
		if effLang == "" {
			effLang = config.PeekLang(cfgPath)
		}
		if adapter, err = pickAdapter(root, effLang); err != nil {
			return nil, err
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if adapter, root, cfgPath, err = resolveProject(cwd, language); err != nil {
			return nil, err
		}
	}

	return evaluateAt(cmd, adapter, root, cfgPath, args)
}

// evaluateDiscovered resolves and evaluates the single module discovered by
// walking up from startDir (adapter auto-detected unless --lang pins one). It
// is the working-directory-independent counterpart of evaluateModule's
// discovery branch, letting the LSP resolve a project from a triggering file's
// directory rather than the process cwd.
func evaluateDiscovered(cmd *cobra.Command, startDir string) (*evaluation, error) {
	language, err := languageFlag(cmd)
	if err != nil {
		return nil, err
	}
	adapter, root, cfgPath, err := resolveProject(startDir, language)
	if err != nil {
		return nil, err
	}
	return evaluateAt(cmd, adapter, root, cfgPath, nil)
}

// evaluateAt loads and evaluates one already-resolved module: its config at
// cfgPath, its import graph via the adapter rooted at root. It is the shared
// core of single-module discovery (evaluateModule) and workspace fan-out.
func evaluateAt(cmd *cobra.Command, adapter lang.Adapter, root, cfgPath string, args []string) (*evaluation, error) {
	rs, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	return evaluateWith(cmd, adapter, root, cfgPath, rs, args)
}

// evaluateWith evaluates one module whose config is already loaded — the shared
// tail of evaluateAt. Polyglot fan-out loads each unit's config first (to read
// its `lang:` key for adapter selection) and hands the result here so it is not
// re-parsed.
func evaluateWith(cmd *cobra.Command, adapter lang.Adapter, root, cfgPath string, rs *core.RuleSet, args []string) (*evaluation, error) {
	graph, err := adapter.New(root).Load(cmd.Context(), args...)
	if err != nil {
		return nil, err
	}
	// Load-time advisories (e.g. the Go adapter degrading to approximate
	// classification) go to stderr so --format json/github/sarif stdout stays clean.
	for _, w := range graph.LoadWarnings {
		fmt.Fprintln(cmd.ErrOrStderr(), "depdog: warning: "+w)
	}

	res, err := core.Evaluate(graph, rs)
	if err != nil {
		return nil, err
	}
	res.Degraded = len(graph.LoadWarnings) > 0
	return &evaluation{Result: res, Graph: graph, Rules: rs, ConfigPath: cfgPath}, nil
}

// languageFlag reads and validates the persistent --lang flag against the
// languages registry. An empty value means auto-detect; anything not registered
// is a usage error (exit 2, same style as check.go's --format/--color checks).
func languageFlag(cmd *cobra.Command) (string, error) {
	language, err := cmd.Flags().GetString("lang")
	if err != nil {
		return "", nil // command without the persistent flag: auto-detect
	}
	if language == "" {
		return "", nil
	}
	if _, ok := adapterByName(language); !ok {
		return "", unknownLangError(language)
	}
	return language, nil
}
