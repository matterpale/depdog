package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/matterpale/depdog/internal/cli"
)

func main() {
	// fang renders --version itself; without this it ignores cobra's Version
	// field and reports "unknown (built from source)".
	if err := fang.Execute(context.Background(), cli.Root(), fang.WithVersion(cli.Version)); err != nil {
		// Violations exit 1 inside check; anything surfacing here is a
		// configuration or usage error.
		os.Exit(2)
	}
}
