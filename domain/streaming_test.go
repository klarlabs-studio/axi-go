package domain_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.klarlabs.de/axi/domain"
)

// Fakes (fakeActionRepo, fakeCapRepo, fakeValidator, fakeActionExecLookup,
// fakeCapExecLookup) are declared in services_test.go and reused here.

// --- Session.Emit ---

func TestSessionEmit_StampsMonotonicIndex(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-emit", "act", nil)
	// Executor pre-sets Index to 99 — the aggregate must overwrite.
	s.Emit(domain.ResultChunk{Index: 99, Kind: "text", Data: "hello"})
	s.Emit(domain.ResultChunk{Index: 99, Kind: "text", Data: "world"})

	chunks := s.ResultChunks()
	if len(chunks) != 2 {
		t.Fatalf("chunks length = %d, want 2", len(chunks))
	}
	if chunks[0].Index != 0 {
		t.Errorf("first chunk Index = %d, want 0", chunks[0].Index)
	}
	if chunks[1].Index != 1 {
		t.Errorf("second chunk Index = %d, want 1", chunks[1].Index)
	}
}

func TestSessionEmit_FillsZeroAt(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-at", "act", nil)
	before := time.Now()
	s.Emit(domain.ResultChunk{Kind: "text"}) // At left zero
	after := time.Now()

	chunk := s.ResultChunks()[0]
	if chunk.At.Before(before) || chunk.At.After(after) {
		t.Errorf("At = %v, want in [%v, %v]", chunk.At, before, after)
	}
}

func TestSessionEmit_PreservesExecutorSuppliedAt(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-at2", "act", nil)
	fixed := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	s.Emit(domain.ResultChunk{Kind: "text", At: fixed})

	chunk := s.ResultChunks()[0]
	if !chunk.At.Equal(fixed) {
		t.Errorf("At = %v, want %v", chunk.At, fixed)
	}
}

func TestSessionEmit_RaisesResultChunkEmittedEvent(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-ev", "act", nil)
	s.Emit(domain.ResultChunk{Kind: "text", Data: "payload", ContentType: "text/plain"})

	events := s.PullEvents()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	emitted, ok := events[0].(domain.ResultChunkEmitted)
	if !ok {
		t.Fatalf("event type = %T, want ResultChunkEmitted", events[0])
	}
	if emitted.SessionID != "s-ev" || emitted.ActionName != "act" {
		t.Errorf("identity wrong: %+v", emitted)
	}
	if emitted.Chunk.Kind != "text" || emitted.Chunk.Data != "payload" {
		t.Errorf("chunk payload wrong: %+v", emitted.Chunk)
	}
	if emitted.EventType() != "result.chunk.emitted" {
		t.Errorf("EventType() = %q, want result.chunk.emitted", emitted.EventType())
	}
}

func TestSessionResultChunks_EmptyIsNotNil(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-empty", "act", nil)
	got := s.ResultChunks()
	if got == nil {
		t.Error("ResultChunks() returned nil on empty session, want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("ResultChunks() = %v, want empty", got)
	}
}

func TestSessionResultChunks_DefensivelyCopied(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-defcopy", "act", nil)
	s.Emit(domain.ResultChunk{Kind: "text", Data: "one"})

	got := s.ResultChunks()
	got[0].Data = "TAMPERED"

	inside := s.ResultChunks()
	if inside[0].Data != "one" {
		t.Errorf("internal chunk mutated through accessor: %v", inside[0].Data)
	}
}

// --- Monotonicity under concurrent Emit ---

func TestSessionEmit_ConcurrentIndicesAreUnique(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-conc", "act", nil)

	const goroutines = 8
	const perGoroutine = 125
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				s.Emit(domain.ResultChunk{Kind: "text"})
			}
		}()
	}
	wg.Wait()

	chunks := s.ResultChunks()
	expected := goroutines * perGoroutine
	if len(chunks) != expected {
		t.Fatalf("len = %d, want %d", len(chunks), expected)
	}
	seen := make(map[int]bool, expected)
	for _, c := range chunks {
		if seen[c.Index] {
			t.Fatalf("duplicate Index %d", c.Index)
		}
		seen[c.Index] = true
	}
	if len(seen) != expected {
		t.Fatalf("unique indices = %d, want %d", len(seen), expected)
	}
}

// --- Snapshot round-trip ---

func TestResultChunks_SurviveSnapshotRoundTrip(t *testing.T) {
	s, _ := domain.NewExecutionSession("s-rt", "act", nil)
	s.Emit(domain.ResultChunk{Kind: "text", Data: "one", ContentType: "text/plain"})
	s.Emit(domain.ResultChunk{Kind: "progress", Data: 0.5})

	snap := s.ToSnapshot()
	if len(snap.ResultChunks) != 2 {
		t.Fatalf("snapshot ResultChunks = %d, want 2", len(snap.ResultChunks))
	}

	restored, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}
	got := restored.ResultChunks()
	if len(got) != 2 {
		t.Fatalf("restored chunks = %d, want 2", len(got))
	}
	if got[0].Data != "one" || got[0].Kind != "text" || got[0].ContentType != "text/plain" {
		t.Errorf("restored chunk[0] fields wrong: %+v", got[0])
	}
	if got[0].Index != 0 || got[1].Index != 1 {
		t.Errorf("indices drifted: %d, %d", got[0].Index, got[1].Index)
	}
}

func TestSessionFromSnapshot_NoChunksFieldLoadsCleanly(t *testing.T) {
	// Pre-1.1 snapshots omit ResultChunks entirely.
	snap := domain.SessionSnapshot{
		Schema:     "1",
		ID:         "legacy",
		ActionName: "act",
		Status:     string(domain.StatusPending),
	}
	s, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}
	chunks := s.ResultChunks()
	if len(chunks) != 0 {
		t.Errorf("legacy snapshot loaded %d chunks, want 0", len(chunks))
	}
}

// --- Streaming executor path ---

type streamingEcho struct {
	chunks []domain.ResultChunk
}

// Execute satisfies ActionExecutor — should NOT be preferred by the
// kernel when ExecuteStream is available. Called out in test as a
// guard rail: the test fails the expectation if Execute fires.
func (e *streamingEcho) Execute(_ context.Context, _ any, _ domain.CapabilityInvoker) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	return domain.ExecutionResult{}, nil, errors.New("synchronous Execute called on streaming executor — kernel should have preferred ExecuteStream")
}

func (e *streamingEcho) ExecuteStream(_ context.Context, _ any, _ domain.CapabilityInvoker, stream domain.ResultStream) (domain.ExecutionResult, []domain.EvidenceRecord, error) {
	for _, c := range e.chunks {
		stream.Emit(c)
	}
	return domain.ExecutionResult{Summary: "streamed"}, nil, nil
}

func TestStreamingActionExecutor_IsPreferredByKernel(t *testing.T) {
	// Reuses the in-package fakes declared in services_test.go so the
	// shared ContractValidator / ActionRepository / lookup contracts
	// behave identically across tests.
	actionRepo := newFakeActionRepo()
	capRepo := newFakeCapRepo()
	actionExecs := &fakeActionExecLookup{executors: map[domain.ActionExecutorRef]domain.ActionExecutor{}}
	capExecs := &fakeCapExecLookup{executors: map[domain.CapabilityExecutorRef]domain.CapabilityExecutor{}}

	exec := &streamingEcho{chunks: []domain.ResultChunk{
		{Kind: "text", Data: "hello"},
		{Kind: "text", Data: "world"},
	}}
	actionExecs.executors["exec.echo"] = exec

	action, _ := domain.NewActionDefinition(
		"echo", "echo",
		domain.EmptyContract(), domain.EmptyContract(), nil,
		domain.EffectProfile{Level: domain.EffectNone},
		domain.IdempotencyProfile{IsIdempotent: true},
	)
	_ = action.BindExecutor("exec.echo")
	_ = actionRepo.Save(action)

	svc := domain.NewActionExecutionService(
		actionRepo,
		domain.NewCapabilityResolutionService(capRepo),
		&fakeValidator{},
		actionExecs,
		capExecs,
	)

	session, _ := domain.NewExecutionSession("s-stream", "echo", nil)
	if err := svc.Execute(context.Background(), session); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	chunks := session.ResultChunks()
	if len(chunks) != 2 {
		t.Fatalf("streamed chunks = %d, want 2 — kernel likely used Execute path by mistake", len(chunks))
	}
	if chunks[0].Data != "hello" || chunks[1].Data != "world" {
		t.Errorf("chunk payloads wrong: %+v", chunks)
	}
}
