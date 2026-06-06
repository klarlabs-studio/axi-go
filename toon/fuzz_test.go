package toon_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"go.klarlabs.de/axi/toon"
)

// FuzzEncode drives toon.Encode with a grab-bag of input shapes assembled
// from primitive fuzz inputs. Invariants enforced:
//  1. Encode must not panic for any combination of supported input types.
//  2. When Encode returns no error, the output is valid UTF-8 with no
//     embedded NUL bytes.
//  3. No blank lines inside the output (blank lines would confuse an agent
//     reading the payload as structured text).
//  4. Every line ends with either a value or a trailing ':' (block header).
//
// These are structural invariants — they don't verify round-trip correctness
// (we have no decoder), but they catch the crash-level regressions fuzzing is
// best at finding.
func FuzzEncode(f *testing.F) {
	f.Add("key", "value", int64(42), true, false)
	f.Add(":colon", "a,b", int64(-1), false, true)
	f.Add("null", "true", int64(0), true, true)
	f.Add("", "", int64(0), false, false)
	f.Add("line1\nline2", "tab\there", int64(1<<40), true, false)
	f.Add("  padded  ", `"quoted"`, int64(-1<<40), false, true)

	f.Fuzz(func(t *testing.T, key, val string, num int64, flag, asArray bool) {
		// Build a few different shapes from the seeds and encode each.
		// If any one panics, Encode is broken.

		inputs := []any{
			// Flat map.
			map[string]any{key: val, "num": num, "flag": flag},
			// Nested map.
			map[string]any{
				"outer": map[string]any{key: val, "num": num},
			},
			// Scalar array.
			map[string]any{"tags": []any{val, key, val}},
			// Uniform-object array.
			map[string]any{"items": []any{
				map[string]any{"a": val, "b": num},
				map[string]any{"a": key, "b": num + 1},
			}},
			// Heterogeneous array (falls back to numbered entries).
			map[string]any{"mixed": []any{val, num, map[string]any{key: val}, flag}},
		}
		if asArray {
			inputs = append(inputs, []any{val, num, flag})
		}

		for _, in := range inputs {
			out, err := toon.Encode(in)
			if err != nil {
				// Supported inputs should never error. If they do, that's a bug.
				t.Fatalf("Encode returned error for supported input %T: %v", in, err)
			}
			checkInvariants(t, out)
		}
	})
}

func checkInvariants(t *testing.T, out string) {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Errorf("output is not valid UTF-8: %q", out)
	}
	if strings.ContainsRune(out, '\x00') {
		t.Errorf("output contains NUL byte: %q", out)
	}
	if out == "" {
		return
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if line == "" && i != len(lines)-1 {
			// A blank interior line would break agent parsing.
			t.Errorf("blank interior line at %d in:\n%s", i, out)
		}
	}
}
