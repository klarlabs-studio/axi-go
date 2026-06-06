package axi_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
)

// recordingPublisher is a thread-safe DomainEventPublisher used in tests.
type recordingPublisher struct {
	mu     sync.Mutex
	events []domain.DomainEvent
}

func (r *recordingPublisher) Publish(event domain.DomainEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recordingPublisher) snapshot() []domain.DomainEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.DomainEvent, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recordingPublisher) typesByPosition() []string {
	out := []string{}
	for _, e := range r.snapshot() {
		out = append(out, e.EventType())
	}
	return out
}

// --- Plugins / executors ---

type echoPlugin struct{}

func (echoPlugin) Contribute() (*domain.PluginContribution, error) {
	a, _ := domain.NewActionDefinition(
		"echo", "echo input",
		domain.EmptyContract(), domain.EmptyContract(),
		nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = a.BindExecutor("exec.echo")
	return domain.NewPluginContribution("echo.plugin", []*domain.ActionDefinition{a}, nil)
}

type echoExec struct{}

func (echoExec) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{Data: input, Summary: "ok"},
		[]domain.EvidenceRecord{{Kind: "trace", Source: "echo", Value: "hi", TokensUsed: 7}},
		nil
}

// failingExec returns an error to exercise the failure event path.
type failingExec struct{}

func (failingExec) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{}, nil, errors.New("boom")
}

type failingPlugin struct{}

func (failingPlugin) Contribute() (*domain.PluginContribution, error) {
	a, _ := domain.NewActionDefinition(
		"fail", "always fails",
		domain.EmptyContract(), domain.EmptyContract(),
		nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: false},
	)
	_ = a.BindExecutor("exec.fail")
	return domain.NewPluginContribution("fail.plugin", []*domain.ActionDefinition{a}, nil)
}

// --- Tests ---

func TestKernel_DomainEventPublisher_HappyPath(t *testing.T) {
	rp := &recordingPublisher{}
	kernel := axi.New().WithDomainEventPublisher(rp)
	kernel.RegisterActionExecutor("exec.echo", echoExec{})
	if err := kernel.RegisterPlugin(echoPlugin{}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	if _, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "echo",
		Input:  map[string]any{"msg": "hi"},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := rp.typesByPosition()
	want := []string{
		"session.started",
		"evidence.recorded",
		"session.completed",
	}
	if !equalStringSlices(got, want) {
		t.Fatalf("event sequence:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestKernel_DomainEventPublisher_FailurePath(t *testing.T) {
	rp := &recordingPublisher{}
	kernel := axi.New().WithDomainEventPublisher(rp)
	kernel.RegisterActionExecutor("exec.fail", failingExec{})
	if err := kernel.RegisterPlugin(failingPlugin{}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}

	result, err := kernel.Execute(context.Background(), axi.Invocation{Action: "fail"})
	if err != nil {
		t.Fatalf("Execute returned go-error: %v", err)
	}
	if result.Status != domain.StatusFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}

	// Failure path must end with session.completed (Status: Failed).
	events := rp.snapshot()
	if len(events) == 0 {
		t.Fatalf("expected events, got none")
	}
	last := events[len(events)-1]
	completed, ok := last.(domain.SessionCompleted)
	if !ok {
		t.Fatalf("last event = %T, want SessionCompleted", last)
	}
	if completed.Status != domain.StatusFailed {
		t.Errorf("SessionCompleted.Status = %s, want failed", completed.Status)
	}
}

func TestKernel_DomainEventPublisher_DefaultIsNop(t *testing.T) {
	// A kernel built without WithDomainEventPublisher must still work —
	// default NopDomainEventPublisher absorbs everything.
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", echoExec{})
	if err := kernel.RegisterPlugin(echoPlugin{}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if _, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "echo",
		Input:  map[string]any{},
	}); err != nil {
		t.Fatalf("Execute with default publisher: %v", err)
	}
}

func TestKernel_DomainEventPublisher_NilFallsBackToNop(t *testing.T) {
	// Passing nil must not panic — the kernel falls back to Nop.
	kernel := axi.New().WithDomainEventPublisher(nil)
	kernel.RegisterActionExecutor("exec.echo", echoExec{})
	if err := kernel.RegisterPlugin(echoPlugin{}); err != nil {
		t.Fatalf("RegisterPlugin: %v", err)
	}
	if _, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "echo",
		Input:  map[string]any{},
	}); err != nil {
		t.Fatalf("Execute after WithDomainEventPublisher(nil): %v", err)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
