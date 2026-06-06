package domain_test

import (
	"testing"

	"go.klarlabs.de/axi/domain"
)

func TestActionDefinition_Creation(t *testing.T) {
	action, err := domain.NewActionDefinition(
		"greet",
		"Greet someone",
		domain.NewContract([]domain.ContractField{{Name: "name", Required: true}}),
		domain.NewContract([]domain.ContractField{{Name: "message", Required: true}}),
		nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action.Name() != "greet" {
		t.Errorf("expected name greet, got %s", action.Name())
	}
	if action.IsBound() {
		t.Error("should not be bound initially")
	}
}

func TestActionDefinition_EmptyNameRejected(t *testing.T) {
	_, err := domain.NewActionDefinition(
		"", "desc",
		domain.EmptyContract(), domain.EmptyContract(),
		nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	if err == nil {
		t.Error("expected error for empty action name")
	}
}

func TestActionDefinition_BindExecutor(t *testing.T) {
	action, _ := domain.NewActionDefinition(
		"a", "desc",
		domain.EmptyContract(), domain.EmptyContract(),
		nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)

	if err := action.BindExecutor(""); err == nil {
		t.Error("expected error for empty executor ref")
	}

	if err := action.BindExecutor("exec.greet"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !action.IsBound() {
		t.Error("expected bound after BindExecutor")
	}
}

func TestCapabilityDefinition_Creation(t *testing.T) {
	cap, err := domain.NewCapabilityDefinition(
		"http.get", "HTTP GET request",
		domain.NewContract([]domain.ContractField{{Name: "url", Required: true}}),
		domain.EmptyContract(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.Name() != "http.get" {
		t.Errorf("expected http.get, got %s", cap.Name())
	}
}

func TestCapabilityDefinition_EmptyNameRejected(t *testing.T) {
	_, err := domain.NewCapabilityDefinition("", "desc", domain.EmptyContract(), domain.EmptyContract())
	if err == nil {
		t.Error("expected error for empty capability name")
	}
}

func TestPluginContribution_Creation(t *testing.T) {
	action, _ := domain.NewActionDefinition("greet", "Greet", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	cap, _ := domain.NewCapabilityDefinition("http.get", "HTTP", domain.EmptyContract(), domain.EmptyContract())

	plugin, err := domain.NewPluginContribution("my.plugin", []*domain.ActionDefinition{action}, []*domain.CapabilityDefinition{cap})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plugin.Status() != domain.ContributionPending {
		t.Errorf("expected Pending, got %s", plugin.Status())
	}
}

func TestPluginContribution_EmptyIDRejected(t *testing.T) {
	_, err := domain.NewPluginContribution("", nil, nil)
	if err == nil {
		t.Error("expected error for empty plugin ID")
	}
}

func TestPluginContribution_DuplicateActionNamesRejected(t *testing.T) {
	a1, _ := domain.NewActionDefinition("greet", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	a2, _ := domain.NewActionDefinition("greet", "B", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})

	_, err := domain.NewPluginContribution("p", []*domain.ActionDefinition{a1, a2}, nil)
	if err == nil {
		t.Error("expected error for duplicate action names")
	}
}

func TestPluginContribution_DuplicateCapabilityNamesRejected(t *testing.T) {
	c1, _ := domain.NewCapabilityDefinition("http.get", "A", domain.EmptyContract(), domain.EmptyContract())
	c2, _ := domain.NewCapabilityDefinition("http.get", "B", domain.EmptyContract(), domain.EmptyContract())

	_, err := domain.NewPluginContribution("p", nil, []*domain.CapabilityDefinition{c1, c2})
	if err == nil {
		t.Error("expected error for duplicate capability names")
	}
}

func TestPluginContribution_ActivationRequiresBinding(t *testing.T) {
	action, _ := domain.NewActionDefinition("greet", "Greet", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	plugin, _ := domain.NewPluginContribution("p", []*domain.ActionDefinition{action}, nil)

	if err := plugin.Activate(); err == nil {
		t.Error("expected error: action not bound")
	}

	_ = action.BindExecutor("exec.greet")
	if err := plugin.Activate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plugin.Status() != domain.ContributionActive {
		t.Error("expected Active status")
	}
}

func TestPluginContribution_DoubleActivationRejected(t *testing.T) {
	plugin, _ := domain.NewPluginContribution("p", nil, nil)
	_ = plugin.Activate()

	if err := plugin.Activate(); err == nil {
		t.Error("expected error for double activation")
	}
}

// testPlugin implements the Plugin interface for testing.
type testPlugin struct {
	id domain.PluginID
}

func (p *testPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition("plugin-action", "From plugin", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = action.BindExecutor("exec.plugin")
	return domain.NewPluginContribution(p.id, []*domain.ActionDefinition{action}, nil)
}

func TestPluginInterface(t *testing.T) {
	var p domain.Plugin = &testPlugin{id: "test.plugin"}
	contribution, err := p.Contribute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contribution.PluginID() != "test.plugin" {
		t.Errorf("expected test.plugin, got %s", contribution.PluginID())
	}
	if len(contribution.Actions()) != 1 {
		t.Errorf("expected 1 action, got %d", len(contribution.Actions()))
	}
}
