package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matterpale/depdog/internal/config"
	"github.com/matterpale/depdog/internal/report"
)

func trendCmd() *cobra.Command {
	var (
		configPath string
		since      string
		format     string
		maxCommits int
	)
	cmd := &cobra.Command{
		Use:   "trend [packages]",
		Short: "Show how the architecture's health moved over git history",
		Long: `trend samples commits from a git ref to HEAD, computes the repo-level
architecture metrics (components, cross-component edges, boundary crossings,
cycles, worst instability) at each, and reports the series plus the first→last
delta — so drift shows up before it becomes violations.

Each sampled commit is materialized in a throwaway git worktree and scanned with
the CURRENT depdog.yaml (like diff), so the trend reflects how the CODE moved,
not config changes. It samples up to --max commits (default 10) to bound the
work. Informational (always exits 0 on success).

Exit codes: 0 written, 2 usage, git or scan error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrend(cmd, args, configPath, since, format, maxCommits)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to depdog.yaml (default: found next to go.mod)")
	cmd.Flags().StringVar(&since, "since", "", "git ref to trend from (required)")
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text or json")
	cmd.Flags().IntVar(&maxCommits, "max", 10, "maximum commits to sample across the range")
	return cmd
}

func runTrend(cmd *cobra.Command, args []string, configPath, since, format string, maxCommits int) error {
	if since == "" {
		return fmt.Errorf("--since <ref> is required (the git ref to trend from)")
	}
	switch format {
	case "text", "json":
	default:
		return fmt.Errorf("unknown --format %q (choose text or json)", format)
	}
	if maxCommits < 2 {
		return fmt.Errorf("--max must be at least 2 (got %d)", maxCommits)
	}

	adapter, root, cfgPath, err := resolveModule(cmd, configPath)
	if err != nil {
		return err
	}
	rs, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	commits, err := trendCommits(root, since, maxCommits)
	if err != nil {
		return err
	}

	points := make([]report.TrendPoint, 0, len(commits))
	for _, c := range commits {
		// beforeGraph materializes the commit in a throwaway worktree, scans it
		// with the current adapter + RuleSet, and always cleans up.
		ev, err := beforeGraph(cmd, adapter, root, cfgPath, rs, c.sha, args)
		if err != nil {
			return err
		}
		m, err := report.Metrics(ev.Graph, rs, len(ev.Result.Cycles))
		if err != nil {
			return err
		}
		points = append(points, report.TrendPointFrom(c.short, m))
	}

	out := cmd.OutOrStdout()
	if format == "json" {
		return report.TrendJSON(out, points, since)
	}
	return report.Trend(out, points, since)
}

type trendCommit struct{ sha, short string }

// trendCommits resolves the range `since..HEAD` into a sampled list of commits,
// oldest first, always including `since` (the baseline) and HEAD. At most
// maxCommits points are returned (evenly spaced) so the scan count is bounded.
func trendCommits(root, since string, maxCommits int) ([]trendCommit, error) {
	sinceSha, err := runGit(root, "rev-parse", since)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve --since %q (unknown ref?) — try a commit SHA, tag or branch: %w", since, err)
	}
	sinceSha = strings.TrimSpace(sinceSha)

	// Commits reachable from HEAD but not `since`, oldest first; prepend `since`
	// itself as the baseline point.
	out, err := runGit(root, "rev-list", "--reverse", since+"..HEAD")
	if err != nil {
		return nil, err
	}
	shas := []string{sinceSha}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			shas = append(shas, s)
		}
	}
	shas = sampleEvenly(shas, maxCommits)

	commits := make([]trendCommit, 0, len(shas))
	for _, sha := range shas {
		short := sha
		if len(short) > 7 {
			short = short[:7]
		}
		commits = append(commits, trendCommit{sha: sha, short: short})
	}
	return commits, nil
}

// sampleEvenly returns at most maxCommits evenly-spaced elements of all, always
// including the first and last. Rounding collisions are de-duplicated, so a
// short range returns fewer than maxCommits rather than repeats. Requires
// maxCommits >= 2 (enforced by the caller).
func sampleEvenly(all []string, maxCommits int) []string {
	if len(all) <= maxCommits {
		return all
	}
	out := make([]string, 0, maxCommits)
	seen := make(map[string]bool, maxCommits)
	for i := 0; i < maxCommits; i++ {
		idx := i * (len(all) - 1) / (maxCommits - 1)
		if s := all[idx]; !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
