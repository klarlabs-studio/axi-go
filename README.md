# axi-go

**A domain-driven execution kernel for AI agent tools — a Go library you embed, not a service you run.**

[![CI](https://github.com/klarlabs-studio/axi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/klarlabs-studio/axi-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/go.klarlabs.de/axi)](https://goreportcard.com/report/go.klarlabs.de/axi)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/go.klarlabs.de/axi.svg)](https://pkg.go.dev/go.klarlabs.de/axi)

**Zero external dependencies.** Standard library only.

---

## Why axi-go?

When you give an AI agent a bag of tools (`search`, `send_email`, `run_sql`), you quickly hit these problems:

- **No safety** — the agent can call `send_email` a thousand times before you know it
- **No audit trail** — you can't explain *why* the agent did what it did
- **Tool sprawl** — 200 raw functions, no grouping, no dependencies, no lifecycle
- **No type information** — the agent has to guess what inputs each tool accepts
- **No approval gates** — the agent can take irreversible actions autonomously

**axi-go solves this** with a two-layer model:

| Layer | Example | Answers |
|-------|---------|---------|
| **Actions** | `greet`, `send-email`, `search-docs` | *What* the agent wants to do (intent) |
| **Capabilities** | `string.upper`, `http.get`, `db.query` | *How* it gets done (mechanics) |

An action declares the capabilities it needs. axi-go resolves them, validates inputs against typed contracts, enforces effect profiles (read-only? writes? external?), pauses for human approval when required, runs within execution budgets, and produces a structured audit trail.

You embed axi-go in your Go program. It has **no HTTP API, no daemon, no protocol assumptions** — those are delivery concerns for you to choose (HTTP, gRPC, CLI, MCP, whatever fits your stack).

## What you get

Every capability below is in the kernel today — no optional module, no extra dependency, no vendor lock-in:

- **Effect-gated approval.** Actions declare their side-effect level (`none`, `read-local`, `write-local`, `read-external`, `write-external`). The kernel pauses any `write-external` action at `awaiting_approval` until a human approves via `kernel.Approve`. Typo catching an agent about to mass-email? Caught before the executor runs.
- **Tamper-evident evidence trail.** Every `EvidenceRecord` appended to a session carries a SHA-256 `Hash` chained to the previous record. `session.VerifyEvidenceChain()` detects any post-emission mutation — your audit log is cryptographically replay-safe for free.
- **Domain events stream.** Implement `domain.DomainEventPublisher` once and subscribe to every lifecycle transition: session started/completed, capability invoked/retried, budget exceeded, evidence recorded. Fan it out to Prometheus, OpenTelemetry, Kafka, a SIEM — the plugin contract is one method.
- **Streaming results.** `StreamingActionExecutor` (optional companion to `ActionExecutor`) emits `ResultChunk` value objects progressively — LLM tokens, large-file reads, row-stream queries — while the kernel stamps monotonic indices under its mutex. Your HTTP/SSE or MCP-SSE adapter forwards chunks as they're produced.
- **Composition via `ActionInvoker`.** Plugin code can invoke other registered actions through `OrchestratorActionExecutor` — the primitive that lets sagas, fan-out/fan-in, and pipeline-of-actions ship as plugins without pulling a durable-log backend into axi-go core.
- **Budgets, rate limits, idempotency, output contracts, TOON encoding, truncation, help, suggestions.** Table further down.

Composing these, not reinventing them in every agent service, is the whole pitch.

## Install

```bash
go get go.klarlabs.de/axi
```

## 60-Second Tour

A single write-external action. The kernel pauses for approval, runs after the human signs off, and exits with a verified-intact evidence trail — while a subscriber prints every lifecycle event. Full runnable source at [`example/quickstart/`](example/quickstart/); `go run ./example/quickstart` reproduces the output below.

```go
package main

import (
    "context"
    "fmt"

    "go.klarlabs.de/axi"
    "go.klarlabs.de/axi/domain"
)

type emailPlugin struct{}

func (emailPlugin) Contribute() (*domain.PluginContribution, error) {
    action, _ := domain.NewActionDefinition(
        "send-email", "Send an email",
        domain.NewContract([]domain.ContractField{{
            Name: "to", Type: "string", Required: true, Description: "Recipient",
        }}),
        domain.EmptyContract(), nil,
        domain.EffectProfile{Level: domain.EffectWriteExternal}, // → approval gate
        domain.IdempotencyProfile{IsIdempotent: false},
    )
    _ = action.BindExecutor("exec.email")
    return domain.NewPluginContribution("email.plugin",
        []*domain.ActionDefinition{action}, nil)
}

type emailExec struct{}

func (emailExec) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
    to := input.(map[string]any)["to"].(string)
    return domain.ExecutionResult{Summary: "sent email to " + to},
        []domain.EvidenceRecord{{
            Kind:   "smtp.delivered",
            Source: "email.plugin",
            Value:  map[string]any{"to": to, "message_id": "msg-42"},
        }}, nil
}

type logEvents struct{}

func (logEvents) Publish(e domain.DomainEvent) { fmt.Printf("  event → %s\n", e.EventType()) }

func main() {
    kernel := axi.New().WithDomainEventPublisher(logEvents{})
    kernel.RegisterActionExecutor("exec.email", emailExec{})
    _ = kernel.RegisterPlugin(emailPlugin{})

    ctx := context.Background()

    // 1) Execute. write-external → kernel pauses before the executor runs.
    result, _ := kernel.Execute(ctx, axi.Invocation{
        Action: "send-email",
        Input:  map[string]any{"to": "alice@example.com"},
    })
    fmt.Println("after Execute:", result.Status) // awaiting_approval

    // 2) Human (or policy bot) approves. Kernel resumes the session.
    final, _ := kernel.Approve(ctx, string(result.SessionID), domain.ApprovalDecision{
        Principal: "ops@example.com",
        Rationale: "recipient verified",
    })
    fmt.Println("after Approve:", final.Status) // succeeded

    // 3) Audit. Each evidence record carries a SHA-256 hash chained to
    //    the previous. VerifyEvidenceChain proves the trail is intact.
    session, _ := kernel.GetSession(string(result.SessionID))
    for _, ev := range session.Evidence() {
        fmt.Printf("  evidence: kind=%s hash=%.10s…\n", ev.Kind, string(ev.Hash))
    }
    if err := session.VerifyEvidenceChain(); err == nil {
        fmt.Println("  chain: intact")
    }
}
```

Output:

```
  event → session.started
  event → session.awaiting_approval
after Execute: awaiting_approval
  event → evidence.recorded
  event → session.completed
after Approve: succeeded
  evidence: kind=smtp.delivered hash=4a70e78708…
  chain: intact
```

Four primitives in one program: effect-gated approval, an evidence record with its tamper-evident hash, `VerifyEvidenceChain()` confirming the trail, and a `DomainEventPublisher` subscriber printing every lifecycle transition. That's the whole 1.x value proposition, compressed.

## More examples

- [`example/main.go`](example/main.go) — fuller plugin showing capability composition, suggestions, TOON, retries.
- [`example/mcp-server/`](example/mcp-server/) — an MCP (Model Context Protocol) adapter in ~250 lines, no external deps.
- [`example/observability/`](example/observability/) — adoption templates for `DomainEventPublisher` as a strict-DDD subscriber, evidence-chain verification as an operator endpoint, and a per-action token-budget guard that composes `DomainEventPublisher` and `RateLimiter` instead of needing a new kernel feature.

To understand the *why* — the reasoning that makes actions, capabilities, effect profiles, and evidence inevitable once you accept certain premises — read [`docs/CONCEPTS.md`](docs/CONCEPTS.md). For versioning commitments and deprecation policy, see [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Configuring a kernel

The fluent builder on `axi.New()` returns a configured `*Kernel`. Chain
the `With*` methods as needed:

```go
kernel := axi.New().
    WithLogger(logger).
    WithBudget(axi.Budget{MaxDuration: 5*time.Minute, MaxCapabilityInvocations: 100}).
    WithRateLimiter(myRateLimiter).
    WithIDGenerator(uuidGen)
```

Register plugins and executors before the first `Execute`:

```go
kernel.RegisterPlugin(plugin)
kernel.RegisterBundle(bundle)  // atomic: metadata + executors together
```

Drive actions from your delivery layer:

```go
result, _ := kernel.Execute(ctx, axi.Invocation{Action: "greet", Input: inp})

// For write-external actions that paused at awaiting_approval:
result, _ := kernel.Approve(ctx, sessionID, decision)
result, _ := kernel.Reject(ctx, sessionID, decision)
```

## Kernel reference (quick)

| Method | Purpose |
|---|---|
| `New()` | Build a kernel with default in-memory adapters |
| `WithLogger`, `WithBudget`, `WithRateLimiter`, `WithIDGenerator`, `WithTimeout` | Fluent configuration |
| `RegisterPlugin`, `RegisterPluginWithConfig`, `RegisterBundle` | Add actions + capabilities |
| `RegisterActionExecutor`, `RegisterCapabilityExecutor` | Bind refs to implementations |
| `DeregisterPlugin` | Remove a plugin and everything it contributed |
| `Execute`, `ExecuteAsync` | Invoke an action synchronously or in the background |
| `Approve`, `Reject` | Resolve a session awaiting approval |
| `GetSession` | Look up a session by id |
| `ListActions`, `ListCapabilities` | Full aggregates |
| `ListActionsResult`, `ListCapabilitiesResult` | Aggregates wrapped with `TotalCount` + `IsEmpty()` |
| `ListActionSummaries`, `ListCapabilitySummaries` | Minimal-schema projections (axi.md #2) |
| `GetAction`, `Help` | Introspect one action or any name (axi.md #10) |

See the godoc on [pkg.go.dev](https://pkg.go.dev/go.klarlabs.de/axi)
for full signatures and runnable examples.

## Safety & Control

| Feature | What it does |
|---------|--------------|
| **Effect profiles** | `none`, `read-local`, `write-local`, `read-external`, `write-external` |
| **Approval gate** | `write-external` actions pause at `awaiting_approval` — call `kernel.Approve(...)` |
| **Execution budgets** | Max duration, max capability invocations, max tokens, and idempotency-gated retries per session |
| **Rate limiting** | Pluggable `RateLimiter` checked before each execution |
| **Output validation** | Results validated against output contracts before `succeeded` |
| **Idempotency profile** | Actions declare whether they're safe to retry |
| **Evidence trail** | Append-only `EvidenceRecord`s with timestamps — full audit log |
| **Pipeline saga** | Mid-pipeline failures return a `*PipelineFailure` with partial outputs and run any `PipelineStep.Compensate` hooks in reverse order |

## Agent-facing output

axi-go draws design cues from [axi.md](https://axi.md/) — a set of principles
for agent-tool interfaces optimized for token efficiency and discoverability.

### Suggestions (axi.md #9)

Actions can emit next-step hints in their result. The agent reads them and
avoids guessing what to call next:

```go
return domain.ExecutionResult{
    Data:    map[string]any{"id": "abc-123"},
    Summary: "created resource abc-123",
    Suggestions: []domain.Suggestion{
        {Action: "resource.get", Description: "Retrieve the created resource"},
        {Action: "resource.list", Description: "List all resources"},
    },
}, nil, nil
```

### TOON encoding (axi.md #1)

The `toon` package encodes results in Token-Optimized Object Notation —
brace-free, quote-free, and ~40% shorter than equivalent JSON on uniform
arrays:

```go
import "go.klarlabs.de/axi/toon"

out, _ := toon.Encode(map[string]any{
    "issues": []any{
        map[string]any{"number": 42, "state": "open", "title": "Fix login bug"},
        map[string]any{"number": 43, "state": "open", "title": "Add dark mode"},
    },
})
// issues[2]{number,state,title}:
//   42,open,Fix login bug
//   43,open,Add dark mode
```

### Token budget (axi.md #1)

Capabilities report token usage via `EvidenceRecord.TokensUsed`; the kernel
sums them and fails the session if the budget is exceeded:

```go
kernel := axi.New().WithBudget(axi.Budget{MaxTokens: 10_000})
// A session whose evidence sums to more than 10k tokens fails with
// FailureReason.Code = "BUDGET_EXCEEDED".
```

### Truncation (axi.md #3)

`axi.Truncate` caps strings and appends a size hint so context windows stay
bounded without silently dropping data:

```go
out, truncated := axi.Truncate(longBody, 500)
// "…first 500 chars… (truncated, 2847 chars total)"
```

### Minimal schemas and empty states (axi.md #2, #5)

`Kernel.ListActionSummaries` and `Kernel.ListCapabilitySummaries` return a
discovery-oriented projection (name, description, effect/idempotency for
actions) instead of full aggregates. All list responses share the
`ListResult[T]` shape with `TotalCount` and `IsEmpty()` so callers can
distinguish "no results" from "not queried":

```go
r := kernel.ListActionSummaries()
if r.IsEmpty() {
    fmt.Println("no actions registered")
}
for _, s := range r.Items {
    fmt.Printf("  %s  (%s, idempotent=%t) — %s\n",
        s.Name, s.Effect, s.Idempotent, s.Description)
}
```

### Help (axi.md #10)

`ActionDefinition.Help()` and `CapabilityDefinition.Help()` return a
formatted reference with contracts and capability requirements.
`Kernel.Help(name)` looks up the name as an action first, then as a
capability — a consistent fallback when contextual suggestions aren't
enough:

```go
text, _ := kernel.Help("greet")
// greet — Greet someone by name
// Effect: none  Idempotent: true
//
// Input:
//   name  (string, required)  Person to greet
//     example: world
// ...
```

## Persistence

Two adapters included. Pick one, or implement the repository interfaces in `domain/` for Postgres, SQLite, Redis, etc.

| Adapter | Package | Use for |
|---------|---------|---------|
| In-memory | `inmemory/` | Tests, single-process, ephemeral |
| JSON files | `jsonstore/` | Small deployments, simple persistence |

By default, `axi.New()` uses `inmemory/`. Swap the repositories by implementing the 4 ports in `domain/`: `ActionRepository`, `CapabilityRepository`, `PluginRepository`, `SessionRepository`.

## Architecture

axi-go is built with strict [Domain-Driven Design](https://www.domainlanguage.com/ddd/):

```
axi (root)       Fluent SDK facade — what you import.
domain/          Aggregates, services, port interfaces. Zero deps.
application/     Use cases that orchestrate the domain.
inmemory/        In-memory adapters + StdLogger.
jsonstore/       File-based JSON persistence adapter.
example/         Working sample plugin.
```

**Dependency direction**: `domain` ← `application` ← `inmemory`/`jsonstore` ← `axi` ← your code

The domain has no external imports and no knowledge of JSON, HTTP, or any delivery mechanism. All port interfaces live in `domain/`.

## Building a delivery adapter

axi-go is a kernel. If you need HTTP, gRPC, MCP, or a CLI, build it as a thin adapter on top:

```go
// Your HTTP handler (you own this, it's not in axi-go)
func executeHandler(kernel *axi.Kernel) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req ExecuteRequest
        _ = json.NewDecoder(r.Body).Decode(&req)

        result, err := kernel.Execute(r.Context(), axi.Invocation{
            Action: req.Action, Input: req.Input,
        })
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        _ = json.NewEncoder(w).Encode(result)
    }
}
```

An MCP server adapter, a gRPC service, or a Cobra CLI would all follow the same pattern: translate protocol → kernel calls → translate response.

## Development

```bash
make check          # Full suite: fmt + lint + test + security
make test           # Run tests
make lint           # golangci-lint
make fmt            # Auto-fix formatting
make install-hooks  # Install pre-commit git hook
go test ./... -race # Race detector
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines and [CLAUDE.md](CLAUDE.md) for a deeper architecture reference.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
