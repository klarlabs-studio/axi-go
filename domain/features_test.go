package domain_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"go.klarlabs.de/axi/domain"
)

// --- Rate Limiting ---

type denyRateLimiter struct{}

func (r *denyRateLimiter) Allow(_ domain.ActionName) error {
	return fmt.Errorf("rate limit exceeded")
}

func TestRateLimiter_Blocks(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)
	execSvc.SetRateLimiter(&denyRateLimiter{})

	action, _ := domain.NewActionDefinition("limited", "Rate limited",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.limited")
	_ = actionRepo.Save(action)
	actionExecs.executors["exec.limited"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "ok"}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "limited", nil)
	err := execSvc.Execute(context.Background(), session)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	var validation *domain.ErrValidation
	if !errors.As(err, &validation) {
		t.Errorf("expected ErrValidation, got %T: %v", err, err)
	}
}

// --- Finer Effect Levels ---

func TestEffectProfile_IsWriteEffect(t *testing.T) {
	tests := []struct {
		level      domain.EffectLevel
		isWrite    bool
		isExternal bool
	}{
		{domain.EffectNone, false, false},
		{domain.EffectReadLocal, false, false},
		{domain.EffectWriteLocal, true, false},
		{domain.EffectReadExternal, false, true},
		{domain.EffectWriteExternal, true, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			p := domain.EffectProfile{Level: tt.level}
			if p.IsWriteEffect() != tt.isWrite {
				t.Errorf("IsWriteEffect() = %v, want %v", p.IsWriteEffect(), tt.isWrite)
			}
			if p.IsExternalEffect() != tt.isExternal {
				t.Errorf("IsExternalEffect() = %v, want %v", p.IsExternalEffect(), tt.isExternal)
			}
		})
	}
}

func TestValidEffectLevel(t *testing.T) {
	valid := []domain.EffectLevel{
		domain.EffectNone, domain.EffectReadLocal, domain.EffectWriteLocal,
		domain.EffectReadExternal, domain.EffectWriteExternal,
	}
	for _, l := range valid {
		if !domain.ValidEffectLevel(l) {
			t.Errorf("%q should be valid", l)
		}
	}
	invalid := []domain.EffectLevel{"catastrophic", "local", "external", ""}
	for _, l := range invalid {
		if domain.ValidEffectLevel(l) {
			t.Errorf("%q should be invalid", l)
		}
	}
}

// --- Approval Gate for write-external ---

func TestApprovalGate_WriteExternal(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)

	action, _ := domain.NewActionDefinition("send-email", "Sends email",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectWriteExternal}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.email")
	_ = actionRepo.Save(action)
	actionExecs.executors["exec.email"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "sent"}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "send-email", nil)
	err := execSvc.Execute(context.Background(), session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status() != domain.StatusAwaitingApproval {
		t.Fatalf("expected AwaitingApproval, got %s", session.Status())
	}

	// Approve and resume.
	if err := session.Approve(domain.ApprovalDecision{Principal: "test-user"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := execSvc.Resume(context.Background(), session); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if session.Status() != domain.StatusSucceeded {
		t.Errorf("expected Succeeded, got %s", session.Status())
	}
}

func TestApprovalGate_ReadExternal_SkipsApproval(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)

	action, _ := domain.NewActionDefinition("search", "Search (read-only)",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectReadExternal}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.search")
	_ = actionRepo.Save(action)
	actionExecs.executors["exec.search"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "results"}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "search", nil)
	_ = execSvc.Execute(context.Background(), session)
	// read-external should NOT trigger approval.
	if session.Status() != domain.StatusSucceeded {
		t.Errorf("expected Succeeded (no approval for read-external), got %s", session.Status())
	}
}

func TestSession_Reject(t *testing.T) {
	session, _ := domain.NewExecutionSession("s1", "action", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkAwaitingApproval()

	err := session.Reject(domain.FailureReason{Code: "REJECTED", Message: "too risky"}, domain.ApprovalDecision{Principal: "test-user", Rationale: "too risky"})
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if session.Status() != domain.StatusRejected {
		t.Errorf("expected Rejected, got %s", session.Status())
	}
	if session.Failure() == nil || session.Failure().Code != "REJECTED" {
		t.Error("expected REJECTED failure")
	}
}

// --- Plugin Deregistration ---

func TestCompositionService_Deregister(t *testing.T) {
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	pluginRepo := newFakePluginRepo()
	svc := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)

	a, _ := domain.NewActionDefinition("greet", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a.BindExecutor("exec.a")
	c, _ := domain.NewCapabilityDefinition("upper", "B", domain.EmptyContract(), domain.EmptyContract())
	_ = c.BindExecutor("exec.b")
	p, _ := domain.NewPluginContribution("p1", []*domain.ActionDefinition{a}, []*domain.CapabilityDefinition{c})
	_ = svc.RegisterContribution(p)

	// Verify registered.
	if _, err := actionRepo.GetByName("greet"); err != nil {
		t.Fatal("action should exist before deregister")
	}

	// Deregister.
	if err := svc.DeregisterPlugin("p1"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}

	// Verify removed.
	if _, err := actionRepo.GetByName("greet"); err == nil {
		t.Error("action should be removed after deregister")
	}
	if _, err := capRepo.GetByName("upper"); err == nil {
		t.Error("capability should be removed after deregister")
	}
}

func TestCompositionService_Deregister_NotFound(t *testing.T) {
	svc := domain.NewCompositionService(newFakeActionRepo(), newFakeCapRepo(), newFakePluginRepo())
	err := svc.DeregisterPlugin("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plugin")
	}
	var notFound *domain.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("expected ErrNotFound, got %T", err)
	}
}

// --- Plugin Lifecycle ---

type lifecyclePlugin struct {
	initCalled  bool
	closeCalled bool
	config      map[string]any
}

func (p *lifecyclePlugin) Init(config map[string]any) error {
	p.initCalled = true
	p.config = config
	return nil
}

func (p *lifecyclePlugin) Close() error {
	p.closeCalled = true
	return nil
}

func (p *lifecyclePlugin) Contribute() (*domain.PluginContribution, error) {
	return domain.NewPluginContribution("lifecycle.plugin", nil, nil)
}

func TestLifecyclePlugin_InitCalledWithConfig(t *testing.T) {
	svc := domain.NewCompositionService(newFakeActionRepo(), newFakeCapRepo(), newFakePluginRepo())
	plugin := &lifecyclePlugin{}
	config := map[string]any{"key": "value"}

	if err := svc.RegisterPluginWithConfig(plugin, config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !plugin.initCalled {
		t.Error("Init should have been called")
	}
	if plugin.config["key"] != "value" {
		t.Error("config should have been passed")
	}
}

type failingLifecyclePlugin struct{ lifecyclePlugin }

func (p *failingLifecyclePlugin) Init(_ map[string]any) error {
	return fmt.Errorf("init failed")
}

func TestLifecyclePlugin_InitFailure(t *testing.T) {
	svc := domain.NewCompositionService(newFakeActionRepo(), newFakeCapRepo(), newFakePluginRepo())
	err := svc.RegisterPluginWithConfig(&failingLifecyclePlugin{}, nil)
	if err == nil {
		t.Error("expected error from failing Init")
	}
}

// --- Pipeline ---

func TestPipeline_SequentialExecution(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)

	// Register two capabilities.
	c1, _ := domain.NewCapabilityDefinition("double", "Doubles", domain.EmptyContract(), domain.EmptyContract())
	_ = c1.BindExecutor("exec.double")
	_ = capRepo.Save(c1)
	capExecs.executors["exec.double"] = &fakeCapExecutor{
		fn: func(_ context.Context, input any) (any, error) {
			n, _ := input.(int)
			return n * 2, nil
		},
	}

	c2, _ := domain.NewCapabilityDefinition("add-one", "Adds one", domain.EmptyContract(), domain.EmptyContract())
	_ = c2.BindExecutor("exec.add-one")
	_ = capRepo.Save(c2)
	capExecs.executors["exec.add-one"] = &fakeCapExecutor{
		fn: func(_ context.Context, input any) (any, error) {
			n, _ := input.(int)
			return n + 1, nil
		},
	}

	// Register a pipeline capability that chains double → add-one.
	pipeline := domain.NewPipeline("double", "add-one")
	pipelineCap, _ := domain.NewCapabilityDefinition("double-then-add", "Pipeline", domain.EmptyContract(), domain.EmptyContract())
	_ = pipelineCap.BindExecutor("exec.pipeline")
	_ = capRepo.Save(pipelineCap)
	capExecs.executors["exec.pipeline"] = pipeline

	// Register action that uses the pipeline.
	reqs, _ := domain.NewRequirementSet(
		domain.Requirement{Capability: "double"},
		domain.Requirement{Capability: "add-one"},
		domain.Requirement{Capability: "double-then-add"},
	)
	action, _ := domain.NewActionDefinition("pipeline-test", "Tests pipeline",
		domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.pipeline-test")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.pipeline-test"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			// Input 5 → double(5)=10 → add-one(10)=11
			result, err := invoker.Invoke("double-then-add", 5)
			if err != nil {
				return domain.ExecutionResult{}, nil, err
			}
			return domain.ExecutionResult{Data: result}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "pipeline-test", nil)
	_ = execSvc.Execute(context.Background(), session)
	if session.Status() != domain.StatusSucceeded {
		t.Fatalf("expected Succeeded, got %s", session.Status())
	}
	if session.Result().Data != 11 {
		t.Errorf("expected 11 (5*2+1), got %v", session.Result().Data)
	}
}

// --- ComposableCapabilityExecutor ---

type composableUpperExecutor struct{}

func (e *composableUpperExecutor) Execute(_ context.Context, input any) (any, error) {
	return nil, fmt.Errorf("should not be called directly")
}

func (e *composableUpperExecutor) ExecuteWithInvoker(_ context.Context, input any, invoker domain.CapabilityInvoker) (any, error) {
	// This composable capability calls another capability.
	s, _ := input.(string)
	result := ""
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			result += string(r - 32)
		} else {
			result += string(r)
		}
	}
	return result, nil
}

func TestComposableCapabilityExecutor(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)

	cap, _ := domain.NewCapabilityDefinition("composable-upper", "Composable upper", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.composable-upper")
	_ = capRepo.Save(cap)
	capExecs.executors["exec.composable-upper"] = &composableUpperExecutor{}

	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "composable-upper"})
	action, _ := domain.NewActionDefinition("use-composable", "Uses composable",
		domain.EmptyContract(), domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.use-composable")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.use-composable"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			result, err := invoker.Invoke("composable-upper", "hello")
			if err != nil {
				return domain.ExecutionResult{}, nil, err
			}
			return domain.ExecutionResult{Data: result}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "use-composable", nil)
	_ = execSvc.Execute(context.Background(), session)
	if session.Status() != domain.StatusSucceeded {
		t.Fatalf("expected Succeeded, got %s", session.Status())
	}
	if session.Result().Data != "HELLO" {
		t.Errorf("expected HELLO, got %v", session.Result().Data)
	}
}

// --- PluginBundle validation ---

func TestPluginBundle_MissingExecutor(t *testing.T) {
	action, _ := domain.NewActionDefinition("bundled", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = action.BindExecutor("exec.missing")
	contribution, _ := domain.NewPluginContribution("bundle.plugin", []*domain.ActionDefinition{action}, nil)

	_, err := domain.NewPluginBundle(contribution, nil, nil)
	if err == nil {
		t.Error("expected error for missing executor in bundle")
	}
}

// --- Concurrent Registration (TOCTOU test) ---

func TestConcurrentRegistration_SameActionName(t *testing.T) {
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	pluginRepo := newFakePluginRepo()
	svc := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)

	const n = 20
	var wg sync.WaitGroup
	var successes atomic.Int32
	var failures atomic.Int32

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a, _ := domain.NewActionDefinition("contested-action", fmt.Sprintf("Plugin %d", idx),
				domain.EmptyContract(), domain.EmptyContract(), nil,
				domain.EffectProfile{}, domain.IdempotencyProfile{},
			)
			_ = a.BindExecutor(domain.ActionExecutorRef(fmt.Sprintf("exec.%d", idx)))
			p, _ := domain.NewPluginContribution(
				domain.PluginID(fmt.Sprintf("plugin-%d", idx)),
				[]*domain.ActionDefinition{a}, nil,
			)
			if err := svc.RegisterContribution(p); err != nil {
				failures.Add(1)
			} else {
				successes.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if successes.Load() != 1 {
		t.Errorf("expected exactly 1 success, got %d (failures: %d)", successes.Load(), failures.Load())
	}
	if failures.Load() != int32(n-1) {
		t.Errorf("expected %d failures, got %d", n-1, failures.Load())
	}
}
