package domain_test

import (
	"context"
	"testing"
	"time"

	"go.klarlabs.de/axi/domain"
)

func TestBudgetEnforcer_MaxInvocations(t *testing.T) {
	// Test via the full execution flow — register an action with a capability,
	// set a budget of 2 invocations, and call the capability 3 times.
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxCapabilityInvocations: 2})

	cap, _ := domain.NewCapabilityDefinition("counter", "Counts", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.counter")
	_ = capRepo.Save(cap)
	capExecs.executors["exec.counter"] = &fakeCapExecutor{
		fn: func(_ context.Context, input any) (any, error) { return input, nil },
	}

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "counter"})
	action, _ := domain.NewActionDefinition("budget-test", "Tests budget",
		domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.budget")
	_ = actionRepo.Save(action)

	callCount := 0
	actionExecs.executors["exec.budget"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			// Try 3 invocations — third should fail.
			for i := 0; i < 3; i++ {
				_, err := invoker.Invoke("counter", i)
				if err != nil {
					return domain.ExecutionResult{}, nil, err
				}
				callCount++
			}
			return domain.ExecutionResult{Data: "done"}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "budget-test", nil)
	err := execSvc.Execute(context.Background(), session)

	// Execution should complete (failure is a valid outcome).
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status() != domain.StatusFailed {
		t.Errorf("expected Failed (budget exceeded), got %s", session.Status())
	}
	if callCount != 2 {
		t.Errorf("expected 2 successful invocations, got %d", callCount)
	}
}

func TestBudgetEnforcer_MaxDuration(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxDuration: 1 * time.Millisecond})

	action, _ := domain.NewActionDefinition("slow-action", "Slow",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.slow")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.slow"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			// Budget is checked on capability invocations, not on the action itself.
			// With no capability invocations, duration budget isn't checked.
			return domain.ExecutionResult{Data: "ok"}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "slow-action", nil)
	err := execSvc.Execute(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without capability invocations, budget isn't enforced (by design).
	if session.Status() != domain.StatusSucceeded {
		t.Errorf("expected Succeeded, got %s", session.Status())
	}
}

func TestBudgetEnforcer_MaxTokens_Exceeded(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxTokens: 100})

	action, _ := domain.NewActionDefinition("token-test", "Tokens",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.tokens")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.tokens"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "ok"},
				[]domain.EvidenceRecord{
					{Kind: "llm", Source: "model-a", TokensUsed: 60},
					{Kind: "llm", Source: "model-b", TokensUsed: 50}, // total 110 > 100
				}, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "token-test", nil)
	if err := execSvc.Execute(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status() != domain.StatusFailed {
		t.Fatalf("expected Failed (budget exceeded), got %s", session.Status())
	}
	if f := session.Failure(); f == nil || f.Code != "BUDGET_EXCEEDED" {
		t.Errorf("expected BUDGET_EXCEEDED failure, got %+v", f)
	}
}

func TestBudgetEnforcer_MaxTokens_WithinBudget(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxTokens: 100})

	action, _ := domain.NewActionDefinition("token-ok", "Tokens ok",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.tokens.ok")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.tokens.ok"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "ok"},
				[]domain.EvidenceRecord{{Kind: "llm", Source: "m", TokensUsed: 40}}, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "token-ok", nil)
	if err := execSvc.Execute(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status() != domain.StatusSucceeded {
		t.Errorf("expected Succeeded, got %s", session.Status())
	}
}

func TestBudgetEnforcer_NoBudget(t *testing.T) {
	// Zero budget means no limit.
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)
	// No budget set (default zero values).

	cap, _ := domain.NewCapabilityDefinition("unlimited", "No limit", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.unlimited")
	_ = capRepo.Save(cap)
	capExecs.executors["exec.unlimited"] = &fakeCapExecutor{
		fn: func(_ context.Context, input any) (any, error) { return input, nil },
	}

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "unlimited"})
	action, _ := domain.NewActionDefinition("many-calls", "Many",
		domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.many")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.many"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			for i := 0; i < 100; i++ {
				if _, err := invoker.Invoke("unlimited", i); err != nil {
					return domain.ExecutionResult{}, nil, err
				}
			}
			return domain.ExecutionResult{Data: "done"}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "many-calls", nil)
	_ = execSvc.Execute(context.Background(), session)
	if session.Status() != domain.StatusSucceeded {
		t.Errorf("expected Succeeded with no budget, got %s", session.Status())
	}
}
