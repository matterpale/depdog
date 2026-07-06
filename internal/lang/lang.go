// Package lang defines the contract between depdog's core and
// language-specific analyzers. Adding support for a new language means
// implementing Loader in a sibling package; core and the reporters stay
// untouched.
package lang

import (
	"context"

	"github.com/matterpale/depdog/internal/core"
)

// Loader produces the import graph of the project it was configured with.
type Loader interface {
	// Load builds the graph for the given package patterns; with no
	// patterns the whole project is loaded.
	Load(ctx context.Context, patterns ...string) (*core.Graph, error)
}
