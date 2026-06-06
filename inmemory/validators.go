package inmemory

import (
	"fmt"
	"reflect"

	"go.klarlabs.de/axi/domain"
)

// Compile-time interface satisfaction check.
var _ domain.ContractValidator = (*ContractValidator)(nil)

// ContractValidator validates input against a contract using field-based checks.
type ContractValidator struct{}

func NewContractValidator() *ContractValidator {
	return &ContractValidator{}
}

func (v *ContractValidator) Validate(contract domain.Contract, input any) error {
	if contract.IsEmpty() {
		return nil
	}

	inputMap, ok := input.(map[string]any)
	if !ok {
		return fmt.Errorf("input must be a map[string]any when contract has fields, got %T", input)
	}

	for _, field := range contract.Fields {
		val, exists := inputMap[field.Name]
		if !exists {
			if field.Required {
				return fmt.Errorf("required field %q is missing", field.Name)
			}
			continue
		}
		if field.Type != "" {
			if err := checkType(field.Name, field.Type, val); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkType validates that val matches the declared contract type.
func checkType(fieldName, declaredType string, val any) error {
	switch declaredType {
	case "string":
		if _, ok := val.(string); !ok {
			return typeMismatch(fieldName, declaredType, val)
		}
	case "number":
		if !isNumeric(val) {
			return typeMismatch(fieldName, declaredType, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return typeMismatch(fieldName, declaredType, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return typeMismatch(fieldName, declaredType, val)
		}
	case "array":
		if !isSlice(val) {
			return typeMismatch(fieldName, declaredType, val)
		}
	}
	return nil
}

func isNumeric(val any) bool {
	switch val.(type) {
	case float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func isSlice(val any) bool {
	return reflect.TypeOf(val).Kind() == reflect.Slice
}

func typeMismatch(fieldName, declaredType string, val any) error {
	return fmt.Errorf("field %q: expected type %q, got %T", fieldName, declaredType, val)
}
