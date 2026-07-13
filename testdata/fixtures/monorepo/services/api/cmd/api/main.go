package main

import (
	"fmt"

	"example.test/api/internal/handler"
)

func main() {
	msg, err := handler.Handle(" o-1 ")
	if err != nil {
		panic(err)
	}
	fmt.Println(msg)
}
