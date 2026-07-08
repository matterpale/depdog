package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
	"github.com/matterpale/depdog/internal/lang/golang"
	"github.com/matterpale/depdog/internal/lang/typescript"
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

// evaluateModule resolves the config (at configPath, or discovered next to
// go.mod), loads the module's import graph and evaluates the rules. Shared by
// check, baseline and the TUI.
func evaluateModule(cmd *cobra.Command, configPath string, args []string) (*evaluation, error) {
	language, err := languageFlag(cmd)
	if err != nil {
		return nil, err
	}

	var (
		cfgPath  string
		root     string
		resolved string
	)
	if configPath != "" {
		// An explicit --config skips discovery; --lang (or, absent it,
		// auto-detect from the config's directory) still picks the adapter.
		if cfgPath, err = filepath.Abs(configPath); err != nil {
			return nil, err
		}
		root = filepath.Dir(cfgPath)
		if resolved, err = resolveLanguage(language, root); err != nil {
			return nil, err
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if cfgPath, root, resolved, err = config.FindWithLanguage(cwd, language); err != nil {
			return nil, err
		}
	}

	rs, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	loader, err := loaderFor(resolved, root)
	if err != nil {
		return nil, err
	}
	graph, err := loader.Load(cmd.Context(), args...)
	if err != nil {
		return nil, err
	}

	res, err := core.Evaluate(graph, rs)
	if err != nil {
		return nil, err
	}
	return &evaluation{Result: res, Graph: graph, Rules: rs, ConfigPath: cfgPath}, nil
}

// languageFlag reads and validates the persistent --lang flag. An empty value
// means auto-detect; anything other than go/ts is a usage error (exit 2, same
// style as check.go's --format/--color validation).
func languageFlag(cmd *cobra.Command) (string, error) {
	language, err := cmd.Flags().GetString("lang")
	if err != nil {
		return "", nil // command without the persistent flag: auto-detect
	}
	switch language {
	case "", "go", "ts":
		return language, nil
	default:
		return "", fmt.Errorf("unknown --lang %q (go or ts)", language)
	}
}

// resolveLanguage picks the adapter when the config path was given explicitly
// (so discovery is skipped): honor --lang when set, else auto-detect from the
// config's directory.
func resolveLanguage(language, root string) (string, error) {
	if language == "go" || language == "ts" {
		return language, nil
	}
	resolved, _, err := config.DetectLanguage(root)
	return resolved, err
}

// loaderFor constructs the adapter for the resolved language.
func loaderFor(language, root string) (lang.Loader, error) {
	switch language {
	case "go":
		return &golang.Loader{Dir: root}, nil
	case "ts":
		return &typescript.Loader{Dir: root}, nil
	default:
		return nil, fmt.Errorf("unknown language %q (go or ts)", language)
	}
}
