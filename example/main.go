// Package main demonstrates axi-go embedded in a Go program.
//
// This is not a server — axi-go is a library. Run it to see the kernel
// exercise actions, capability composition, approval gates, and evidence.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
	"go.klarlabs.de/axi/inmemory"
	"go.klarlabs.de/axi/toon"
)

// --- Sample plugin: "greeter" ---

type greeterPlugin struct{}

func (p *greeterPlugin) Contribute() (*domain.PluginContribution, error) {
	capName, _ := domain.NewCapabilityName("string.upper")
	upperCap, _ := domain.NewCapabilityDefinition(capName, "Uppercases a string",
		domain.NewContract([]domain.ContractField{
			{Name: "text", Type: "string", Description: "Text to uppercase", Required: true, Example: "hello"},
		}),
		domain.EmptyContract(),
	)
	_ = upperCap.BindExecutor("exec.string.upper")

	actionName, _ := domain.NewActionName("greet")
	reqs, _ := domain.NewRequirementSet(domain.Requirement{Capability: capName})
	action, _ := domain.NewActionDefinition(
		actionName,
		"Greet someone by name, with their name uppercased",
		domain.NewContract([]domain.ContractField{
			{Name: "name", Type: "string", Description: "Person to greet", Required: true, Example: "world"},
		}),
		domain.NewContract([]domain.ContractField{
			{Name: "message", Type: "string", Description: "The greeting message", Required: true},
		}),
		reqs,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.greet")

	return domain.NewPluginContribution("greeter.plugin",
		[]*domain.ActionDefinition{action},
		[]*domain.CapabilityDefinition{upperCap},
	)
}

// --- Executors ---

type upperExecutor struct{}

func (e *upperExecutor) Execute(_ context.Context, input any) (any, error) {
	s, _ := input.(string)
	return strings.ToUpper(s), nil
}

type greetExecutor struct{}

func (e *greetExecutor) Execute(_ context.Context, input any, caps domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	m := input.(map[string]any)
	name, _ := m["name"].(string)

	upper, err := caps.Invoke("string.upper", name)
	if err != nil {
		return domain.ExecutionResult{}, nil, err
	}

	return domain.ExecutionResult{
			Data:        map[string]any{"message": fmt.Sprintf("Hello, %s!", upper)},
			Summary:     "Greeted " + name,
			ContentType: "application/json",
			// axi.md #9: guide the agent toward sensible follow-ups.
			Suggestions: []domain.Suggestion{
				{Action: "greet", Description: "Greet someone else"},
			},
		}, []domain.EvidenceRecord{
			// axi.md #1: capabilities report token usage for budget enforcement.
			{Kind: "invocation", Source: "greet", Value: map[string]any{"name": name, "upper": upper}, TokensUsed: 12},
		}, nil
}

func main() {
	// 1. Build the kernel with a fluent builder.
	//    Budget covers duration, invocation count, tokens, and idempotency retries.
	kernel := axi.New().
		WithLogger(inmemory.NewStdLogger(inmemory.LevelInfo)).
		WithBudget(axi.Budget{
			MaxCapabilityInvocations: 100,
			MaxTokens:                1_000,
			MaxRetries:               2,
		})

	// 2. Register executors, then the plugin metadata.
	kernel.RegisterActionExecutor("exec.greet", &greetExecutor{})
	kernel.RegisterCapabilityExecutor("exec.string.upper", &upperExecutor{})
	if err := kernel.RegisterPlugin(&greeterPlugin{}); err != nil {
		fmt.Fprintln(os.Stderr, "register:", err)
		os.Exit(1)
	}

	// 3. Discovery via the minimal Summary projection (axi.md #2).
	fmt.Println("=== Registered actions ===")
	summaries := kernel.ListActionSummaries()
	if summaries.IsEmpty() {
		fmt.Println("  (no actions registered)")
	}
	for _, s := range summaries.Items {
		fmt.Printf("  - %s (%s, idempotent=%t) — %s\n",
			s.Name, s.Effect, s.Idempotent, s.Description)
	}
	fmt.Printf("  total: %d\n\n", summaries.TotalCount)

	// 4. Unified help (axi.md #10).
	fmt.Println("=== Help for 'greet' ===")
	if help, err := kernel.Help("greet"); err == nil {
		fmt.Println(help)
	}
	fmt.Println()

	// 5. Execute the action.
	fmt.Println("=== Executing 'greet' ===")
	result, err := kernel.Execute(context.Background(), axi.Invocation{
		Action: "greet",
		Input:  map[string]any{"name": "world"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "execute:", err)
		os.Exit(1)
	}

	// 6. Inspect the result — JSON for comparison, TOON for agent-friendly output.
	fmt.Printf("Status: %s\n", result.Status)
	data, _ := json.MarshalIndent(result.Result.Data, "", "  ")
	fmt.Printf("Result (JSON):\n%s\n", data)

	if toonOut, err := toon.Encode(result.Result.Data); err == nil {
		fmt.Printf("Result (TOON):\n%s\n", toonOut)
	}

	fmt.Printf("Evidence: %d record(s), %d tokens reported\n",
		len(result.Evidence), sumTokens(result.Evidence))
	for _, ev := range result.Evidence {
		fmt.Printf("  - [%s] from %s: %v (tokens=%d)\n", ev.Kind, ev.Source, ev.Value, ev.TokensUsed)
	}

	// 7. Suggestions let the agent pick a sensible next move (axi.md #9).
	if len(result.Suggestions) > 0 {
		fmt.Println("Suggested next actions:")
		for _, s := range result.Suggestions {
			fmt.Printf("  -> %s — %s\n", s.Action, s.Description)
		}
	}
	fmt.Println()

	// 8. Poll the session.
	fmt.Println("=== Session state ===")
	session, _ := kernel.GetSession(string(result.SessionID))
	fmt.Printf("Session %s is %s\n", session.ID(), session.Status())
}

func sumTokens(evidence []domain.EvidenceRecord) int64 {
	var total int64
	for _, e := range evidence {
		total += e.TokensUsed
	}
	return total
}
