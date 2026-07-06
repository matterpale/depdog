package pricing

import _ "example.test/dirty/internal/repository"

func Discount(cents int) int { return cents / 2 }
