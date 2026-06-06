// Package main demonstrates adopter patterns for the axi-go 1.1/1.2
// observability, integrity, and cost-control primitives.
//
// Three reference patterns are wired here:
//
//  1. DomainEventPublisher — strict-DDD subscriber pattern replacing
//     bespoke analytics adapters. One type subscribes to every kernel
//     lifecycle event and fans out to whatever sink the adopter wants
//     (stdout here; in production you'd log-ship, push to Prometheus,
//     emit OpenTelemetry spans, append to a Kafka topic, etc.).
//
//  2. Evidence chain verification — compliance endpoint pattern.
//     After a session completes, session.VerifyEvidenceChain() returns
//     either nil or *ErrChainBroken. Operators expose this via an HTTP
//     endpoint or CLI subcommand so auditors can prove the evidence
//     trail has not been tampered with post-emission.
//
//  3. Per-action token budget — runaway-motion guard composed from two
//     existing ports (DomainEventPublisher and RateLimiter) rather
//     than a new kernel feature. The same guard struct observes
//     EvidenceRecorded events to accumulate tokens per action, then
//     rejects new invocations once a configured limit is crossed.
//     This is why axi-go did NOT add per-action budgets to the kernel
//     in any 1.x release: the ports compose.
//
// Run: go run ./example/observability
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
)

// --- Pattern 1: DomainEventPublisher as strict-DDD subscriber ---

// loggingPublisher prints each event to stdout. A real adapter would
// fan out to Prometheus, OpenTelemetry, a Kafka outbox, or a SIEM —
// the plugin-author contract is unchanged: one interface, one method.
//
// Must be safe for concurrent use and MUST NOT block: the kernel calls
// Publish synchronously on the hot path of execution.
type loggingPublisher struct{}

func (loggingPublisher) Publish(event domain.DomainEvent) {
	switch ev := event.(type) {
	case domain.SessionStarted:
		fmt.Printf("  [event] session.started        session=%s action=%s\n", ev.SessionID, ev.ActionName)
	case domain.SessionCompleted:
		fmt.Printf("  [event] session.completed      session=%s action=%s status=%s duration=%s\n",
			ev.SessionID, ev.ActionName, ev.Status, ev.Duration.Round(time.Microsecond))
	case domain.CapabilityInvoked:
		fmt.Printf("  [event] capability.invoked     session=%s capability=%s outcome=%s duration=%s\n",
			ev.SessionID, ev.Capability, ev.Outcome, ev.Duration.Round(time.Microsecond))
	case domain.EvidenceRecorded:
		fmt.Printf("  [event] evidence.recorded      session=%s kind=%q tokens=%d hash=%s\n",
			ev.SessionID, ev.EvidenceKind, ev.Tokens, shortHash(ev.Hash))
	case domain.BudgetExceeded:
		fmt.Printf("  [event] budget.exceeded        session=%s kind=%s\n", ev.SessionID, ev.Kind)
	case domain.ResultChunkEmitted:
		fmt.Printf("  [event] result.chunk.emitted   session=%s index=%d kind=%q\n",
			ev.SessionID, ev.Chunk.Index, ev.Chunk.Kind)
	default:
		fmt.Printf("  [event] %s\n", ev.EventType())
	}
}

func shortHash(h domain.EvidenceHash) string {
	s := string(h)
	if len(s) <= 10 {
		return s
	}
	return s[:10] + "…"
}

// --- Pattern 3: token-budget guard composing DomainEventPublisher + RateLimiter ---

// tokenBudgetGuard enforces a per-action cumulative token cap across
// all invocations. It implements BOTH DomainEventPublisher (to observe
// EvidenceRecorded tokens) and RateLimiter (to reject new invocations
// once the cap is crossed). Wire one struct into both ports on the
// kernel and the halves coordinate through the shared state.
//
// This is the alternative to adding a cross-session budget to the
// kernel: adopters implement the policy they want (per-action,
// per-plugin prefix, sliding window, …) via existing ports.
type tokenBudgetGuard struct {
	mu      sync.Mutex
	limit   int64
	spent   map[domain.ActionName]int64
	tripped map[domain.ActionName]bool
}

func newTokenBudgetGuard(limit int64) *tokenBudgetGuard {
	return &tokenBudgetGuard{
		limit:   limit,
		spent:   map[domain.ActionName]int64{},
		tripped: map[domain.ActionName]bool{},
	}
}

// Publish is the DomainEventPublisher half. EvidenceRecorded events
// carry the action name and the tokens attributed to that record; we
// accumulate per-action totals and flip the tripped flag when a limit
// is crossed.
func (g *tokenBudgetGuard) Publish(event domain.DomainEvent) {
	ev, ok := event.(domain.EvidenceRecorded)
	if !ok || ev.Tokens <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.spent[ev.ActionName] += ev.Tokens
	if g.spent[ev.ActionName] >= g.limit {
		g.tripped[ev.ActionName] = true
	}
}

// Allow is the RateLimiter half. Once an action trips, new invocations
// fail fast at the rate-limit check — before the executor runs, before
// any further tokens are spent. Error text is inspected by operators
// and logs; structured error types are easy to add if needed.
func (g *tokenBudgetGuard) Allow(action domain.ActionName) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.tripped[action] {
		return fmt.Errorf(
			"token budget exhausted for %q: spent %d tokens, limit %d",
			action, g.spent[action], g.limit,
		)
	}
	return nil
}

// Spent exposes the per-action running total for the compliance report
// below. Real adopters expose this via an operator endpoint or a
// metrics sink.
func (g *tokenBudgetGuard) Spent() map[domain.ActionName]int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[domain.ActionName]int64, len(g.spent))
	for k, v := range g.spent {
		out[k] = v
	}
	return out
}

// --- A cheap plugin that reports tokens so the guard has something to observe ---

type chatPlugin struct{}

func (chatPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"chat.reply", "a chat-style action that reports tokens",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.chat")
	return domain.NewPluginContribution("chat.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

type chatExec struct {
	tokensPerCall int64
}

func (c *chatExec) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{
			Data:    map[string]any{"reply": "hello"},
			Summary: "chat reply",
		},
		[]domain.EvidenceRecord{{
			Kind:       "llm.completion",
			Source:     "chat.reply",
			Value:      map[string]any{"tokens": c.tokensPerCall},
			TokensUsed: c.tokensPerCall,
		}}, nil
}

// --- Main ---

func main() {
	guard := newTokenBudgetGuard(250) // low limit so the demo trips quickly
	publisher := &compositePublisher{children: []domain.DomainEventPublisher{loggingPublisher{}, guard}}

	kernel := axi.New().
		WithDomainEventPublisher(publisher).
		WithRateLimiter(guard)

	kernel.RegisterActionExecutor("exec.chat", &chatExec{tokensPerCall: 100})
	if err := kernel.RegisterPlugin(chatPlugin{}); err != nil {
		fatal("register: %v", err)
	}

	fmt.Println("=== pattern 1 + 3 : events flowing; token-budget guard composed ===")
	for i := 1; i <= 4; i++ {
		fmt.Printf("\n--- invocation %d ---\n", i)
		result, err := kernel.Execute(context.Background(), axi.Invocation{
			Action: "chat.reply",
			Input:  map[string]any{"prompt": "hi"},
		})
		if err != nil {
			fmt.Printf("  [kernel] rejected: %v\n", err)
			continue
		}
		fmt.Printf("  [kernel] status=%s tokens-spent-total=%d\n",
			result.Status, guard.Spent()["chat.reply"])
	}

	fmt.Println("\n=== pattern 2 : compliance endpoint — verify the evidence chain ===")
	// In a real service, this is what your /sessions/:id/verify HTTP
	// endpoint or `axi-admin verify <id>` CLI subcommand wraps.
	// All it does is call the aggregate method.
	session, err := kernel.GetSession("session-1")
	if err != nil {
		fatal("GetSession: %v", err)
	}
	if err := session.VerifyEvidenceChain(); err != nil {
		var chainErr *domain.ErrChainBroken
		if errors.As(err, &chainErr) {
			fmt.Printf("  FAIL: chain broken at record %d — %s\n", chainErr.Index, chainErr.Reason)
		} else {
			fmt.Printf("  FAIL: %v\n", err)
		}
	} else {
		fmt.Printf("  OK: session %q evidence chain intact (%d records)\n",
			session.ID(), len(session.Evidence()))
	}
}

// compositePublisher fans each event out to N child publishers. Handy
// when you want a log sink AND a metrics sink AND a budget guard all
// subscribing to the same stream, each for different reasons.
type compositePublisher struct {
	children []domain.DomainEventPublisher
}

func (c *compositePublisher) Publish(event domain.DomainEvent) {
	for _, child := range c.children {
		child.Publish(event)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
