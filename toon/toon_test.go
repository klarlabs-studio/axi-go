package toon_test

import (
	"strings"
	"testing"

	"go.klarlabs.de/axi/toon"
)

func TestEncode_Scalars(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"int", 42, "42"},
		{"negative int", -7, "-7"},
		{"int64", int64(1 << 40), "1099511627776"},
		{"float", 3.14, "3.14"},
		{"float integer", 2.0, "2"},
		{"simple string", "hello", "hello"},
		{"empty string", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toon.Encode(tt.in)
			if err != nil {
				t.Fatalf("Encode(%v) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("Encode(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEncode_StringQuoting(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello world", "hello world"},         // bare ok
		{"hello: world", `"hello: world"`},     // colon
		{"a,b", `"a,b"`},                       // comma
		{"line1\nline2", `"line1\nline2"`},     // newline
		{"null", `"null"`},                     // reserved
		{"true", `"true"`},                     // reserved
		{"42", `"42"`},                         // number-like
		{"1.5e3", `"1.5e3"`},                   // number-like
		{"  padded  ", `"  padded  "`},         // leading/trailing space
		{`with "quotes"`, `"with \"quotes\""`}, // embedded quote
		{`back\slash`, `"back\\slash"`},        // backslash
	}
	for _, tt := range tests {
		got, err := toon.Encode(tt.in)
		if err != nil {
			t.Fatalf("Encode(%q) error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("Encode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEncode_FlatMap(t *testing.T) {
	in := map[string]any{
		"name": "Alice",
		"age":  30,
	}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Keys are sorted.
	want := "age: 30\nname: Alice"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncode_NestedMap(t *testing.T) {
	in := map[string]any{
		"user": map[string]any{
			"name": "Alice",
			"age":  30,
		},
	}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := "user:\n  age: 30\n  name: Alice"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncode_ScalarArray(t *testing.T) {
	in := map[string]any{
		"tags": []any{"red", "green", "blue"},
	}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := "tags[3]:\n  red\n  green\n  blue"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncode_UniformObjectArray(t *testing.T) {
	in := map[string]any{
		"issues": []any{
			map[string]any{"number": 42, "title": "Fix login bug", "state": "open"},
			map[string]any{"number": 43, "title": "Add dark mode", "state": "open"},
		},
	}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Fields sorted alphabetically: number, state, title.
	want := strings.Join([]string{
		"issues[2]{number,state,title}:",
		"  42,open,Fix login bug",
		"  43,open,Add dark mode",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncode_HeterogeneousArray(t *testing.T) {
	in := map[string]any{
		"mixed": []any{
			"a",
			map[string]any{"k": "v"},
			42,
		},
	}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Falls back to numbered-entry form. Numeric index keys are quoted so
	// they can't be confused with numeric values by an agent reader.
	want := strings.Join([]string{
		`mixed[3]:`,
		`  "0": a`,
		`  "1":`,
		`    k: v`,
		`  "2": 42`,
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncode_EmptyMap(t *testing.T) {
	got, err := toon.Encode(map[string]any{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got != "" {
		t.Errorf("Encode(empty map) = %q, want empty", got)
	}
}

func TestEncode_TopLevelArray(t *testing.T) {
	in := []any{1, 2, 3}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := "[3]:\n  1\n  2\n  3"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEncode_UnsupportedType(t *testing.T) {
	type custom struct{ X int }
	_, err := toon.Encode(custom{X: 1})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

// TokenSavings verifies that TOON is at least 25% shorter than JSON for a
// uniform-array payload — the scenario axi.md cites as the 40% savings case.
func TestEncode_TokenSavings(t *testing.T) {
	payload := map[string]any{
		"issues": []any{
			map[string]any{"number": 42, "state": "open", "title": "Fix login bug"},
			map[string]any{"number": 43, "state": "open", "title": "Add dark mode"},
			map[string]any{"number": 44, "state": "closed", "title": "Remove v1 API"},
		},
	}
	out, err := toon.Encode(payload)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Rough JSON equivalent length — hand-computed to avoid pulling encoding/json.
	jsonLen := len(`{"issues":[{"number":42,"state":"open","title":"Fix login bug"},{"number":43,"state":"open","title":"Add dark mode"},{"number":44,"state":"closed","title":"Remove v1 API"}]}`)
	ratio := float64(len(out)) / float64(jsonLen)
	if ratio > 0.75 {
		t.Errorf("TOON %d / JSON %d = %.2f, expected < 0.75", len(out), jsonLen, ratio)
	}
}
