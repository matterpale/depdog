package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/matterpale/depdog/internal/core"
)

// BaselineName is the baseline file expected next to depdog.yaml.
const BaselineName = "depdog.baseline.yaml"

type baselineFile struct {
	Version    int                 `yaml:"version"`
	Violations []baselineEntryYAML `yaml:"violations"`
}

type baselineEntryYAML struct {
	From   string `yaml:"from"`
	Import string `yaml:"import"`
}

// WriteBaseline serializes b as depdog.baseline.yaml: a commented, sorted,
// diff-friendly list of tolerated violations.
func WriteBaseline(w io.Writer, b *core.Baseline) error {
	var sb strings.Builder
	sb.WriteString("# depdog baseline — tolerated violations; shrink this over time.\n")
	sb.WriteString("# Written by `depdog baseline`; `depdog check --fail-on new` ignores these.\n")
	sb.WriteString("version: 1\n")
	if len(b.Entries) == 0 {
		sb.WriteString("violations: []\n")
	} else {
		sb.WriteString("violations:\n")
		for _, e := range b.Entries {
			fmt.Fprintf(&sb, "  - from:   %s\n    import: %s\n", e.FromPackage, e.Import)
		}
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

// LoadBaseline reads and validates a baseline file.
func LoadBaseline(path string) (*core.Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b, err := ParseBaseline(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return b, nil
}

// LoadBaselineOrEmpty behaves like LoadBaseline but treats a missing file as an
// empty baseline, so `check --fail-on new` is safe to enable before any
// baseline is recorded.
func LoadBaselineOrEmpty(path string) (*core.Baseline, error) {
	b, err := LoadBaseline(path)
	if errors.Is(err, os.ErrNotExist) {
		return &core.Baseline{}, nil
	}
	return b, err
}

// ParseBaseline compiles raw baseline YAML into a validated, sorted baseline.
func ParseBaseline(data []byte) (*core.Baseline, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f baselineFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return &core.Baseline{}, nil
		}
		return nil, err
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("unsupported baseline version %d (this depdog understands version 1)", f.Version)
	}
	b := &core.Baseline{}
	for i, e := range f.Violations {
		if e.From == "" || e.Import == "" {
			return nil, fmt.Errorf("baseline entry %d needs both `from` and `import`", i+1)
		}
		b.Entries = append(b.Entries, core.BaselineEntry{FromPackage: e.From, Import: e.Import})
	}
	b.Sort()
	return b, nil
}
