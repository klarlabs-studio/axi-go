package axi_test

import (
	"context"
	"fmt"

	"go.klarlabs.de/axi"
	"go.klarlabs.de/axi/domain"
)

// This file contains godoc examples that appear in 'go doc' and at
// pkg.go.dev. Each Example* function demonstrates one slice of the API. Keep
// them small, readable, and deterministic — the // Output: block runs them.

type exampleGreetExecutor struct{}

func (e *exampleGreetExecutor) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	m, _ := input.(map[string]any)
	name, _ := m["name"].(string)
	return domain.ExecutionResult{
		Data:    map[string]any{"message": "Hello, " + name},
		Summary: "greeted " + name,
		Suggestions: []domain.Suggestion{
			{Action: "greet", Description: "Greet another person"},
		},
	}, nil, nil
}

type exampleGreetPlugin struct{}

func (p *exampleGreetPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"greet",
		"Greets a person by name",
		domain.NewContract([]domain.ContractField{
			{Name: "name", Type: "string", Description: "Person to greet", Required: true, Example: "world"},
		}),
		domain.EmptyContract(),
		nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.greet")
	return domain.NewPluginContribution("godoc.plugin",
		[]*domain.ActionDefinition{action}, nil)
}

func ExampleKernel_Execute() {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.greet", &exampleGreetExecutor{})
	_ = kernel.RegisterPlugin(&exampleGreetPlugin{})

	result, _ := kernel.Execute(context.Background(), axi.Invocation{
		Action: "greet",
		Input:  map[string]any{"name": "world"},
	})
	data := result.Result.Data.(map[string]any)
	fmt.Println(data["message"])
	fmt.Println("status:", result.Status)
	// Output:
	// Hello, world
	// status: succeeded
}

func ExampleKernel_Help() {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.greet", &exampleGreetExecutor{})
	_ = kernel.RegisterPlugin(&exampleGreetPlugin{})

	help, _ := kernel.Help("greet")
	fmt.Println(help)
	// Output:
	// greet — Greets a person by name
	// Effect: none  Idempotent: true
	//
	// Input:
	//   name  (string, required)  Person to greet
	//     example: world
	//
	// Output:
	//   (no fields)
}

func ExampleKernel_ListActionSummaries() {
	kernel := axi.New()
	kernel.RegisterActionExecutor("exec.greet", &exampleGreetExecutor{})
	_ = kernel.RegisterPlugin(&exampleGreetPlugin{})

	summaries := kernel.ListActionSummaries()
	fmt.Printf("total: %d, empty: %t\n", summaries.TotalCount, summaries.IsEmpty())
	for _, s := range summaries.Items {
		fmt.Printf("  %s — %s (effect=%s, idempotent=%t)\n", s.Name, s.Description, s.Effect, s.Idempotent)
	}
	// Output:
	// total: 1, empty: false
	//   greet — Greets a person by name (effect=none, idempotent=true)
}

func ExampleTruncate() {
	short, truncated := axi.Truncate("hello", 20)
	fmt.Printf("%q truncated=%t\n", short, truncated)

	long, truncated := axi.Truncate("the quick brown fox jumps over the lazy dog", 10)
	fmt.Printf("%q truncated=%t\n", long, truncated)
	// Output:
	// "hello" truncated=false
	// "the quick … (truncated, 43 chars total)" truncated=true
}
