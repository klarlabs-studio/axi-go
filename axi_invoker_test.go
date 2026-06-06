package axi_test

import (
	"context"
	"errors"
	"testing"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
)

// --- Helpers: sub-action executors the orchestrator calls into ---

type subActionPlugin struct{}

func (subActionPlugin) Contribute() (*domain.PluginContribution, error) {
	// "sub.echo" — a trivial leaf action the orchestrator invokes.
	echo, _ := domain.NewActionDefinition(
		"sub.echo", "echoes input as data",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = echo.BindExecutor("exec.sub.echo")

	// "sub.fail" — returns a Go error so the session transitions to Failed.
	fail, _ := domain.NewActionDefinition(
		"sub.fail", "always fails",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: false},
	)
	_ = fail.BindExecutor("exec.sub.fail")

	return domain.NewPluginContribution("sub.plugin",
		[]*domain.ActionDefinition{echo, fail}, nil)
}

type subEchoExec struct{}

func (subEchoExec) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{Data: input, Summary: "echoed"}, nil, nil
}

type subFailExec struct{}

func (subFailExec) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{}, nil, errors.New("sub.fail: boom")
}

// --- Orchestrator action under test ---

type orchestratorPlugin struct{}

func (orchestratorPlugin) Contribute() (*domain.PluginContribution, error) {
	a, _ := domain.NewActionDefinition(
		"orch.compose", "orchestrates sub-actions",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = a.BindExecutor("exec.orch")
	return domain.NewPluginContribution("orch.plugin",
		[]*domain.ActionDefinition{a}, nil)
}

// orchExec invokes the action configured via the step slice using the
// ActionInvoker passed in by the kernel, then aggregates the outcomes
// into a single ExecutionResult.Data for the test to inspect.
type orchExec struct {
	steps []string // action names to invoke in sequence
}

func (o *orchExec) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	// Synchronous fallback — the tests guard against this by asserting
	// the orchestrated path ran (outcomes slice is populated).
	return domain.ExecutionResult{}, nil, errors.New("orchExec.Execute called — kernel should have preferred ExecuteOrchestrated")
}

func (o *orchExec) ExecuteOrchestrated(ctx context.Context, _ any, _ domain.CapabilityInvoker, actions domain.ActionInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	outcomes := make([]*domain.ActionOutcome, 0, len(o.steps))
	for _, step := range o.steps {
		out, err := actions.Invoke(ctx, domain.ActionName(step), map[string]any{"step": step})
		if err != nil {
			return domain.ExecutionResult{}, nil, err
		}
		outcomes = append(outcomes, out)
	}
	return domain.ExecutionResult{
		Data:    outcomes,
		Summary: "orchestrated",
	}, nil, nil
}

// --- Tests ---

func newKernelWithSubAndOrch(t *testing.T, steps []string) *axi.Kernel {
	t.Helper()
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.sub.echo", subEchoExec{})
	kernel.RegisterActionExecutor("exec.sub.fail", subFailExec{})
	kernel.RegisterActionExecutor("exec.orch", &orchExec{steps: steps})
	if err := kernel.RegisterPlugin(subActionPlugin{}); err != nil {
		t.Fatalf("register sub plugin: %v", err)
	}
	if err := kernel.RegisterPlugin(orchestratorPlugin{}); err != nil {
		t.Fatalf("register orch plugin: %v", err)
	}
	return kernel
}

func TestOrchestrator_InvokesSubActionSuccessfully(t *testing.T) {
	kernel := newKernelWithSubAndOrch(t, []string{"sub.echo"})

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "orch.compose",
		Input:  map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.StatusSucceeded {
		t.Fatalf("status = %s, want succeeded", result.Status)
	}

	outcomes, ok := result.Result.Data.([]*domain.ActionOutcome)
	if !ok {
		t.Fatalf("result data type = %T, want []*ActionOutcome", result.Result.Data)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes len = %d, want 1", len(outcomes))
	}
	if !outcomes[0].IsSuccess() {
		t.Errorf("sub-action outcome = %+v, want success", outcomes[0])
	}
	if outcomes[0].Result == nil {
		t.Fatal("sub-action Result is nil")
	}
	if outcomes[0].Result.Summary != "echoed" {
		t.Errorf("sub-action summary = %q, want 'echoed'", outcomes[0].Result.Summary)
	}
}

func TestOrchestrator_SubActionFailureSurfacesAsOutcome(t *testing.T) {
	kernel := newKernelWithSubAndOrch(t, []string{"sub.fail"})

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "orch.compose",
		Input:  map[string]any{},
	})
	if err != nil {
		// A sub-action failing is NOT a Go error — it's a domain outcome
		// the orchestrator is expected to inspect and decide on.
		t.Fatalf("orchestrator Execute returned Go error for sub-failure: %v", err)
	}
	if result.Status != domain.StatusSucceeded {
		t.Fatalf("parent status = %s, want succeeded (orchestrator completed, even though sub failed)", result.Status)
	}

	outcomes := result.Result.Data.([]*domain.ActionOutcome)
	if !outcomes[0].IsFailure() {
		t.Errorf("sub-action outcome = %+v, want failure", outcomes[0])
	}
	if outcomes[0].Failure == nil {
		t.Fatal("failed outcome has nil Failure")
	}
}

func TestOrchestrator_InvokesMultipleSubActionsInOrder(t *testing.T) {
	kernel := newKernelWithSubAndOrch(t, []string{"sub.echo", "sub.echo", "sub.fail", "sub.echo"})

	result, _ := kernel.Execute(context.Background(), axi.Invocation{
		Action: "orch.compose",
		Input:  map[string]any{},
	})
	outcomes := result.Result.Data.([]*domain.ActionOutcome)
	if len(outcomes) != 4 {
		t.Fatalf("outcomes len = %d, want 4", len(outcomes))
	}
	wantStatuses := []domain.ExecutionStatus{
		domain.StatusSucceeded,
		domain.StatusSucceeded,
		domain.StatusFailed,
		domain.StatusSucceeded,
	}
	for i, want := range wantStatuses {
		if outcomes[i].Status != want {
			t.Errorf("outcome[%d].Status = %s, want %s", i, outcomes[i].Status, want)
		}
	}
}

func TestOrchestrator_SubSessionsHaveDistinctIDs(t *testing.T) {
	// Each invocation must spawn a fresh session with its own ID so
	// downstream persistence/audit can distinguish them.
	kernel := newKernelWithSubAndOrch(t, []string{"sub.echo", "sub.echo", "sub.echo"})

	result, _ := kernel.Execute(context.Background(), axi.Invocation{
		Action: "orch.compose",
		Input:  map[string]any{},
	})
	outcomes := result.Result.Data.([]*domain.ActionOutcome)
	seen := make(map[domain.ExecutionSessionID]bool)
	for _, o := range outcomes {
		if o.SessionID == "" {
			t.Error("sub-outcome has empty SessionID")
		}
		if seen[o.SessionID] {
			t.Errorf("duplicate SessionID %q across sub-actions", o.SessionID)
		}
		seen[o.SessionID] = true
	}
	if result.SessionID == "" {
		t.Error("parent session has empty SessionID")
	}
	for _, o := range outcomes {
		if o.SessionID == result.SessionID {
			t.Error("sub-session SessionID collides with parent SessionID")
		}
	}
}

func TestOrchestrator_InvokesUnknownAction_ParentSessionFails(t *testing.T) {
	// "Transport-level" failure — the sub-action doesn't exist.
	// ActionInvoker.Invoke returns a Go error (not an ActionOutcome).
	// The orchestrator's ExecuteOrchestrated propagates the error up,
	// and the service marks the parent session Failed — not a Go
	// error from Kernel.Execute, since axi-go's contract is that
	// action failure is a valid outcome, not a transport-level error.
	kernel := newKernelWithSubAndOrch(t, []string{"no.such.action"})

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "orch.compose",
		Input:  map[string]any{},
	})
	if err != nil {
		t.Fatalf("Kernel.Execute returned Go error: %v — expected parent session marked Failed instead", err)
	}
	if result.Status != domain.StatusFailed {
		t.Errorf("parent status = %s, want failed", result.Status)
	}
	if result.Failure == nil {
		t.Fatal("parent has nil Failure")
	}
	if result.Failure.Code != "EXECUTION_ERROR" {
		t.Errorf("Failure.Code = %q, want EXECUTION_ERROR", result.Failure.Code)
	}
}

// --- Nested orchestration ---

type nestedOrchPlugin struct{}

func (nestedOrchPlugin) Contribute() (*domain.PluginContribution, error) {
	// "orch.outer" invokes "orch.inner" which invokes "sub.echo".
	outer, _ := domain.NewActionDefinition(
		"orch.outer", "outer orchestrator",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = outer.BindExecutor("exec.orch.outer")
	inner, _ := domain.NewActionDefinition(
		"orch.inner", "inner orchestrator",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = inner.BindExecutor("exec.orch.inner")
	return domain.NewPluginContribution("orch.nested.plugin",
		[]*domain.ActionDefinition{outer, inner}, nil)
}

type nestedExec struct {
	target domain.ActionName
}

func (n *nestedExec) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{}, nil, errors.New("should use ExecuteOrchestrated")
}

func (n *nestedExec) ExecuteOrchestrated(ctx context.Context, _ any, _ domain.CapabilityInvoker, actions domain.ActionInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	out, err := actions.Invoke(ctx, n.target, nil)
	if err != nil {
		return domain.ExecutionResult{}, nil, err
	}
	return domain.ExecutionResult{Data: out, Summary: "nested"}, nil, nil
}

func TestOrchestrator_NestedInvocationWorks(t *testing.T) {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.sub.echo", subEchoExec{})
	kernel.RegisterActionExecutor("exec.sub.fail", subFailExec{})
	kernel.RegisterActionExecutor("exec.orch.outer", &nestedExec{target: "orch.inner"})
	kernel.RegisterActionExecutor("exec.orch.inner", &nestedExec{target: "sub.echo"})
	if err := kernel.RegisterPlugin(subActionPlugin{}); err != nil {
		t.Fatalf("register sub: %v", err)
	}
	if err := kernel.RegisterPlugin(nestedOrchPlugin{}); err != nil {
		t.Fatalf("register nested: %v", err)
	}

	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "orch.outer",
		Input:  map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != domain.StatusSucceeded {
		t.Fatalf("outer status = %s, want succeeded", result.Status)
	}

	// Outer's Data is the inner's ActionOutcome — which itself carries
	// sub.echo's outcome nested one more layer.
	innerOutcome, ok := result.Result.Data.(*domain.ActionOutcome)
	if !ok {
		t.Fatalf("outer Result.Data type = %T, want *ActionOutcome", result.Result.Data)
	}
	if !innerOutcome.IsSuccess() {
		t.Errorf("inner outcome = %+v, want success", innerOutcome)
	}
	echoOutcome, ok := innerOutcome.Result.Data.(*domain.ActionOutcome)
	if !ok {
		t.Fatalf("inner Result.Data type = %T, want *ActionOutcome from nested Invoke", innerOutcome.Result.Data)
	}
	if !echoOutcome.IsSuccess() {
		t.Errorf("echo outcome = %+v, want success", echoOutcome)
	}
}

// --- ActionOutcome helpers ---

func TestActionOutcome_Helpers(t *testing.T) {
	var nilO *domain.ActionOutcome
	if nilO.IsSuccess() {
		t.Error("nil.IsSuccess() = true, want false")
	}
	if nilO.IsFailure() {
		t.Error("nil.IsFailure() = true, want false")
	}

	succ := &domain.ActionOutcome{Status: domain.StatusSucceeded}
	if !succ.IsSuccess() || succ.IsFailure() {
		t.Errorf("succeeded outcome helpers wrong: success=%v failure=%v", succ.IsSuccess(), succ.IsFailure())
	}
	fail := &domain.ActionOutcome{Status: domain.StatusFailed}
	if fail.IsSuccess() || !fail.IsFailure() {
		t.Errorf("failed outcome helpers wrong: success=%v failure=%v", fail.IsSuccess(), fail.IsFailure())
	}
	rej := &domain.ActionOutcome{Status: domain.StatusRejected}
	if rej.IsSuccess() || !rej.IsFailure() {
		t.Errorf("rejected outcome helpers wrong: success=%v failure=%v", rej.IsSuccess(), rej.IsFailure())
	}
	pending := &domain.ActionOutcome{Status: domain.StatusAwaitingApproval}
	if pending.IsSuccess() || pending.IsFailure() {
		t.Errorf("awaiting-approval outcome helpers wrong: success=%v failure=%v", pending.IsSuccess(), pending.IsFailure())
	}
}
