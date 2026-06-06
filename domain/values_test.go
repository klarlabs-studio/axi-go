package domain_test

import (
	"testing"

	"go.klarlabs.de/axi/domain"
)

func TestNewActionName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "greet", false},
		{"valid with dots", "my.action", false},
		{"valid with dashes", "my-action", false},
		{"valid with underscores", "my_action", false},
		{"valid mixed", "my.Action-1_a", false},
		{"empty", "", true},
		{"starts with number", "1action", true},
		{"has spaces", "my action", true},
		{"special chars", "my@action", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NewActionName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewActionName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.input {
				t.Errorf("NewActionName(%q) = %q", tt.input, got)
			}
		})
	}
}

func TestNewCapabilityName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "http.get", false},
		{"empty", "", true},
		{"invalid chars", "http get!", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domain.NewCapabilityName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCapabilityName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestNewPluginID(t *testing.T) {
	_, err := domain.NewPluginID("")
	if err == nil {
		t.Error("expected error for empty plugin ID")
	}
	id, err := domain.NewPluginID("my.plugin")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if id != "my.plugin" {
		t.Errorf("got %q", id)
	}
}

func TestNewRequirementSet_NoDuplicates(t *testing.T) {
	_, err := domain.NewRequirementSet(
		domain.Requirement{Capability: "cap.a"},
		domain.Requirement{Capability: "cap.b"},
	)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewRequirementSet_RejectsDuplicates(t *testing.T) {
	_, err := domain.NewRequirementSet(
		domain.Requirement{Capability: "cap.a"},
		domain.Requirement{Capability: "cap.a"},
	)
	if err == nil {
		t.Error("expected error for duplicate requirements")
	}
}

func TestContract(t *testing.T) {
	empty := domain.EmptyContract()
	if !empty.IsEmpty() {
		t.Error("expected empty contract")
	}

	c := domain.NewContract([]domain.ContractField{
		{Name: "query", Required: true},
	})
	if c.IsEmpty() {
		t.Error("expected non-empty contract")
	}
}
