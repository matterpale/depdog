package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/matterpale/depdog/internal/cli"
)

func main() {
	if err := fang.Execute(context.Background(), cli.Root()); err != nil {
		// Violations exit 1 inside check; anything surfacing here is a
		// configuration or usage error.
		os.Exit(2)
	}
}
