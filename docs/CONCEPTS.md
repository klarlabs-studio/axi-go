# Concepts

This document explains the *why* behind axi-go's design — the reasoning that
makes certain shapes inevitable once you accept certain premises. For the
*what* (API surface, runnable code, feature list), see the
[README](../README.md). For the *how* (task recipes), the README has
a how-to section for each agent-facing feature.

This page exists because the right mental model matters more than any single
API. Get the concepts right and everything else follows.

---

## The problem

Give an AI agent a bag of function calls (`search`, `send_email`, `run_sql`)
and three classes of problem appear within a week in production:

1. **Safety.** The agent can call `send_email` a thousand times before anyone
   notices. There is no gate between the model's next token and the outside
   world.
2. **Audit.** When something goes wrong, nobody can answer "what did the
   agent do, when, why, and on whose authority?" without trawling logs that
   weren't designed for the question.
3. **Reliability.** One transient failure in step 3 of a 5-step flow
   invalidates steps 1 and 2. There is no retry, no compensation, no
   partial-completion semantics.

Every tool-calling framework solves the *ergonomics* (give the model a
function, call the function). Few solve the *kernel* (make the function
call safe, auditable, and reliable by construction).

axi-go is that kernel.

---

## Two vocabularies: intent and mechanics

The most important distinction in the library:

- **Actions** express *intent*. "Send an email." "Greet a user." "Create a
  resource." The agent reasons in action-space because that's the space
  where business decisions live.
- **Capabilities** express *mechanics*. "SMTP send." "HTTP GET." "String
  uppercase." These are the reusable building blocks. An action declares
  which capabilities it needs; the kernel resolves them.

The split is not cosmetic. It's what makes effect profiles, approval gates,
and capability budgets possible — all of those live on actions, because
they're intent-level concerns. A capability has no opinion on whether it
should be approved; the action does.

When you describe an axi-go plugin in one sentence, the grammar is always:
*"Action X uses capabilities Y and Z to achieve W."* If that sentence
doesn't parse, the design is wrong.

---

## Effect profiles are a trust boundary

Every action declares an `EffectProfile.Level`:

| Level              | Meaning                                | Approval? |
|--------------------|----------------------------------------|-----------|
| `none`             | Pure computation (no I/O)              | No        |
| `read-local`       | Reads in-process state                 | No        |
| `write-local`      | Mutates in-process state               | No        |
| `read-external`    | Reads an external system (HTTP, DB)    | No        |
| `write-external`   | Mutates an external system             | **Yes**   |

The kernel treats `write-external` as the trust boundary between "things
the agent decided" and "things that commit to the world." Actions at that
level pause at `AwaitingApproval` until a caller invokes `Kernel.Approve`
with a `domain.ApprovalDecision{Principal, Rationale}`. The principal is
type-required non-empty — you cannot approve anonymously.

Why this specific taxonomy? Because these are the levels at which **blast
radius changes by an order of magnitude**. A `read-local` bug loses a
request. A `write-local` bug corrupts an in-process cache. A
`read-external` bug leaks a query. A `write-external` bug sends a thousand
emails. The taxonomy maps 1:1 to how bad the worst case can be.

The taxonomy does *not* distinguish "reads private data" from "reads public
data" — the data-sensitivity axis is intentionally out of scope. That's a
different concern (governed by where you deploy the kernel, what plugins
you register, and what your delivery adapter filters), and mixing it into
the effect profile would create a two-dimensional matrix that nobody would
use correctly.

---

## Evidence is the audit trail

Every execution produces an `ExecutionSession`. The session is the unit of
audit. Inside it, `Evidence []EvidenceRecord` is the append-only log of
what happened: each capability invocation, each token consumption, each
pipeline compensation. When a question comes in — "what did the agent do,
when, why, and on whose authority?" — the session answers all four.

Two consequences follow from this design:

1. **Evidence emission is a first-class responsibility of action
   executors.** If an action executor forgets to return an
   `EvidenceRecord`, the audit trail has a hole. The `Pipeline` type
   surfaces this automatically — `PipelineFailure.Evidence()` returns the
   records an action executor can append with one line.

2. **The token budget is enforced against reported evidence, not against
   actual token consumption.** A malicious plugin can forge
   `TokensUsed: 0` *at emission time*. This is a documented trust
   boundary — plugins are trusted code in your deployment, so what they
   report at `AppendEvidence` is what the kernel counts. Registration
   is trust.

3. **Post-emission tampering is detected.** As of axi-go 1.1, the
   evidence trail is a tamper-evident hash chain: each
   `EvidenceRecord` carries a SHA-256 `Hash` computed from its canonical
   form plus the previous record's `Hash`. The `ExecutionSession`
   aggregate assigns these hashes at `AppendEvidence` time — plugins
   cannot forge them. Call `session.VerifyEvidenceChain()` after loading
   a session to detect any mutation of persisted evidence; it returns
   `*ErrChainBroken` pointing at the first offending record. Legacy
   records (empty `Hash`, e.g. from pre-1.1 snapshots) are treated as
   unverifiable, not broken, so existing persisted sessions continue
   to load cleanly.

---

## Why the state machine is strict

`ExecutionSession.Status` moves through a fixed graph:

```
Pending → Validated → Resolved → [AwaitingApproval] → Running → Succeeded | Failed | Rejected
```

Every transition is guarded. You cannot mark a session `Running` without
first marking it `Resolved`. You cannot `Fail` a `Pending` session. The
transitions are type-enforced: `session.Approve(...)` only works on
`AwaitingApproval`; `session.Succeed(...)` only works on `Running`.

The strict graph is not about elegance. It's about **making the "what
happened?" question answerable from the status alone**. If you see a
session in `Failed`, you know its `Failure()` is populated. If you see it
in `Succeeded`, you know its `Result()` is populated. If you see it in
`AwaitingApproval`, you know `RequiresApproval()` is true and someone
still needs to decide.

A loose state machine would let you reach the same states by different
paths and force every audit query to do case analysis. A strict state
machine collapses the paths.

---

## Why `domain/` has zero external imports

The library has no runtime dependencies beyond the Go standard library.
This is a costly constraint — no UUIDs, no high-precision rate limiting,
no tracing library integration — and it's paid for three reasons:

1. **Supply chain.** Every import is a potential vulnerability vector. A
   kernel whose job is safety cannot afford a bad transitive dependency.
2. **Audit.** Zero deps means an adopter's legal and security teams can
   review the entire dependency tree in an hour.
3. **Adoption.** `go get go.klarlabs.de/axi` with no
   follow-up is a different adoption experience from `go get ...` followed
   by vetting 12 transitive packages.

The concrete design pattern: **port interfaces in the domain, adapters
outside.** `domain.Logger`, `domain.RateLimiter`, `domain.ContractValidator`
are all interfaces. `inmemory/` and `jsonstore/` provide default
implementations. An adopter who wants Prometheus metrics defines their own
implementation of a (future) `domain.MetricsReporter`, and the library
itself never imports `prometheus/client_golang`.

---

## Why pipelines have saga semantics

A `Pipeline` is a composable capability that chains steps sequentially:
step 2 receives step 1's output, step 3 receives step 2's output, etc.
When step 3 of a 5-step pipeline fails, you have three choices:

1. **Fail hard.** Discard steps 1 and 2, return an error. Simple. Useless
   for any pipeline that commits side effects.
2. **Leak partial state.** Return whatever you have. The caller reasons
   about "is this a full result or a partial result?" forever.
3. **Compensate.** Call the Compensate hook (if defined) on steps 2 and 1
   in reverse, then return a structured failure that describes what was
   rolled back.

axi-go chose #3. Every completed step's Compensate hook runs in reverse
order, compensation errors are collected separately from the root cause,
and the whole saga surfaces as evidence via `PipelineFailure.Evidence()`.
This is saga-lite — full distributed sagas have more machinery (local
transactions, compensating transactions with at-least-once semantics, durable
event logs) that a single-process library cannot and should not provide.
What it does provide is the *shape* of saga semantics in-process, so
adopters who later grow into distributed sagas have a migration path that
doesn't invalidate their existing compensators.

---

## When axi-go is the wrong choice

The library is wrong for:

- **LLM orchestration frameworks** (LangChain, LlamaIndex, Haystack).
  axi-go is the kernel under a tool call, not the workflow engine that
  decides which tool to call.
- **Pure agent loops without tools.** If your agent only talks to a
  model, axi-go has no role.
- **Synchronous request/response systems that don't need approval,
  retries, or audit.** You'd be paying for a state machine you don't use.
- **Sub-millisecond latency.** The kernel's mutex-protected aggregates,
  defensive slice copies, and post-hoc budget enforcement add microseconds
  to every Invoke. Fine for agent latency budgets; wrong for a trading
  system.

If your problem is in one of those categories, use something else. If your
problem is "my agent's tools need to be safe, auditable, and survive
transient failure," read the README next.

---

## Further reading

- **[axi.md](https://axi.md/)** — the 10 design principles for agent-tool
  interfaces. axi-go addresses the eight that apply to a library
  (principles 7 and 8 are CLI-specific).
- **[Issue #8](https://github.com/klarlabs-studio/axi-go/issues/8)** — the
  closed issue that tracked axi.md alignment.
- **[CHANGELOG.md](../CHANGELOG.md)** — what has shipped, grouped by
  theme.
