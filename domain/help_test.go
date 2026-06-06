package domain_test

import (
	"strings"
	"testing"

	"go.klarlabs.de/axi/domain"
)

func TestActionDefinition_Help(t *testing.T) {
	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "string.upper"})
	input := domain.NewContract([]domain.ContractField{
		{Name: "name", Type: "string", Description: "Person to greet", Required: true, Example: "world"},
	})
	output := domain.NewContract([]domain.ContractField{
		{Name: "message", Type: "string", Description: "The greeting", Required: true},
	})
	a, err := domain.NewActionDefinition(
		"greet", "Greet someone by name",
		input, output, reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	if err != nil {
		t.Fatalf("NewActionDefinition: %v", err)
	}

	help := a.Help()
	for _, want := range []string{
		"greet — Greet someone by name",
		"Effect: none",
		"Idempotent: true",
		"name  (string, required)",
		"Person to greet",
		"example: world",
		"message  (string, required)",
		"Requires capabilities:",
		"- string.upper",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("Help() missing %q in:\n%s", want, help)
		}
	}
}

func TestActionDefinition_Help_NoRequirements(t *testing.T) {
	a, err := domain.NewActionDefinition(
		"bare", "", domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectReadLocal},
		domain.IdempotencyProfile{},
	)
	if err != nil {
		t.Fatalf("NewActionDefinition: %v", err)
	}
	help := a.Help()
	if strings.Contains(help, "Requires capabilities") {
		t.Errorf("expected no capability section, got:\n%s", help)
	}
	if !strings.Contains(help, "Input:\n  (no fields)") {
		t.Errorf("expected empty-contract marker, got:\n%s", help)
	}
}

func TestCapabilityDefinition_Help(t *testing.T) {
	input := domain.NewContract([]domain.ContractField{
		{Name: "text", Type: "string", Description: "Text to uppercase", Required: true, Example: "hello"},
	})
	c, err := domain.NewCapabilityDefinition("string.upper", "Uppercases a string", input, domain.EmptyContract())
	if err != nil {
		t.Fatalf("NewCapabilityDefinition: %v", err)
	}
	help := c.Help()
	for _, want := range []string{
		"string.upper — Uppercases a string",
		"text  (string, required)",
		"example: hello",
		"Output:",
		"(no fields)",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("Help() missing %q in:\n%s", want, help)
		}
	}
}
