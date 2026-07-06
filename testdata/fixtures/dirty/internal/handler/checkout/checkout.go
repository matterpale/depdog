package checkout

import (
	"example.test/dirty/internal/domain/pricing"
	_ "example.test/dirty/internal/service"
)

func Checkout(cents int) int { return pricing.Total(cents) }
