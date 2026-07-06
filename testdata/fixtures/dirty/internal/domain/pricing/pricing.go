// Package pricing breaks the rules on purpose: domain may import std only.
package pricing

import (
	_ "example.test/dirty/internal/repository"
	_ "example.test/extlib"
)

func Total(cents int) int { return cents }
