// Package main is the axi-go 60-second tour — the program embedded in
// README.md. A single write-external action pauses for approval, runs,
// and lands in a tamper-evident evidence trail; a DomainEventPublisher
// prints every lifecycle transition as it happens.
//
// Run: go run ./example/quickstart
package main

import (
	"context"
	"fmt"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
)

// A single action: send-email. Effect is write-external, so axi-go
// pauses at awaiting_approval before the executor ever runs.
type emailPlugin struct{}

func (emailPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"send-email", "Send an email",
		domain.NewContract([]domain.ContractField{{
			Name: "to", Type: "string", Required: true, Description: "Recipient",
		}}),
		domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectWriteExternal},
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

// Any type that implements Publish(DomainEvent) subscribes to the
// kernel's full lifecycle — wire one into WithDomainEventPublisher
// and fan-out to metrics, audit logs, Kafka, etc.
type logEvents struct{}

func (logEvents) Publish(e domain.DomainEvent) {
	fmt.Printf("  event → %s\n", e.EventType())
}

func main() {
	kernel := axi.New().WithDomainEventPublisher(logEvents{})
	kernel.RegisterActionExecutor("exec.email", emailExec{})
	_ = kernel.RegisterPlugin(emailPlugin{})

	ctx := context.Background()

	// 1. Execute. Because send-email is write-external, the session
	//    pauses at awaiting_approval — the executor does NOT run yet.
	fmt.Println("→ kernel.Execute")
	result, _ := kernel.Execute(ctx, axi.Invocation{
		Action: "send-email",
		Input:  map[string]any{"to": "alice@example.com"},
	})
	fmt.Printf("  status = %s\n\n", result.Status)

	// 2. A human (or a policy bot) approves. The kernel resumes the
	//    paused session and runs the executor.
	fmt.Println("→ kernel.Approve")
	final, _ := kernel.Approve(ctx, string(result.SessionID), domain.ApprovalDecision{
		Principal: "ops@example.com",
		Rationale: "recipient verified",
	})
	fmt.Printf("  status = %s\n\n", final.Status)

	// 3. Evidence trail. Each record carries a SHA-256 hash chained
	//    to the previous record; VerifyEvidenceChain proves the trail
	//    hasn't been mutated since recording.
	fmt.Println("→ audit")
	session, _ := kernel.GetSession(string(result.SessionID))
	for _, ev := range session.Evidence() {
		fmt.Printf("  evidence: kind=%s hash=%.10s…\n", ev.Kind, string(ev.Hash))
	}
	if err := session.VerifyEvidenceChain(); err != nil {
		fmt.Println("  chain: BROKEN —", err)
	} else {
		fmt.Println("  chain: intact")
	}
}
