package domain_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"go.klarlabs.de/axi/domain"
)

// --- In-test fakes (domain_test can't import inmemory without a cycle via application) ---

type fakeActionRepo struct {
	mu      sync.RWMutex
	actions map[domain.ActionName]*domain.ActionDefinition
}

func newFakeActionRepo() *fakeActionRepo {
	return &fakeActionRepo{actions: make(map[domain.ActionName]*domain.ActionDefinition)}
}

func (r *fakeActionRepo) GetByName(name domain.ActionName) (*domain.ActionDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.actions[name]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return a, nil
}

func (r *fakeActionRepo) Save(a *domain.ActionDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[a.Name()] = a
	return nil
}

func (r *fakeActionRepo) Delete(name domain.ActionName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.actions, name)
	return nil
}

func (r *fakeActionRepo) List() []*domain.ActionDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.ActionDefinition, 0, len(r.actions))
	for _, a := range r.actions {
		out = append(out, a)
	}
	return out
}

type fakeCapRepo struct {
	mu   sync.RWMutex
	caps map[domain.CapabilityName]*domain.CapabilityDefinition
}

func newFakeCapRepo() *fakeCapRepo {
	return &fakeCapRepo{caps: make(map[domain.CapabilityName]*domain.CapabilityDefinition)}
}

func (r *fakeCapRepo) GetByName(name domain.CapabilityName) (*domain.CapabilityDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.caps[name]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return c, nil
}

func (r *fakeCapRepo) Save(c *domain.CapabilityDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caps[c.Name()] = c
	return nil
}

func (r *fakeCapRepo) Delete(name domain.CapabilityName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.caps, name)
	return nil
}

func (r *fakeCapRepo) List() []*domain.CapabilityDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*domain.CapabilityDefinition, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c)
	}
	return out
}

type fakePluginRepo struct {
	mu      sync.RWMutex
	plugins map[domain.PluginID]*domain.PluginContribution
}

func newFakePluginRepo() *fakePluginRepo {
	return &fakePluginRepo{plugins: make(map[domain.PluginID]*domain.PluginContribution)}
}

func (r *fakePluginRepo) Save(p *domain.PluginContribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.PluginID()] = p
	return nil
}

func (r *fakePluginRepo) GetByID(id domain.PluginID) (*domain.PluginContribution, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return p, nil
}

func (r *fakePluginRepo) Delete(id domain.PluginID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, id)
	return nil
}

func (r *fakePluginRepo) Exists(id domain.PluginID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.plugins[id]
	return ok
}

type fakeValidator struct{}

func (v *fakeValidator) Validate(contract domain.Contract, input any) error {
	if contract.IsEmpty() {
		return nil
	}
	m, ok := input.(map[string]any)
	if !ok {
		return fmt.Errorf("input must be map[string]any")
	}
	for _, f := range contract.Fields {
		if f.Required {
			if _, exists := m[f.Name]; !exists {
				return fmt.Errorf("missing required field %q", f.Name)
			}
		}
	}
	return nil
}

type fakeActionExecutor struct {
	fn func(ctx context.Context, input any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error)
}

func (e *fakeActionExecutor) Execute(ctx context.Context, input any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return e.fn(ctx, input, invoker)
}

type fakeCapExecutor struct {
	fn func(ctx context.Context, input any) (any, error)
}

func (e *fakeCapExecutor) Execute(ctx context.Context, input any) (any, error) {
	return e.fn(ctx, input)
}

type fakeActionExecLookup struct {
	executors map[domain.ActionExecutorRef]domain.ActionExecutor
}

func (l *fakeActionExecLookup) GetActionExecutor(ref domain.ActionExecutorRef) (domain.ActionExecutor, error) {
	e, ok := l.executors[ref]
	if !ok {
		return nil, fmt.Errorf("executor %q not found", ref)
	}
	return e, nil
}

type fakeCapExecLookup struct {
	executors map[domain.CapabilityExecutorRef]domain.CapabilityExecutor
}

func (l *fakeCapExecLookup) GetCapabilityExecutor(ref domain.CapabilityExecutorRef) (domain.CapabilityExecutor, error) {
	e, ok := l.executors[ref]
	if !ok {
		return nil, fmt.Errorf("executor %q not found", ref)
	}
	return e, nil
}

// --- CompositionService tests ---

func TestCompositionService_RegisterContribution(t *testing.T) {
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	pluginRepo := newFakePluginRepo()
	svc := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)

	action, _ := domain.NewActionDefinition("greet", "Greet", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = action.BindExecutor("exec.greet")
	cap, _ := domain.NewCapabilityDefinition("string.upper", "Upper", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.upper")

	plugin, _ := domain.NewPluginContribution("test.plugin", []*domain.ActionDefinition{action}, []*domain.CapabilityDefinition{cap})

	if err := svc.RegisterContribution(plugin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plugin.Status() != domain.ContributionActive {
		t.Errorf("expected Active, got %s", plugin.Status())
	}

	// Verify persisted.
	got, err := actionRepo.GetByName("greet")
	if err != nil || got == nil {
		t.Error("action not persisted")
	}
	gotCap, err := capRepo.GetByName("string.upper")
	if err != nil || gotCap == nil {
		t.Error("capability not persisted")
	}
}

func TestCompositionService_RejectsDuplicatePlugin(t *testing.T) {
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	pluginRepo := newFakePluginRepo()
	svc := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)

	p1, _ := domain.NewPluginContribution("dup", nil, nil)
	_ = svc.RegisterContribution(p1)

	p2, _ := domain.NewPluginContribution("dup", nil, nil)
	if err := svc.RegisterContribution(p2); err == nil {
		t.Error("expected error for duplicate plugin")
	}
}

func TestCompositionService_RejectsConflictingActionName(t *testing.T) {
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	pluginRepo := newFakePluginRepo()
	svc := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)

	a1, _ := domain.NewActionDefinition("greet", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a1.BindExecutor("exec.a")
	p1, _ := domain.NewPluginContribution("p1", []*domain.ActionDefinition{a1}, nil)
	_ = svc.RegisterContribution(p1)

	a2, _ := domain.NewActionDefinition("greet", "B", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a2.BindExecutor("exec.b")
	p2, _ := domain.NewPluginContribution("p2", []*domain.ActionDefinition{a2}, nil)
	if err := svc.RegisterContribution(p2); err == nil {
		t.Error("expected error for conflicting action name")
	}
}

func TestCompositionService_RejectsConflictingCapabilityName(t *testing.T) {
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	pluginRepo := newFakePluginRepo()
	svc := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)

	c1, _ := domain.NewCapabilityDefinition("http.get", "A", domain.EmptyContract(), domain.EmptyContract())
	_ = c1.BindExecutor("exec.a")
	p1, _ := domain.NewPluginContribution("p1", nil, []*domain.CapabilityDefinition{c1})
	_ = svc.RegisterContribution(p1)

	c2, _ := domain.NewCapabilityDefinition("http.get", "B", domain.EmptyContract(), domain.EmptyContract())
	_ = c2.BindExecutor("exec.b")
	p2, _ := domain.NewPluginContribution("p2", nil, []*domain.CapabilityDefinition{c2})
	if err := svc.RegisterContribution(p2); err == nil {
		t.Error("expected error for conflicting capability name")
	}
}

func TestCompositionService_RejectsUnboundContribution(t *testing.T) {
	svc := domain.NewCompositionService(newFakeActionRepo(), newFakeCapRepo(), newFakePluginRepo())

	action, _ := domain.NewActionDefinition("unbound", "No binding", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	plugin, _ := domain.NewPluginContribution("p", []*domain.ActionDefinition{action}, nil)

	if err := svc.RegisterContribution(plugin); err == nil {
		t.Error("expected error for unbound action")
	}
}

// --- RegisterPlugin (Plugin interface) ---

type contributorPlugin struct {
	id domain.PluginID
}

func (p *contributorPlugin) Contribute() (*domain.PluginContribution, error) {
	a, _ := domain.NewActionDefinition("contributed-action", "From plugin", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a.BindExecutor("exec.contributed")
	return domain.NewPluginContribution(p.id, []*domain.ActionDefinition{a}, nil)
}

type failingPlugin struct{}

func (p *failingPlugin) Contribute() (*domain.PluginContribution, error) {
	return nil, errors.New("plugin init failed")
}

func TestCompositionService_RegisterPlugin(t *testing.T) {
	svc := domain.NewCompositionService(newFakeActionRepo(), newFakeCapRepo(), newFakePluginRepo())

	plugin := &contributorPlugin{id: "contrib.plugin"}
	if err := svc.RegisterPlugin(plugin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompositionService_RegisterPlugin_ContributeError(t *testing.T) {
	svc := domain.NewCompositionService(newFakeActionRepo(), newFakeCapRepo(), newFakePluginRepo())

	if err := svc.RegisterPlugin(&failingPlugin{}); err == nil {
		t.Error("expected error from failing Contribute()")
	}
}

// --- CapabilityResolutionService tests ---

func TestResolutionService_ResolvesAll(t *testing.T) {
	capRepo := newFakeCapRepo()
	c1, _ := domain.NewCapabilityDefinition("cap.a", "A", domain.EmptyContract(), domain.EmptyContract())
	c2, _ := domain.NewCapabilityDefinition("cap.b", "B", domain.EmptyContract(), domain.EmptyContract())
	_ = capRepo.Save(c1)
	_ = capRepo.Save(c2)

	svc := domain.NewCapabilityResolutionService(capRepo)
	reqs, _ := domain.NewRequirementSet(
		domain.Requirement{Capability: "cap.a"},
		domain.Requirement{Capability: "cap.b"},
	)

	resolved, err := svc.Resolve(reqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 2 {
		t.Errorf("expected 2 resolved, got %d", len(resolved))
	}
}

func TestResolutionService_MissingCapability(t *testing.T) {
	svc := domain.NewCapabilityResolutionService(newFakeCapRepo())
	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "missing"})

	_, err := svc.Resolve(reqs)
	if err == nil {
		t.Error("expected error for missing capability")
	}
}

func TestResolutionService_EmptyRequirements(t *testing.T) {
	svc := domain.NewCapabilityResolutionService(newFakeCapRepo())
	resolved, err := svc.Resolve(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected 0, got %d", len(resolved))
	}
}

// --- ActionExecutionService tests ---

func setupExecution(t *testing.T) (
	*domain.ActionExecutionService,
	*fakeActionRepo,
	*fakeCapRepo,
	*fakeActionExecLookup,
	*fakeCapExecLookup,
) {
	t.Helper()
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	validator := &fakeValidator{}
	actionExecs := &fakeActionExecLookup{executors: make(map[domain.ActionExecutorRef]domain.ActionExecutor)}
	capExecs := &fakeCapExecLookup{executors: make(map[domain.CapabilityExecutorRef]domain.CapabilityExecutor)}
	resSvc := domain.NewCapabilityResolutionService(capRepo)
	execSvc := domain.NewActionExecutionService(actionRepo, resSvc, validator, actionExecs, capExecs)
	return execSvc, actionRepo, capRepo, actionExecs, capExecs
}

func TestExecutionService_SuccessWithCapability(t *testing.T) {
	execSvc, actionRepo, capRepo, actionExecs, capExecs := setupExecution(t)

	// Register capability.
	cap, _ := domain.NewCapabilityDefinition("string.reverse", "Reverse", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.reverse")
	_ = capRepo.Save(cap)

	capExecs.executors["exec.reverse"] = &fakeCapExecutor{
		fn: func(_ context.Context, input any) (any, error) {
			s := input.(string)
			runes := []rune(s)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		},
	}

	// Register action.
	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: "string.reverse"})
	action, _ := domain.NewActionDefinition("reverse-greet", "Reverse greet",
		domain.NewContract([]domain.ContractField{{Name: "name", Required: true}}),
		domain.EmptyContract(), reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.reverse-greet")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.reverse-greet"] = &fakeActionExecutor{
		fn: func(_ context.Context, input any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			m := input.(map[string]any)
			reversed, err := invoker.Invoke("string.reverse", m["name"])
			if err != nil {
				return domain.ExecutionResult{}, nil, err
			}
			return domain.ExecutionResult{Data: reversed, Summary: "reversed"},
				[]domain.EvidenceRecord{{Kind: "transform", Source: "reverse-greet", Value: reversed}}, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "reverse-greet", map[string]any{"name": "hello"})
	if err := execSvc.Execute(context.Background(), session); err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if session.Status() != domain.StatusSucceeded {
		t.Errorf("expected Succeeded, got %s", session.Status())
	}
	if session.Result().Data != "olleh" {
		t.Errorf("expected olleh, got %v", session.Result().Data)
	}
	if len(session.Evidence()) != 1 {
		t.Errorf("expected 1 evidence, got %d", len(session.Evidence()))
	}
	if len(session.ResolvedCapabilities()) != 1 || session.ResolvedCapabilities()[0] != "string.reverse" {
		t.Errorf("unexpected resolved capabilities: %v", session.ResolvedCapabilities())
	}
}

func TestExecutionService_ActionNotFound(t *testing.T) {
	execSvc, _, _, _, _ := setupExecution(t)

	session, _ := domain.NewExecutionSession("s1", "nonexistent", nil)
	err := execSvc.Execute(context.Background(), session)
	if err == nil {
		t.Error("expected error for missing action")
	}
}

func TestExecutionService_ValidationFailure(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)

	action, _ := domain.NewActionDefinition("strict", "Strict",
		domain.NewContract([]domain.ContractField{{Name: "required-field", Required: true}}),
		domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.strict")
	_ = actionRepo.Save(action)
	actionExecs.executors["exec.strict"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{}, nil, nil
		},
	}

	session, _ := domain.NewExecutionSession("s1", "strict", map[string]any{})
	err := execSvc.Execute(context.Background(), session)
	if err == nil {
		t.Error("expected validation error")
	}
	if session.Status() != domain.StatusPending {
		t.Errorf("session should remain Pending on validation failure, got %s", session.Status())
	}
}

func TestExecutionService_ExecutorFailure(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)

	action, _ := domain.NewActionDefinition("fail", "Fails",
		domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.fail")
	_ = actionRepo.Save(action)
	actionExecs.executors["exec.fail"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{},
				[]domain.EvidenceRecord{{Kind: "error", Source: "fail", Value: "boom"}},
				errors.New("intentional failure")
		},
	}

	session, _ := domain.NewExecutionSession("s1", "fail", nil)
	err := execSvc.Execute(context.Background(), session)
	if err != nil {
		t.Fatalf("execution failure is a valid outcome, should not return error: %v", err)
	}
	if session.Status() != domain.StatusFailed {
		t.Errorf("expected Failed, got %s", session.Status())
	}
	if session.Failure() == nil || session.Failure().Code != "EXECUTION_ERROR" {
		t.Error("expected EXECUTION_ERROR failure code")
	}
	if len(session.Evidence()) != 1 {
		t.Errorf("expected evidence even on failure, got %d", len(session.Evidence()))
	}
}

func TestExecutionService_MissingExecutorBinding(t *testing.T) {
	execSvc, actionRepo, _, _, _ := setupExecution(t)

	action, _ := domain.NewActionDefinition("orphan", "No executor registered",
		domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.missing")
	_ = actionRepo.Save(action)

	session, _ := domain.NewExecutionSession("s1", "orphan", nil)
	err := execSvc.Execute(context.Background(), session)
	if err == nil {
		t.Error("expected error for missing executor")
	}
	if session.Status() != domain.StatusFailed {
		t.Errorf("expected Failed when executor not found, got %s", session.Status())
	}
}

func TestExecutionService_InvokerRejectsUnresolvedCapability(t *testing.T) {
	execSvc, actionRepo, _, actionExecs, _ := setupExecution(t)

	action, _ := domain.NewActionDefinition("bad-invoke", "Tries to invoke unresolved cap",
		domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.bad")
	_ = actionRepo.Save(action)

	actionExecs.executors["exec.bad"] = &fakeActionExecutor{
		fn: func(_ context.Context, _ any, invoker domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			_, err := invoker.Invoke("not.resolved", nil)
			return domain.ExecutionResult{}, nil, err
		},
	}

	session, _ := domain.NewExecutionSession("s1", "bad-invoke", nil)
	err := execSvc.Execute(context.Background(), session)
	if err != nil {
		t.Fatalf("failure is valid outcome: %v", err)
	}
	if session.Status() != domain.StatusFailed {
		t.Errorf("expected Failed, got %s", session.Status())
	}
}
