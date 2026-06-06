package application_test

import (
	"context"
	"testing"

	"go.klarlabs.de/axi/application"
	"go.klarlabs.de/axi/domain"
	"go.klarlabs.de/axi/inmemory"
)

// stubActionExecutor is a simple action executor for testing.
type stubActionExecutor struct {
	fn func(ctx context.Context, input any, caps domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error)
}

func (s *stubActionExecutor) Execute(ctx context.Context, input any, caps domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return s.fn(ctx, input, caps)
}

// stubCapExecutor is a simple capability executor for testing.
type stubCapExecutor struct {
	fn func(ctx context.Context, input any) (any, error)
}

func (s *stubCapExecutor) Execute(ctx context.Context, input any) (any, error) {
	return s.fn(ctx, input)
}

func setupFullSystem(t *testing.T) (
	*application.RegisterPluginContributionUseCase,
	*application.ExecuteActionUseCase,
	*inmemory.ActionExecutorRegistry,
	*inmemory.CapabilityExecutorRegistry,
) {
	t.Helper()

	actionRepo := inmemory.NewActionDefinitionRepository()
	capRepo := inmemory.NewCapabilityDefinitionRepository()
	pluginRepo := inmemory.NewPluginContributionRepository()
	sessionRepo := inmemory.NewExecutionSessionRepository()
	validator := inmemory.NewContractValidator()
	actionExecReg := inmemory.NewActionExecutorRegistry()
	capExecReg := inmemory.NewCapabilityExecutorRegistry()
	idGen := inmemory.NewSequentialIDGenerator()

	compositionService := domain.NewCompositionService(actionRepo, capRepo, pluginRepo)
	resolutionService := domain.NewCapabilityResolutionService(capRepo)
	executionService := domain.NewActionExecutionService(actionRepo, resolutionService, validator, actionExecReg, capExecReg)

	registerUC := &application.RegisterPluginContributionUseCase{
		CompositionService: compositionService,
	}
	executeUC := &application.ExecuteActionUseCase{
		SessionRepo:      sessionRepo,
		ExecutionService: executionService,
		IDGen:            idGen,
	}

	return registerUC, executeUC, actionExecReg, capExecReg
}

func TestFullExecutionFlow_Success(t *testing.T) {
	registerUC, executeUC, actionExecReg, capExecReg := setupFullSystem(t)

	// Define a capability.
	capName, _ := domain.NewCapabilityName("string.upper")
	cap, _ := domain.NewCapabilityDefinition(capName, "Uppercase a string", domain.EmptyContract(), domain.EmptyContract())
	_ = cap.BindExecutor("exec.upper")

	// Register capability executor.
	capExecReg.Register("exec.upper", &stubCapExecutor{
		fn: func(_ context.Context, input any) (any, error) {
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
		},
	})

	// Define an action that requires the capability.
	actionName, _ := domain.NewActionName("greet")
	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: capName})
	action, _ := domain.NewActionDefinition(
		actionName, "Greet someone",
		domain.NewContract([]domain.ContractField{{Name: "name", Required: true}}),
		domain.NewContract([]domain.ContractField{{Name: "message", Required: true}}),
		reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.greet")

	// Register action executor.
	actionExecReg.Register("exec.greet", &stubActionExecutor{
		fn: func(_ context.Context, input any, caps domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			m := input.(map[string]any)
			name := m["name"].(string)

			upper, err := caps.Invoke("string.upper", name)
			if err != nil {
				return domain.ExecutionResult{}, nil, err
			}

			msg := "Hello, " + upper.(string) + "!"
			return domain.ExecutionResult{
					Data:    map[string]any{"message": msg},
					Summary: "Greeted " + name,
				}, []domain.EvidenceRecord{
					{Kind: "invocation", Source: "greet", Value: name},
				}, nil
		},
	})

	// Register plugin contribution.
	plugin, err := domain.NewPluginContribution("greet.plugin",
		[]*domain.ActionDefinition{action},
		[]*domain.CapabilityDefinition{cap},
	)
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}
	if err := registerUC.Execute(plugin); err != nil {
		t.Fatalf("failed to register plugin: %v", err)
	}

	// Execute the action.
	output, err := executeUC.Execute(context.Background(), application.ExecuteActionInput{
		ActionName: "greet",
		Input:      map[string]any{"name": "world"},
	})
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if output.Status != domain.StatusSucceeded {
		t.Errorf("expected Succeeded, got %s", output.Status)
	}
	if output.Result == nil {
		t.Fatal("expected result")
	}
	resultMap, ok := output.Result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", output.Result.Data)
	}
	if resultMap["message"] != "Hello, WORLD!" {
		t.Errorf("expected 'Hello, WORLD!', got %v", resultMap["message"])
	}
	if len(output.Evidence) != 1 {
		t.Errorf("expected 1 evidence record, got %d", len(output.Evidence))
	}
}

func TestFullExecutionFlow_ValidationFailure(t *testing.T) {
	registerUC, executeUC, actionExecReg, _ := setupFullSystem(t)

	actionName, _ := domain.NewActionName("strict-action")
	action, _ := domain.NewActionDefinition(
		actionName, "Requires name field",
		domain.NewContract([]domain.ContractField{{Name: "name", Required: true}}),
		domain.EmptyContract(),
		nil,
		domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.strict")
	actionExecReg.Register("exec.strict", &stubActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "ok"}, nil, nil
		},
	})

	plugin, _ := domain.NewPluginContribution("strict.plugin", []*domain.ActionDefinition{action}, nil)
	_ = registerUC.Execute(plugin)

	// Missing required "name" field.
	_, err := executeUC.Execute(context.Background(), application.ExecuteActionInput{
		ActionName: "strict-action",
		Input:      map[string]any{},
	})
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestFullExecutionFlow_ActionFailure(t *testing.T) {
	registerUC, executeUC, actionExecReg, _ := setupFullSystem(t)

	actionName, _ := domain.NewActionName("fail-action")
	action, _ := domain.NewActionDefinition(
		actionName, "Always fails",
		domain.EmptyContract(), domain.EmptyContract(),
		nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.fail")
	actionExecReg.Register("exec.fail", &stubActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{}, []domain.EvidenceRecord{{Kind: "error", Source: "fail-action", Value: "intentional"}},
				context.DeadlineExceeded
		},
	})

	plugin, _ := domain.NewPluginContribution("fail.plugin", []*domain.ActionDefinition{action}, nil)
	_ = registerUC.Execute(plugin)

	output, err := executeUC.Execute(context.Background(), application.ExecuteActionInput{
		ActionName: "fail-action",
		Input:      nil,
	})
	if err != nil {
		t.Fatalf("unexpected error (failure is a valid outcome): %v", err)
	}
	if output.Status != domain.StatusFailed {
		t.Errorf("expected Failed, got %s", output.Status)
	}
	if output.Failure == nil {
		t.Fatal("expected failure reason")
	}
	if len(output.Evidence) != 1 {
		t.Errorf("expected 1 evidence record, got %d", len(output.Evidence))
	}
}

func TestPluginRegistration_DuplicatePluginRejected(t *testing.T) {
	registerUC, _, _, _ := setupFullSystem(t)

	p1, _ := domain.NewPluginContribution("same.id", nil, nil)
	_ = registerUC.Execute(p1)

	p2, _ := domain.NewPluginContribution("same.id", nil, nil)
	if err := registerUC.Execute(p2); err == nil {
		t.Error("expected error for duplicate plugin ID")
	}
}

// appPlugin implements domain.Plugin for application-level testing.
type appPlugin struct {
	id domain.PluginID
}

func (p *appPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition("plugin-greet", "Greet from plugin",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.plugin-greet")
	return domain.NewPluginContribution(p.id, []*domain.ActionDefinition{action}, nil)
}

func TestPluginRegistration_ViaPluginInterface(t *testing.T) {
	registerUC, executeUC, actionExecReg, _ := setupFullSystem(t)

	actionExecReg.Register("exec.plugin-greet", &stubActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{Data: "hello from plugin", Summary: "greeted"}, nil, nil
		},
	})

	plugin := &appPlugin{id: "app.plugin"}
	if err := registerUC.ExecutePlugin(plugin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := executeUC.Execute(context.Background(), application.ExecuteActionInput{
		ActionName: "plugin-greet",
		Input:      nil,
	})
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if output.Status != domain.StatusSucceeded {
		t.Errorf("expected Succeeded, got %s", output.Status)
	}
	if output.Result.Data != "hello from plugin" {
		t.Errorf("expected 'hello from plugin', got %v", output.Result.Data)
	}
}

func TestExecuteAsync_PropagatesContextValues(t *testing.T) {
	registerUC, executeUC, actionExecReg, _ := setupFullSystem(t)

	type ctxKey struct{}
	const sentinel = "trace-id-abc123"

	// Channel to capture the context value observed inside the executor.
	observed := make(chan any, 1)

	actionName, _ := domain.NewActionName("async-ctx")
	action, _ := domain.NewActionDefinition(
		actionName, "Checks context propagation",
		domain.EmptyContract(), domain.EmptyContract(),
		nil, domain.EffectProfile{}, domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.async-ctx")
	actionExecReg.Register("exec.async-ctx", &stubActionExecutor{
		fn: func(ctx context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			observed <- ctx.Value(ctxKey{})
			return domain.ExecutionResult{Data: "ok", Summary: "done"}, nil, nil
		},
	})

	plugin, _ := domain.NewPluginContribution("async-ctx.plugin", []*domain.ActionDefinition{action}, nil)
	if err := registerUC.Execute(plugin); err != nil {
		t.Fatalf("failed to register plugin: %v", err)
	}

	// Put a value on the caller's context.
	ctx := context.WithValue(context.Background(), ctxKey{}, sentinel)

	_, err := executeUC.ExecuteAsync(ctx, application.ExecuteActionInput{
		ActionName: "async-ctx",
		Input:      nil,
	})
	if err != nil {
		t.Fatalf("ExecuteAsync failed: %v", err)
	}

	// Wait for the background goroutine to report the value it observed.
	got := <-observed
	if got != sentinel {
		t.Errorf("expected context value %q, got %v", sentinel, got)
	}
}

func TestPluginRegistration_ConflictingActionNamesRejected(t *testing.T) {
	registerUC, _, actionExecReg, _ := setupFullSystem(t)

	a1, _ := domain.NewActionDefinition("greet", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a1.BindExecutor("exec.a")
	actionExecReg.Register("exec.a", &stubActionExecutor{
		fn: func(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
			return domain.ExecutionResult{}, nil, nil
		},
	})

	p1, _ := domain.NewPluginContribution("p1", []*domain.ActionDefinition{a1}, nil)
	if err := registerUC.Execute(p1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a2, _ := domain.NewActionDefinition("greet", "B", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a2.BindExecutor("exec.b")
	p2, _ := domain.NewPluginContribution("p2", []*domain.ActionDefinition{a2}, nil)
	if err := registerUC.Execute(p2); err == nil {
		t.Error("expected error for conflicting action name")
	}
}
