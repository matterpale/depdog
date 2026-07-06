package repository

import (
	"fmt"

	"example.test/clean/internal/domain/order"
	"example.test/extlib"
)

func Store(o order.Order) string {
	return fmt.Sprintf("stored %s in slot %d", o.ID, extlib.Answer())
}
