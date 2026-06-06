package jsonstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.klarlabs.de/axi/domain"
	"go.klarlabs.de/axi/jsonstore"
)

func TestActionStore_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store, err := jsonstore.NewActionStore(dir)
	if err != nil {
		t.Fatalf("NewActionStore: %v", err)
	}

	action, _ := domain.NewActionDefinition("greet", "Greet someone",
		domain.NewContract([]domain.ContractField{
			{Name: "name", Type: "string", Description: "Person to greet", Required: true},
		}),
		domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.greet")

	if err := store.Save(action); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetByName("greet")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name() != "greet" {
		t.Errorf("expected greet, got %s", got.Name())
	}
	if got.Description() != "Greet someone" {
		t.Errorf("expected description, got %s", got.Description())
	}
	if !got.IsBound() {
		t.Error("expected bound")
	}
	if got.InputContract().Fields[0].Type != "string" {
		t.Error("expected string type on input field")
	}
}

func TestActionStore_List(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewActionStore(dir)

	a1, _ := domain.NewActionDefinition("a", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a1.BindExecutor("e1")
	a2, _ := domain.NewActionDefinition("b", "B", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a2.BindExecutor("e2")

	_ = store.Save(a1)
	_ = store.Save(a2)

	list := store.List()
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestActionStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewActionStore(dir)

	a, _ := domain.NewActionDefinition("temp", "Temp", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a.BindExecutor("e")
	_ = store.Save(a)

	if err := store.Delete("temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.GetByName("temp"); err == nil {
		t.Error("expected not found after delete")
	}
}

func TestActionStore_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewActionStore(dir)

	_, err := store.GetByName("missing")
	if err == nil {
		t.Error("expected error")
	}
}

func TestCapabilityStore_SaveAndGet(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewCapabilityStore(dir)

	cap, _ := domain.NewCapabilityDefinition("http.get", "HTTP GET",
		domain.NewContract([]domain.ContractField{{Name: "url", Type: "string", Required: true}}),
		domain.EmptyContract(),
	)
	_ = cap.BindExecutor("exec.http")

	_ = store.Save(cap)
	got, err := store.GetByName("http.get")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Name() != "http.get" {
		t.Errorf("expected http.get, got %s", got.Name())
	}
}

func TestPluginStore_SaveExistsDelete(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewPluginStore(dir)

	a, _ := domain.NewActionDefinition("act", "A", domain.EmptyContract(), domain.EmptyContract(), nil, domain.EffectProfile{}, domain.IdempotencyProfile{})
	_ = a.BindExecutor("e")
	p, _ := domain.NewPluginContribution("test.plugin", []*domain.ActionDefinition{a}, nil)
	_ = p.Activate()

	_ = store.Save(p)

	if !store.Exists("test.plugin") {
		t.Error("expected exists")
	}

	_, err := store.GetByID("test.plugin")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	_ = store.Delete("test.plugin")
	if store.Exists("test.plugin") {
		t.Error("expected not exists after delete")
	}
}

func TestSessionStore_SaveAndGet_Pending(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s1", "greet", map[string]any{"name": "world"})

	if err := store.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get("s1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != "s1" {
		t.Errorf("expected s1, got %s", got.ID())
	}
	if got.ActionName() != "greet" {
		t.Errorf("expected greet, got %s", got.ActionName())
	}
	if got.Status() != domain.StatusPending {
		t.Errorf("expected pending, got %s", got.Status())
	}
}

func TestSessionStore_SaveAndGet_Succeeded(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s2", "greet", map[string]any{"name": "world"})
	_ = session.MarkValidated()
	_ = session.MarkResolved([]domain.CapabilityName{"string.upper"})
	_ = session.MarkRunning()
	session.AppendEvidence(domain.EvidenceRecord{Kind: "log", Source: "test", Value: "ran", Timestamp: 1234567890})
	_ = session.Succeed(domain.ExecutionResult{Data: "Hello!", Summary: "greeted", ContentType: "text/plain"})

	_ = store.Save(session)
	got, err := store.Get("s2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status() != domain.StatusSucceeded {
		t.Errorf("expected succeeded, got %s", got.Status())
	}
	if got.Result() == nil || got.Result().Data != "Hello!" {
		t.Errorf("expected result Hello!, got %v", got.Result())
	}
	if got.Result().ContentType != "text/plain" {
		t.Errorf("expected text/plain, got %s", got.Result().ContentType)
	}
	if len(got.Evidence()) != 1 || got.Evidence()[0].Timestamp != 1234567890 {
		t.Errorf("expected evidence with timestamp, got %v", got.Evidence())
	}
	if len(got.ResolvedCapabilities()) != 1 || got.ResolvedCapabilities()[0] != "string.upper" {
		t.Errorf("expected resolved capabilities, got %v", got.ResolvedCapabilities())
	}
}

func TestSessionStore_RoundTrip_TokensUsed(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s-tokens", "greet", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkRunning()
	session.AppendEvidence(domain.EvidenceRecord{Kind: "llm", Source: "model-a", TokensUsed: 42, Timestamp: 100})
	session.AppendEvidence(domain.EvidenceRecord{Kind: "llm", Source: "model-b", TokensUsed: 58, Timestamp: 200})
	_ = session.Succeed(domain.ExecutionResult{Data: "ok"})

	if err := store.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get("s-tokens")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	ev := got.Evidence()
	if len(ev) != 2 {
		t.Fatalf("expected 2 evidence records, got %d", len(ev))
	}
	var total int64
	for _, e := range ev {
		total += e.TokensUsed
	}
	if total != 100 {
		t.Errorf("TokensUsed round-trip failed: got sum %d, want 100", total)
	}
	if ev[0].TokensUsed != 42 || ev[1].TokensUsed != 58 {
		t.Errorf("per-record TokensUsed lost on round-trip: %v", ev)
	}
}

func TestSessionStore_RoundTrip_Suggestions(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s-sugg", "create", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkRunning()
	_ = session.Succeed(domain.ExecutionResult{
		Data: map[string]any{"id": "abc"},
		Suggestions: []domain.Suggestion{
			{Action: "resource.get", Description: "Retrieve the created resource"},
			{Action: "resource.list", Description: "List all resources"},
		},
	})

	if err := store.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get("s-sugg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	result := got.Result()
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(result.Suggestions))
	}
	if result.Suggestions[0].Action != "resource.get" {
		t.Errorf("Suggestions[0].Action = %q, want resource.get", result.Suggestions[0].Action)
	}
	if result.Suggestions[0].Description != "Retrieve the created resource" {
		t.Errorf("Suggestions[0].Description lost on round-trip: %q", result.Suggestions[0].Description)
	}
	if result.Suggestions[1].Action != "resource.list" {
		t.Errorf("Suggestions[1].Action = %q, want resource.list", result.Suggestions[1].Action)
	}
}

func TestSessionStore_RoundTrip_SchemaVersion(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s-schema", "noop", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkRunning()
	_ = session.Succeed(domain.ExecutionResult{Data: "ok"})

	if err := store.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read the raw snapshot from disk and verify the schema field is set.
	path := filepath.Join(dir, "sessions", "s-schema.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["schema"] != domain.CurrentSessionSchema {
		t.Errorf("schema = %v, want %q", parsed["schema"], domain.CurrentSessionSchema)
	}
}

func TestSessionStore_LoadsLegacySnapshotWithoutSchemaField(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Pre-schema snapshot (omits the schema field entirely).
	legacy := `{
		"id": "legacy-1",
		"action_name": "old-action",
		"input": null,
		"status": "succeeded",
		"requires_approval": false,
		"resolved_capabilities": [],
		"evidence": [],
		"result": {"data": "ok", "summary": ""}
	}`
	if err := os.WriteFile(filepath.Join(sessionsDir, "legacy-1.json"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	store, _ := jsonstore.NewSessionStore(dir)
	got, err := store.Get("legacy-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status() != domain.StatusSucceeded {
		t.Errorf("status = %s, want succeeded", got.Status())
	}
}

func TestSessionStore_RoundTrip_ApprovalDecision(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s-approval", "send", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkAwaitingApproval()
	_ = session.Approve(domain.ApprovalDecision{Principal: "alice@example.com", Rationale: "vetted the request"})

	if err := store.Save(session); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get("s-approval")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	dec := got.ApprovalDecision()
	if dec == nil {
		t.Fatal("expected ApprovalDecision, got nil")
	}
	if dec.Principal != "alice@example.com" {
		t.Errorf("Principal = %q, want alice@example.com", dec.Principal)
	}
	if dec.Rationale != "vetted the request" {
		t.Errorf("Rationale lost on round-trip: %q", dec.Rationale)
	}
}

func TestSessionStore_SaveAndGet_Failed(t *testing.T) {
	dir := t.TempDir()
	store, _ := jsonstore.NewSessionStore(dir)

	session, _ := domain.NewExecutionSession("s3", "fail", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkRunning()
	_ = session.Fail(domain.FailureReason{Code: "ERR", Message: "boom"})

	_ = store.Save(session)
	got, _ := store.Get("s3")

	if got.Status() != domain.StatusFailed {
		t.Errorf("expected failed, got %s", got.Status())
	}
	if got.Failure() == nil || got.Failure().Code != "ERR" {
		t.Errorf("expected failure reason, got %v", got.Failure())
	}
}
