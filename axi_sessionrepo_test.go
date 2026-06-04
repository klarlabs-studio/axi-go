package axi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/felixgeelhaar/axi-go"
	"github.com/felixgeelhaar/axi-go/domain"
)

// snapStore is a durable SessionRepository that persists sessions as JSON
// snapshots — the shape a PostgreSQL adapter takes. It uses only the public
// ToSnapshot / SessionFromSnapshot API.
type snapStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newSnapStore() *snapStore { return &snapStore{data: map[string][]byte{}} }

func (s *snapStore) Save(session *domain.ExecutionSession) error {
	b, err := json.Marshal(session.ToSnapshot())
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.data[string(session.ID())] = b
	s.mu.Unlock()
	return nil
}

func (s *snapStore) Get(id domain.ExecutionSessionID) (*domain.ExecutionSession, error) {
	s.mu.Lock()
	b, ok := s.data[string(id)]
	s.mu.Unlock()
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "session", ID: string(id)}
	}
	var snap domain.SessionSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	return domain.SessionFromSnapshot(snap)
}

// notifyPlugin contributes a write-external action that pauses for approval.
type notifyPlugin struct{}

func (notifyPlugin) Contribute() (*domain.PluginContribution, error) {
	action, _ := domain.NewActionDefinition(
		"notify", "Send a regulatory notification",
		domain.NewContract([]domain.ContractField{{Name: "to", Type: "string", Required: true}}),
		domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectWriteExternal},
		domain.IdempotencyProfile{},
	)
	_ = action.BindExecutor("exec.notify")
	return domain.NewPluginContribution("notify.plugin", []*domain.ActionDefinition{action}, nil)
}

type notifyExec struct{}

func (notifyExec) Execute(_ context.Context, input any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	to := input.(map[string]any)["to"]
	return domain.ExecutionResult{Summary: fmt.Sprintf("notified %v", to)},
		[]domain.EvidenceRecord{{Kind: "notification.sent", Source: "notify.plugin", Value: map[string]any{"to": to}}}, nil
}

func newKernel(store domain.SessionRepository) *axi.Kernel {
	k := axi.New().WithSessionRepository(store)
	k.RegisterActionExecutor("exec.notify", notifyExec{})
	_ = k.RegisterPlugin(notifyPlugin{})
	return k
}

// TestWithSessionRepository_SurvivesRestart proves a write-external session that
// paused at AwaitingApproval can be approved by a *different* kernel instance
// sharing the same durable repository — i.e. it survives a process restart.
func TestWithSessionRepository_SurvivesRestart(t *testing.T) {
	ctx := context.Background()
	store := newSnapStore()

	// Kernel A: submit a write-external action; it pauses for approval and is
	// persisted to the durable store.
	kernelA := newKernel(store)
	res, err := kernelA.Execute(ctx, axi.Invocation{Action: "notify", Input: map[string]any{"to": "Lead DPA"}})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.RequiresApproval {
		t.Fatal("expected the action to pause for approval")
	}
	sessionID := string(res.SessionID)

	// Confirm it really is persisted in the durable store.
	if _, err := store.Get(domain.ExecutionSessionID(sessionID)); err != nil {
		t.Fatalf("session not persisted: %v", err)
	}

	// Simulate a process restart: a brand-new kernel with the same store and the
	// same registered action/executor. Kernel A is discarded.
	kernelB := newKernel(store)

	final, err := kernelB.Approve(ctx, sessionID, domain.ApprovalDecision{
		Principal: "officer@tenant.example",
		Rationale: "incident confirmed reportable",
	})
	if err != nil {
		t.Fatalf("approve after restart: %v", err)
	}
	if string(final.Status) != "succeeded" {
		t.Fatalf("status after approval = %q, want succeeded", final.Status)
	}

	// The reloaded session's evidence chain must still verify.
	session, err := kernelB.GetSession(sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if err := session.VerifyEvidenceChain(); err != nil {
		t.Fatalf("evidence chain broken after restart+approval: %v", err)
	}
}
