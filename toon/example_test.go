package toon_test

import (
	"fmt"

	"go.klarlabs.de/axi/toon"
)

func ExampleEncode() {
	out, _ := toon.Encode(map[string]any{
		"name":    "widget",
		"count":   3,
		"enabled": true,
	})
	fmt.Println(out)
	// Output:
	// count: 3
	// enabled: true
	// name: widget
}

func ExampleEncode_tabularArray() {
	issues := []any{
		map[string]any{"number": 42, "title": "Fix login bug", "state": "open"},
		map[string]any{"number": 43, "title": "Add dark mode", "state": "open"},
	}
	out, _ := toon.Encode(map[string]any{"issues": issues})
	fmt.Println(out)
	// Output:
	// issues[2]{number,state,title}:
	//   42,open,Fix login bug
	//   43,open,Add dark mode
}
