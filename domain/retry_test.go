package domain_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.klarlabs.de/axi/domain"
)

// flakyExecutor fails the first n calls, then succeeds.
type flakyExecutor struct {
	failures int32
	calls    atomic.Int32
}

func (f *flakyExecutor) Execute(_ context.Context, input any) (any, error) {
	n := f.calls.Add(1)
	if n <= f.failures {
		return nil, errors.New("transient failure")
	}
	return input, nil
}

func TestRetry_IdempotentActionRecovers(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxRetries: 3, RetryBackoff: time.Microsecond})

	cap, _ := domain.NewCapabilityDefinition("flaky", "", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.flaky")
	_ = capRepo.Save(cap)
	flaky := &flakyExecutor{failures: 2} // fail twice, then succeed
	capExecs.executors["exec.flaky"] = flaky

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "flaky"})
	action, _ := domain.NewActionDefinition("safe-op", "", domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.op")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.op"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			out, err := invoker.Invoke("flaky", "hi")
			if err != nil {
				return domain.ExecutionResult{}, nil, err
			}
			return domain.ExecutionResult{Data: out}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "safe-op", nil)
	if err := execSvc.Execute(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status() != domain.StatusSucceeded {
		t.Fatalf("expected Succeeded (retries recover), got %s: %+v", session.Status(), session.Failure())
	}
	if got := flaky.calls.Load(); got != 3 {
		t.Errorf("expected 3 capability calls (2 failures + 1 success), got %d", got)
	}
}

func TestRetry_NonIdempotentActionFailsImmediately(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxRetries: 5, RetryBackoff: time.Microsecond})

	cap, _ := domain.NewCapabilityDefinition("flaky", "", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.flaky")
	_ = capRepo.Save(cap)
	flaky := &flakyExecutor{failures: 10}
	capExecs.executors["exec.flaky"] = flaky

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "flaky"})
	action, _ := domain.NewActionDefinition("unsafe-op", "", domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: false},
	)
	_ = action.BindExecutor("exec.op")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.op"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			out, err := invoker.Invoke("flaky", "hi")
			if err != nil {
				return domain.ExecutionResult{}, nil, err
			}
			return domain.ExecutionResult{Data: out}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "unsafe-op", nil)
	if err := execSvc.Execute(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status() != domain.StatusFailed {
		t.Fatalf("expected Failed (no retry for non-idempotent), got %s", session.Status())
	}
	if got := flaky.calls.Load(); got != 1 {
		t.Errorf("expected 1 capability call (no retries), got %d", got)
	}
}

func TestRetry_ExhaustsBudget(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxRetries: 2, RetryBackoff: time.Microsecond})

	cap, _ := domain.NewCapabilityDefinition("flaky", "", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.flaky")
	_ = capRepo.Save(cap)
	flaky := &flakyExecutor{failures: 100} // always fails
	capExecs.executors["exec.flaky"] = flaky

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "flaky"})
	action, _ := domain.NewActionDefinition("safe-op", "", domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.op")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.op"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			_, err := invoker.Invoke("flaky", "hi")
			return domain.ExecutionResult{}, nil, err
		},
	}

	session, _ := domain.NewExecutionSession("s1", "safe-op", nil)
	_ = execSvc.Execute(context.Background(), session)

	if session.Status() != domain.StatusFailed {
		t.Fatalf("expected Failed (retry budget exhausted), got %s", session.Status())
	}
	// Initial attempt + MaxRetries(2) = 3 calls total.
	if got := flaky.calls.Load(); got != 3 {
		t.Errorf("expected 3 capability calls (1 + 2 retries), got %d", got)
	}
}

func TestRetry_ContextCancellationStopsBackoff(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)
	execSvc.SetDefaultBudget(domain.ExecutionBudget{MaxRetries: 10, RetryBackoff: 50 * time.Millisecond})

	cap, _ := domain.NewCapabilityDefinition("flaky", "", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.flaky")
	_ = capRepo.Save(cap)
	flaky := &flakyExecutor{failures: 100}
	capExecs.executors["exec.flaky"] = flaky

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "flaky"})
	action, _ := domain.NewActionDefinition("safe-op", "", domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.op")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.op"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			_, err := invoker.Invoke("flaky", "hi")
			return domain.ExecutionResult{}, nil, err
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	session, _ := domain.NewExecutionSession("s1", "safe-op", nil)
	_ = execSvc.Execute(ctx, session)
	elapsed := time.Since(start)

	// With 10 retries × 50ms backoff (doubling), uncancelled would take >>1s.
	// Cancellation should stop the loop quickly.
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected fast cancellation; took %v", elapsed)
	}
	if session.Status() != domain.StatusFailed {
		t.Errorf("expected Failed, got %s", session.Status())
	}
}
