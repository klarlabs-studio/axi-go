package axi_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
)

// --- Test plugin ---

type testPlugin struct{}

func (p *testPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"echo", "Echoes input",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.echo")
	return domain.NewPluginContribution("test.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

type echoExecutor struct{}

func (e *echoExecutor) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{Data: input, Summary: "echoed"}, nil, nil
}

// --- Tests ---

func TestKernel_FluentBuilder(t *testing.T) {
	kernel := axi.New().
		WithBudget(axi.Budget{MaxCapabilityInvocations: 50}).
		WithTimeout(5 * time.Second)

	if kernel == nil {
		t.Fatal("expected kernel")
	}
}

func TestKernel_RegisterAndExecute(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})

	if err := kernel.RegisterPlugin(&testPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "echo",
		Input:  map[string]any{"msg": "hi"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != domain.StatusSucceeded {
		t.Errorf("expected succeeded, got %s", result.Status)
	}
	if result.Result == nil {
		t.Fatal("expected result")
	}
}

func TestKernel_InvalidActionName(t *testing.T) {
	kernel := axi.New()
	_, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "1-invalid",
	})
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestKernel_ListActions(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	_ = kernel.RegisterPlugin(&testPlugin{})

	actions := kernel.ListActions()
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Name() != "echo" {
		t.Errorf("expected echo, got %s", actions[0].Name())
	}
}

func TestKernel_GetAction(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	_ = kernel.RegisterPlugin(&testPlugin{})

	action, err := kernel.GetAction("echo")
	if err != nil {
		t.Fatalf("GetAction: %v", err)
	}
	if action.Description() != "Echoes input" {
		t.Errorf("unexpected description: %s", action.Description())
	}
}

func TestKernel_Deregister(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	_ = kernel.RegisterPlugin(&testPlugin{})

	if err := kernel.DeregisterPlugin("test.plugin"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if len(kernel.ListActions()) != 0 {
		t.Error("expected no actions after deregister")
	}
}

// Approval gate via the fluent API.

type externalPlugin struct{}

func (p *externalPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"send", "Sends something external",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectWriteExternal},
		domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.send")
	return domain.NewPluginContribution("ext.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

func TestKernel_ApprovalFlow(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.send", &echoExecutor{})
	_ = kernel.RegisterPlugin(&externalPlugin{})

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "send", Input: map[string]any{"to": "world"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != domain.StatusAwaitingApproval {
		t.Fatalf("expected awaiting_approval, got %s", result.Status)
	}

	approved, err := kernel.Approve(context.Background(), string(result.SessionID), domain.ApprovalDecision{Principal: "test-user"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != domain.StatusSucceeded {
		t.Errorf("expected succeeded after approval, got %s", approved.Status)
	}
}

func TestKernel_RejectFlow(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.send", &echoExecutor{})
	_ = kernel.RegisterPlugin(&externalPlugin{})

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "send", Input: map[string]any{"to": "world"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != domain.StatusAwaitingApproval {
		t.Fatalf("expected awaiting_approval, got %s", result.Status)
	}

	rejected, err := kernel.Reject(context.Background(), string(result.SessionID),
		domain.ApprovalDecision{Principal: "reviewer", Rationale: "risk too high"})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != domain.StatusRejected {
		t.Errorf("expected rejected, got %s", rejected.Status)
	}
	if rejected.Failure == nil || rejected.Failure.Message != "risk too high" {
		t.Errorf("failure message should come from Rationale, got %+v", rejected.Failure)
	}
	if rejected.ApprovalDecision == nil || rejected.ApprovalDecision.Principal != "reviewer" {
		t.Errorf("decision should carry principal, got %+v", rejected.ApprovalDecision)
	}
}

func TestKernel_Async(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	_ = kernel.RegisterPlugin(&testPlugin{})

	result, err := kernel.ExecuteAsync(context.Background(), axi.Invocation{
		Action: "echo", Input: map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatalf("ExecuteAsync: %v", err)
	}
	if result.Status != domain.StatusPending {
		t.Errorf("expected pending, got %s", result.Status)
	}

	// Poll for completion.
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		session, err := kernel.GetSession(string(result.SessionID))
		if err != nil {
			continue
		}
		if session.Status() == domain.StatusSucceeded {
			return
		}
	}
	t.Error("async execution did not complete")
}

// panicExecutor panics on every execution. Used to verify async panic
// recovery: the kernel's ExecuteAsync goroutine must not crash the process
// and the session must transition to Failed with a PANIC reason.
type panicExecutor struct{}

func (e *panicExecutor) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	panic("boom")
}

type panicPlugin struct{}

func (p *panicPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"panic-me", "Always panics",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.panic")
	return domain.NewPluginContribution("panic.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

func TestKernel_ExecuteAsync_ParallelPanics(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.panic", &panicExecutor{})
	if err := kernel.RegisterPlugin(&panicPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	const parallelism = 32
	sessions := make([]string, parallelism)

	// Fire N async executions concurrently. Each one panics inside its
	// goroutine; the kernel's panic recovery must translate that into a
	// Failed session without crashing the process or racing on shared state.
	var wg sync.WaitGroup
	wg.Add(parallelism)
	for i := 0; i < parallelism; i++ {
		i := i
		go func() {
			defer wg.Done()
			out, err := kernel.ExecuteAsync(context.Background(), axi.Invocation{
				Action: "panic-me",
				Input:  map[string]any{"i": i},
			})
			if err != nil {
				t.Errorf("ExecuteAsync #%d: %v", i, err)
				return
			}
			sessions[i] = string(out.SessionID)
		}()
	}
	wg.Wait()

	// Poll each session until it settles (Succeeded or Failed). With panics,
	// every one of them should end up Failed.
	deadline := time.Now().Add(2 * time.Second)
	for _, id := range sessions {
		if id == "" {
			continue
		}
		for {
			session, err := kernel.GetSession(id)
			if err == nil {
				status := session.Status()
				if status == domain.StatusFailed {
					if f := session.Failure(); f == nil || f.Code != "PANIC" {
						t.Errorf("session %s: expected PANIC failure, got %+v", id, f)
					}
					break
				}
				if status == domain.StatusSucceeded {
					t.Errorf("session %s: expected Failed, got Succeeded", id)
					break
				}
			}
			if time.Now().After(deadline) {
				t.Errorf("session %s did not settle within deadline", id)
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestKernel_RegisterBundle(t *testing.T) {
	kernel := axi.New()

	action, _ := domain.NewActionDefinition("bundled", "B",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = action.BindExecutor("exec.bundled")
	contribution, _ := domain.NewPluginContribution("bundle.plugin",
		[]*domain.ActionDefinition{action}, nil)

	bundle, err := domain.NewPluginBundle(contribution,
		map[domain.ActionExecutorRef]domain.ActionExecutor{
			"exec.bundled": &echoExecutor{},
		}, nil)
	if err != nil {
		t.Fatalf("NewPluginBundle: %v", err)
	}

	if err := kernel.RegisterBundle(bundle); err != nil {
		t.Fatalf("RegisterBundle: %v", err)
	}

	if len(kernel.ListActions()) != 1 {
		t.Error("expected bundle to register 1 action")
	}
}

// Demonstrate the example from the package doc works.
func Example() {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.greet", &greetDocExecutor{})
	_ = kernel.RegisterPlugin(&docPlugin{})

	result, _ := kernel.Execute(context.Background(), axi.Invocation{
		Action: "greet",
		Input:  map[string]any{"name": "world"},
	})
	data, _ := result.Result.Data.(map[string]any)
	_ = strings.Contains(data["message"].(string), "world")
}

type docPlugin struct{}

func (p *docPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition("greet", "",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = action.BindExecutor("exec.greet")
	return domain.NewPluginContribution("doc.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

type greetDocExecutor struct{}

func (e *greetDocExecutor) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	m := input.(map[string]any)
	return domain.ExecutionResult{Data: map[string]any{"message": "Hello, " + m["name"].(string)}}, nil, nil
}

// --- Suggestions tests ---

type suggestingExecutor struct{}

func (e *suggestingExecutor) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{
		Data:    map[string]any{"created": true},
		Summary: "created resource",
		Suggestions: []domain.Suggestion{
			{Action: "resource.get", Description: "Retrieve the created resource"},
			{Action: "resource.list", Description: "List all resources"},
		},
	}, nil, nil
}

type suggestingPlugin struct{}

func (p *suggestingPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"resource.create", "Creates a resource",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: false},
	)
	_ = action.BindExecutor("exec.resource.create")
	return domain.NewPluginContribution("suggest.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

func TestKernel_Help(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.echo", &echoExecutor{})
	if err := kernel.RegisterPlugin(&testPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	help, err := kernel.Help("echo")
	if err != nil {
		t.Fatalf("Help: %v", err)
	}
	if !strings.Contains(help, "echo — Echoes input") {
		t.Errorf("expected description in help, got:\n%s", help)
	}

	_, err = kernel.Help("does.not.exist")
	if err == nil {
		t.Error("expected error for unknown name")
	}
}

func TestKernel_ExecutionSuggestions(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.resource.create", &suggestingExecutor{})

	if err := kernel.RegisterPlugin(&suggestingPlugin{}); err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "resource.create",
		Input:  map[string]any{},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != domain.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
	if result.Result == nil {
		t.Fatal("expected result")
	}
	if len(result.Result.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(result.Result.Suggestions))
	}
	if result.Result.Suggestions[0].Action != "resource.get" {
		t.Errorf("expected first suggestion action 'resource.get', got %q", result.Result.Suggestions[0].Action)
	}
	if result.Result.Suggestions[0].Description != "Retrieve the created resource" {
		t.Errorf("unexpected first suggestion description: %s", result.Result.Suggestions[0].Description)
	}
	if result.Result.Suggestions[1].Action != "resource.list" {
		t.Errorf("expected second suggestion action 'resource.list', got %q", result.Result.Suggestions[1].Action)
	}
}
