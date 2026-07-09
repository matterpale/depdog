package cli

import (
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
		// An explicit --config skips discovery; --lang (or, absent it,
		// auto-detect from the config's directory) still picks the adapter.
		if cfgPath, err = filepath.Abs(configPath); err != nil {
			return nil, err
		}
		root = filepath.Dir(cfgPath)
		if adapter, err = pickAdapter(root, language); err != nil {
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

	rs, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	graph, err := adapter.New(root).Load(cmd.Context(), args...)
	if err != nil {
		return nil, err
	}

	res, err := core.Evaluate(graph, rs)
	if err != nil {
		return nil, err
	}
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
