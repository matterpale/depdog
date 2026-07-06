package main

import (
	"fmt"

	_ "example.test/dirty/internal/domain/pricing"
	_ "example.test/dirty/internal/handler/checkout"
	_ "example.test/dirty/internal/repository"
	_ "example.test/dirty/internal/service"
)

func main() { fmt.Println("dirty fixture") }
