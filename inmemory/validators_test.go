package inmemory_test

import (
	"strings"
	"testing"

	"go.klarlabs.de/axi/domain"
	"go.klarlabs.de/axi/inmemory"
)

func TestContractValidator_Validate(t *testing.T) {
	v := inmemory.NewContractValidator()

	tests := []struct {
		name      string
		contract  domain.Contract
		input     any
		wantErr   bool
		errSubstr string // substring expected in error message
	}{
		// --- empty contract / nil input ---
		{
			name:     "empty contract accepts any input",
			contract: domain.EmptyContract(),
			input:    "anything",
			wantErr:  false,
		},

		// --- required-field presence ---
		{
			name: "required field present with correct type passes",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "name", Type: "string", Required: true},
			}),
			input:   map[string]any{"name": "alice"},
			wantErr: false,
		},
		{
			name: "required field missing fails",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "name", Type: "string", Required: true},
			}),
			input:     map[string]any{"other": "value"},
			wantErr:   true,
			errSubstr: "required field",
		},

		// --- type checking on required fields ---
		{
			name: "required field present with wrong type fails",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "count", Type: "number", Required: true},
			}),
			input:     map[string]any{"count": "not-a-number"},
			wantErr:   true,
			errSubstr: "expected type \"number\"",
		},

		// --- type checking on optional fields ---
		{
			name: "optional field present with wrong type fails",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "tag", Type: "string", Required: false},
			}),
			input:     map[string]any{"tag": 123},
			wantErr:   true,
			errSubstr: "expected type \"string\"",
		},
		{
			name: "optional field absent passes",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "tag", Type: "string", Required: false},
			}),
			input:   map[string]any{},
			wantErr: false,
		},

		// --- empty Type means any value passes (backward compat) ---
		{
			name: "field with empty Type accepts any value",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "data", Type: "", Required: true},
			}),
			input:   map[string]any{"data": 42},
			wantErr: false,
		},
		{
			name: "field with empty Type accepts string",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "data", Type: "", Required: true},
			}),
			input:   map[string]any{"data": "hello"},
			wantErr: false,
		},

		// --- string type ---
		{
			name: "string type accepts string",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "s", Type: "string", Required: true},
			}),
			input:   map[string]any{"s": "hello"},
			wantErr: false,
		},
		{
			name: "string type rejects int",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "s", Type: "string", Required: true},
			}),
			input:     map[string]any{"s": 42},
			wantErr:   true,
			errSubstr: "expected type \"string\"",
		},

		// --- number type ---
		{
			name: "number type accepts float64",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "n", Type: "number", Required: true},
			}),
			input:   map[string]any{"n": float64(3.14)},
			wantErr: false,
		},
		{
			name: "number type accepts int",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "n", Type: "number", Required: true},
			}),
			input:   map[string]any{"n": 42},
			wantErr: false,
		},
		{
			name: "number type accepts int64",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "n", Type: "number", Required: true},
			}),
			input:   map[string]any{"n": int64(100)},
			wantErr: false,
		},
		{
			name: "number type accepts int32",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "n", Type: "number", Required: true},
			}),
			input:   map[string]any{"n": int32(10)},
			wantErr: false,
		},
		{
			name: "number type accepts float32",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "n", Type: "number", Required: true},
			}),
			input:   map[string]any{"n": float32(1.5)},
			wantErr: false,
		},
		{
			name: "number type rejects string",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "n", Type: "number", Required: true},
			}),
			input:     map[string]any{"n": "forty-two"},
			wantErr:   true,
			errSubstr: "expected type \"number\"",
		},

		// --- boolean type ---
		{
			name: "boolean type accepts bool",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "b", Type: "boolean", Required: true},
			}),
			input:   map[string]any{"b": true},
			wantErr: false,
		},
		{
			name: "boolean type rejects string",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "b", Type: "boolean", Required: true},
			}),
			input:     map[string]any{"b": "true"},
			wantErr:   true,
			errSubstr: "expected type \"boolean\"",
		},

		// --- object type ---
		{
			name: "object type accepts map[string]any",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "o", Type: "object", Required: true},
			}),
			input:   map[string]any{"o": map[string]any{"key": "value"}},
			wantErr: false,
		},
		{
			name: "object type rejects string",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "o", Type: "object", Required: true},
			}),
			input:     map[string]any{"o": "not-an-object"},
			wantErr:   true,
			errSubstr: "expected type \"object\"",
		},

		// --- array type ---
		{
			name: "array type accepts []any",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "a", Type: "array", Required: true},
			}),
			input:   map[string]any{"a": []any{1, 2, 3}},
			wantErr: false,
		},
		{
			name: "array type accepts []string (typed slice)",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "a", Type: "array", Required: true},
			}),
			input:   map[string]any{"a": []string{"a", "b"}},
			wantErr: false,
		},
		{
			name: "array type accepts []int (typed slice)",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "a", Type: "array", Required: true},
			}),
			input:   map[string]any{"a": []int{1, 2, 3}},
			wantErr: false,
		},
		{
			name: "array type rejects string",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "a", Type: "array", Required: true},
			}),
			input:     map[string]any{"a": "not-an-array"},
			wantErr:   true,
			errSubstr: "expected type \"array\"",
		},

		// --- non-map input with fields ---
		{
			name: "non-map input fails when contract has fields",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "x", Type: "string", Required: true},
			}),
			input:     "not-a-map",
			wantErr:   true,
			errSubstr: "input must be a map[string]any",
		},

		// --- multiple fields ---
		{
			name: "multiple fields all valid",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "name", Type: "string", Required: true},
				{Name: "age", Type: "number", Required: true},
				{Name: "active", Type: "boolean", Required: false},
			}),
			input:   map[string]any{"name": "alice", "age": 30, "active": true},
			wantErr: false,
		},
		{
			name: "multiple fields one wrong type",
			contract: domain.NewContract([]domain.ContractField{
				{Name: "name", Type: "string", Required: true},
				{Name: "age", Type: "number", Required: true},
			}),
			input:     map[string]any{"name": "alice", "age": "thirty"},
			wantErr:   true,
			errSubstr: "expected type \"number\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.contract, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error containing %q, got %q", tt.errSubstr, err.Error())
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
