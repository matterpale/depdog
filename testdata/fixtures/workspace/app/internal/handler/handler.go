package handler

import (
	"fmt"

	"example.test/libs/store"
)

// Handle reaches across the workspace into example.test/libs — a cross-module
// import.
func Handle() { fmt.Println(store.Value()) }
