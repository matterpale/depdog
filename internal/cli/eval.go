package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/core"
	"github.com/matterpale/depdog/internal/lang"
	"github.com/matterpale/depdog/internal/lang/golang"
)

// evaluateModule resolves the config (at configPath, or discovered next to
// go.mod), loads the module's import graph and evaluates the rules. It returns
// the result and the resolved config path, so callers can locate sibling files
// such as the baseline. Shared by `check` and `baseline`.
func evaluateModule(cmd *cobra.Command, configPath string, args []string) (*core.Result, string, error) {
	var (
		cfgPath string
		root    string
		err     error
	)
	if configPath != "" {
		if cfgPath, err = filepath.Abs(configPath); err != nil {
			return nil, "", err
		}
		root = filepath.Dir(cfgPath)
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, "", err
		}
		if cfgPath, root, err = config.Find(cwd); err != nil {
			return nil, "", err
		}
	}

	rs, err := config.Load(cfgPath)
	if err != nil {
		return nil, "", err
	}

	var loader lang.Loader = &golang.Loader{Dir: root}
	graph, err := loader.Load(cmd.Context(), args...)
	if err != nil {
		return nil, "", err
	}

	res, err := core.Evaluate(graph, rs)
	if err != nil {
		return nil, "", err
	}
	return res, cfgPath, nil
}
