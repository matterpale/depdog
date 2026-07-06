package handler

import (
	"fmt"

	"example.test/clean/internal/domain/order"
)

func Describe(o order.Order) string {
	return fmt.Sprintf("order %s", o.ID)
}
