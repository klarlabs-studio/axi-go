// Package axi is the entry point for axi-go — a domain-driven execution kernel
// for semantic actions. It provides a fluent, descriptive SDK for registering
// plugins and executing actions with built-in safety controls.
//
// Example:
//
//	kernel := axi.New().
//	    WithLogger(logger).
//	    WithBudget(axi.Budget{MaxCapabilityInvocations: 100})
//
//	if err := kernel.RegisterPlugin(myPlugin); err != nil {
//	    return err
//	}
//
//	result, err := kernel.Execute(ctx, axi.Invocation{
//	    Action: "greet",
//	    Input:  map[string]any{"name": "world"},
//	})
//
// axi-go is a library you embed, not a service you run. There is no HTTP API.
// Delivery mechanisms (HTTP, gRPC, CLI, MCP) are the caller's choice; build
// your own adapter around this kernel.
package axi

import (
	"context"
	"time"

	"github.com/felixgeelhaar/axi-go/application"
	"github.com/felixgeelhaar/axi-go/domain"
	"github.com/felixgeelhaar/axi-go/inmemory"
)

// Kernel is the fluent entry point for axi-go. Build it with New(), configure
// it with With* methods, register plugins, then Execute actions.
//
// A Kernel is NOT safe to configure concurrently. Call With* before the first
// Execute. Execute itself is safe for concurrent use.
type Kernel struct {
	actionRepo    *inmemory.ActionDefinitionRepository
	capRepo       *inmemory.CapabilityDefinitionRepository
	pluginRepo    *inmemory.PluginContributionRepository
	sessionRepo   domain.SessionRepository
	actionExecReg *inmemory.ActionExecutorRegistry
	capExecReg    *inmemory.CapabilityExecutorRegistry
	validator     domain.ContractValidator
	idGen         application.IDGenerator

	composition *domain.CompositionService
	resolution  *domain.CapabilityResolutionService
	execution   *domain.ActionExecutionService

	register *application.RegisterPluginContributionUseCase
	execute  *application.ExecuteActionUseCase
}

// Budget is an alias for domain.ExecutionBudget for ergonomic SDK usage.
type Budget = domain.ExecutionBudget

// Invocation is the input to Execute — an action name plus its input data.
type Invocation struct {
	Action string
	Input  map[string]any
}

// Result is the output of Execute — session state, result data, evidence.
type Result = application.ExecuteActionOutput

// New creates a Kernel with default in-memory adapters.
// Further configuration is done via chainable With* methods.
func New() *Kernel {
	k := &Kernel{
		actionRepo:    inmemory.NewActionDefinitionRepository(),
		capRepo:       inmemory.NewCapabilityDefinitionRepository(),
		pluginRepo:    inmemory.NewPluginContributionRepository(),
		sessionRepo:   inmemory.NewExecutionSessionRepository(),
		actionExecReg: inmemory.NewActionExecutorRegistry(),
		capExecReg:    inmemory.NewCapabilityExecutorRegistry(),
		validator:     inmemory.NewContractValidator(),
		idGen:         inmemory.NewSequentialIDGenerator(),
	}
	k.wire()
	return k
}

// wire builds the domain services and use cases from the current adapters.
func (k *Kernel) wire() {
	k.composition = domain.NewCompositionService(k.actionRepo, k.capRepo, k.pluginRepo)
	k.resolution = domain.NewCapabilityResolutionService(k.capRepo)
	k.execution = domain.NewActionExecutionService(
		k.actionRepo, k.resolution, k.validator, k.actionExecReg, k.capExecReg,
	)

	k.register = &application.RegisterPluginContributionUseCase{
		CompositionService: k.composition,
	}
	k.execute = &application.ExecuteActionUseCase{
		SessionRepo:      k.sessionRepo,
		ExecutionService: k.execution,
		IDGen:            k.idGen,
	}

	// Wire the ActionInvoker so OrchestratorActionExecutor plugins
	// (sagas, fan-out, aggregators) can invoke other registered
	// actions through the kernel's full lifecycle. The invoker is a
	// thin wrapper over ExecuteActionUseCase — each sub-action runs
	// with its own fresh session, budget, and evidence chain.
	k.execution.SetActionInvoker(&kernelActionInvoker{execute: k.execute})
}

// kernelActionInvoker is the kernel-level implementation of
// domain.ActionInvoker. It delegates to the ExecuteActionUseCase so
// sub-action invocations traverse the identical pipeline (rate limit,
// validate, resolve, approve, run, validate output, events) as
// top-level Kernel.Execute calls.
type kernelActionInvoker struct {
	execute *application.ExecuteActionUseCase
}

// Invoke runs action with the given input in a fresh ExecutionSession
// and returns a domain.ActionOutcome. Go-level errors are reserved for
// transport-level failures — domain failures surface through the
// returned ActionOutcome's Status + Failure fields.
func (i *kernelActionInvoker) Invoke(ctx context.Context, action domain.ActionName, input any) (*domain.ActionOutcome, error) {
	out, err := i.execute.Execute(ctx, application.ExecuteActionInput{
		ActionName: action,
		Input:      input,
	})
	if err != nil {
		return nil, err
	}
	return &domain.ActionOutcome{
		SessionID: out.SessionID,
		Status:    out.Status,
		Result:    out.Result,
		Failure:   out.Failure,
		Evidence:  out.Evidence,
	}, nil
}

// WithLogger sets a structured logger for the kernel. Returns the kernel for chaining.
func (k *Kernel) WithLogger(logger domain.Logger) *Kernel {
	k.execution.SetLogger(logger)
	return k
}

// WithBudget sets the default execution budget (max duration, max capability
// invocations, max retries, max tokens). Returns the kernel for chaining.
func (k *Kernel) WithBudget(budget Budget) *Kernel {
	k.execution.SetDefaultBudget(budget)
	return k
}

// WithRateLimiter sets a rate limiter checked before each execution.
// Returns the kernel for chaining.
func (k *Kernel) WithRateLimiter(rl domain.RateLimiter) *Kernel {
	k.execution.SetRateLimiter(rl)
	return k
}

// WithDomainEventPublisher wires a publisher that receives domain events
// raised during execution (session lifecycle, capability invocations,
// budget exhaustion, evidence). Pass nil to fall back to the no-op
// default. Returns the kernel for chaining.
//
// The default is domain.NopDomainEventPublisher, which discards events
// and preserves axi-go's zero-deps story for callers that do not wire
// observability. Adapters classify events into Prometheus metrics,
// OpenTelemetry spans, audit logs, or any other downstream concern;
// the kernel itself imports nothing vendor-specific.
func (k *Kernel) WithDomainEventPublisher(p domain.DomainEventPublisher) *Kernel {
	k.execution.SetDomainEventPublisher(p)
	return k
}

// WithTimeout configures a default execution timeout via the budget's MaxDuration.
// Returns the kernel for chaining.
func (k *Kernel) WithTimeout(d time.Duration) *Kernel {
	k.execution.SetDefaultBudget(Budget{MaxDuration: d})
	return k
}

// WithIDGenerator overrides the default session ID generator.
func (k *Kernel) WithIDGenerator(gen application.IDGenerator) *Kernel {
	k.idGen = gen
	k.execute.IDGen = gen
	return k
}

// WithSessionRepository overrides the default in-memory session store with a
// custom SessionRepository — for example a PostgreSQL-backed repository so that
// write-external sessions paused at AwaitingApproval survive process restarts
// (the default in-memory store does not). Implementations serialize sessions
// via ExecutionSession.ToSnapshot and reload them with SessionFromSnapshot.
//
// Must be called before the first Execute. Returns the kernel for chaining.
func (k *Kernel) WithSessionRepository(repo domain.SessionRepository) *Kernel {
	k.sessionRepo = repo
	k.execute.SessionRepo = repo
	return k
}

// RegisterPlugin registers a Plugin by calling Contribute() and activating it.
// If the plugin implements domain.LifecyclePlugin, Init() is called first.
func (k *Kernel) RegisterPlugin(plugin domain.Plugin) error {
	return k.composition.RegisterPlugin(plugin)
}

// RegisterPluginWithConfig registers a LifecyclePlugin with configuration.
func (k *Kernel) RegisterPluginWithConfig(plugin domain.Plugin, config domain.PluginConfig) error {
	return k.composition.RegisterPluginWithConfig(plugin, config)
}

// RegisterBundle atomically registers a plugin contribution along with its
// executor implementations. Preferred over RegisterPlugin when you want to
// validate executor refs match implementations before registration.
func (k *Kernel) RegisterBundle(bundle *domain.PluginBundle) error {
	return k.composition.RegisterBundle(bundle, k.actionExecReg, k.capExecReg)
}

// DeregisterPlugin removes a plugin and all its contributed actions/capabilities.
func (k *Kernel) DeregisterPlugin(id string) error {
	return k.composition.DeregisterPlugin(domain.PluginID(id))
}

// RegisterActionExecutor wires an executor ref to an implementation.
// Use this when registering actions without a PluginBundle.
func (k *Kernel) RegisterActionExecutor(ref string, executor domain.ActionExecutor) {
	k.actionExecReg.Register(domain.ActionExecutorRef(ref), executor)
}

// RegisterCapabilityExecutor wires a capability executor ref to an implementation.
func (k *Kernel) RegisterCapabilityExecutor(ref string, executor domain.CapabilityExecutor) {
	k.capExecReg.Register(domain.CapabilityExecutorRef(ref), executor)
}

// Execute runs an action synchronously and returns the full result.
// If the action has effect_level "write-external", execution pauses at
// AwaitingApproval and the caller must Approve() or Reject() before completion.
func (k *Kernel) Execute(ctx context.Context, inv Invocation) (*Result, error) {
	actionName, err := domain.NewActionName(inv.Action)
	if err != nil {
		return nil, err
	}
	return k.execute.Execute(ctx, application.ExecuteActionInput{
		ActionName: actionName,
		Input:      inv.Input,
	})
}

// ExecuteAsync submits an action for background execution and returns immediately.
// Poll via GetSession(sessionID) to check status.
func (k *Kernel) ExecuteAsync(ctx context.Context, inv Invocation) (*Result, error) {
	actionName, err := domain.NewActionName(inv.Action)
	if err != nil {
		return nil, err
	}
	return k.execute.ExecuteAsync(ctx, application.ExecuteActionInput{
		ActionName: actionName,
		Input:      inv.Input,
	})
}

// Approve approves a session in AwaitingApproval state and resumes execution.
// The decision must include a non-empty Principal identifying who approved.
func (k *Kernel) Approve(ctx context.Context, sessionID string, decision domain.ApprovalDecision) (*Result, error) {
	return k.execute.ApproveSession(ctx, domain.ExecutionSessionID(sessionID), decision)
}

// Reject rejects a session in AwaitingApproval state. The decision's
// Rationale is recorded as the FailureReason.Message; Principal is required
// non-empty. Signature is symmetric with Approve.
func (k *Kernel) Reject(ctx context.Context, sessionID string, decision domain.ApprovalDecision) (*Result, error) {
	return k.execute.RejectSession(ctx, domain.ExecutionSessionID(sessionID), decision)
}

// GetSession returns the current state of an execution session by ID.
func (k *Kernel) GetSession(sessionID string) (*domain.ExecutionSession, error) {
	return k.sessionRepo.Get(domain.ExecutionSessionID(sessionID))
}

// ListActions returns all registered actions.
func (k *Kernel) ListActions() []*domain.ActionDefinition {
	return k.actionRepo.List()
}

// ListCapabilities returns all registered capabilities.
func (k *Kernel) ListCapabilities() []*domain.CapabilityDefinition {
	return k.capRepo.List()
}

// GetAction returns an action definition by name.
func (k *Kernel) GetAction(name string) (*domain.ActionDefinition, error) {
	return k.actionRepo.GetByName(domain.ActionName(name))
}

// Help returns a human-readable description of the named action or capability
// (actions are looked up first). Aligned with axi.md principle #10 — callers
// fall back to Help when contextual suggestions aren't enough.
func (k *Kernel) Help(name string) (string, error) {
	if a, err := k.actionRepo.GetByName(domain.ActionName(name)); err == nil {
		return a.Help(), nil
	}
	if c, err := k.capRepo.GetByName(domain.CapabilityName(name)); err == nil {
		return c.Help(), nil
	}
	return "", &domain.ErrNotFound{Entity: "action or capability", ID: name}
}
