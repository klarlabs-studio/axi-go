package domain_test

import (
	"errors"
	"strings"
	"testing"

	"go.klarlabs.de/axi/domain"
)

// --- Helpers ---

func appendEvidence(t *testing.T, s *domain.ExecutionSession, records ...domain.EvidenceRecord) {
	t.Helper()
	for _, r := range records {
		s.AppendEvidence(r)
	}
}

func newValidatedSession(t *testing.T, id domain.ExecutionSessionID) *domain.ExecutionSession {
	t.Helper()
	s, err := domain.NewExecutionSession(id, "act", nil)
	if err != nil {
		t.Fatalf("NewExecutionSession: %v", err)
	}
	if err := s.MarkValidated(); err != nil {
		t.Fatalf("MarkValidated: %v", err)
	}
	_ = s.PullEvents() // drop SessionStarted to focus on evidence
	return s
}

// --- Hash determinism ---

func TestAppendEvidence_PopulatesHashAndChain(t *testing.T) {
	s := newValidatedSession(t, "s-1")

	appendEvidence(t, s,
		domain.EvidenceRecord{Kind: "a", Source: "x", Value: "one", TokensUsed: 1},
		domain.EvidenceRecord{Kind: "b", Source: "y", Value: "two", TokensUsed: 2},
		domain.EvidenceRecord{Kind: "c", Source: "z", Value: "three", TokensUsed: 3},
	)

	evidence := s.Evidence()
	if len(evidence) != 3 {
		t.Fatalf("evidence length = %d, want 3", len(evidence))
	}

	if evidence[0].PreviousHash != "" {
		t.Errorf("head record PreviousHash = %q, want empty", evidence[0].PreviousHash)
	}
	if evidence[0].Hash == "" {
		t.Error("head record Hash is empty, want populated")
	}
	for i := 1; i < len(evidence); i++ {
		if evidence[i].PreviousHash != evidence[i-1].Hash {
			t.Errorf("record %d PreviousHash = %q, want %q", i, evidence[i].PreviousHash, evidence[i-1].Hash)
		}
		if evidence[i].Hash == "" {
			t.Errorf("record %d Hash is empty", i)
		}
		if evidence[i].Hash == evidence[i-1].Hash {
			t.Errorf("record %d Hash collides with previous: %q", i, evidence[i].Hash)
		}
	}
}

func TestAppendEvidence_OverwritesPluginSuppliedHashFields(t *testing.T) {
	s := newValidatedSession(t, "s-2")

	// A hostile or confused plugin presupplying forged chain fields —
	// the aggregate must ignore them.
	s.AppendEvidence(domain.EvidenceRecord{
		Kind:         "forged",
		Source:       "plugin",
		Value:        "data",
		Hash:         domain.EvidenceHash("deadbeef"),
		PreviousHash: domain.EvidenceHash("feedface"),
	})

	evidence := s.Evidence()
	if evidence[0].Hash == "deadbeef" || evidence[0].Hash == "" {
		t.Errorf("expected aggregate to overwrite Hash, got %q", evidence[0].Hash)
	}
	if evidence[0].PreviousHash != "" {
		t.Errorf("head record PreviousHash = %q, want empty", evidence[0].PreviousHash)
	}
}

// --- Verification ---

func TestVerifyEvidenceChain_HappyPath(t *testing.T) {
	s := newValidatedSession(t, "s-3")
	appendEvidence(t, s,
		domain.EvidenceRecord{Kind: "a", Value: "one"},
		domain.EvidenceRecord{Kind: "b", Value: "two"},
		domain.EvidenceRecord{Kind: "c", Value: "three"},
	)
	if err := s.VerifyEvidenceChain(); err != nil {
		t.Errorf("VerifyEvidenceChain on intact chain: %v", err)
	}
}

func TestVerifyEvidenceChain_EmptyTrailIsValid(t *testing.T) {
	s := newValidatedSession(t, "s-4")
	if err := s.VerifyEvidenceChain(); err != nil {
		t.Errorf("VerifyEvidenceChain on empty trail: %v", err)
	}
}

func TestVerifyEvidenceChain_DetectsTamperedValue(t *testing.T) {
	// Populate a chain, then simulate tampering by round-tripping through
	// snapshot — mutating the Value in the snapshot before rehydrating.
	s := newValidatedSession(t, "s-5")
	appendEvidence(t, s,
		domain.EvidenceRecord{Kind: "a", Value: "one"},
		domain.EvidenceRecord{Kind: "b", Value: "two"},
	)
	snap := s.ToSnapshot()
	snap.Evidence[1].Value = "TAMPERED" // break the chain

	restored, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}
	err = restored.VerifyEvidenceChain()
	if err == nil {
		t.Fatal("VerifyEvidenceChain on tampered chain: want error, got nil")
	}
	var chainErr *domain.ErrChainBroken
	if !errors.As(err, &chainErr) {
		t.Fatalf("expected *ErrChainBroken, got %T: %v", err, err)
	}
	if chainErr.Index != 1 {
		t.Errorf("broken-at index = %d, want 1", chainErr.Index)
	}
	if !strings.Contains(chainErr.Reason, "Hash mismatch") {
		t.Errorf("Reason = %q, expected 'Hash mismatch'", chainErr.Reason)
	}
}

func TestVerifyEvidenceChain_DetectsBrokenLinkage(t *testing.T) {
	s := newValidatedSession(t, "s-6")
	appendEvidence(t, s,
		domain.EvidenceRecord{Kind: "a", Value: "one"},
		domain.EvidenceRecord{Kind: "b", Value: "two"},
	)
	snap := s.ToSnapshot()
	// Rewrite PreviousHash on the second record to a forged value but
	// leave its Hash untouched — linkage breaks.
	snap.Evidence[1].PreviousHash = domain.EvidenceHash("deadbeef")

	restored, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}
	err = restored.VerifyEvidenceChain()
	if err == nil {
		t.Fatal("want ErrChainBroken on PreviousHash mismatch, got nil")
	}
	var chainErr *domain.ErrChainBroken
	if !errors.As(err, &chainErr) {
		t.Fatalf("expected *ErrChainBroken, got %T", err)
	}
	if !strings.Contains(chainErr.Reason, "PreviousHash mismatch") {
		t.Errorf("Reason = %q, expected 'PreviousHash mismatch'", chainErr.Reason)
	}
}

func TestVerifyEvidenceChain_LegacyUnverifiableRecordsDoNotBreakChain(t *testing.T) {
	// Simulate loading a pre-1.1 snapshot where Hash and PreviousHash
	// are empty. VerifyEvidenceChain must treat these as unverifiable
	// (not broken) and return nil.
	snap := domain.SessionSnapshot{
		Schema:     "1",
		ID:         "legacy-1",
		ActionName: "act",
		Status:     string(domain.StatusPending),
		Evidence: []domain.EvidenceSnapshot{
			{Kind: "a", Source: "old", Value: "one"},
			{Kind: "b", Source: "old", Value: "two"},
		},
	}
	s, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}
	if err := s.VerifyEvidenceChain(); err != nil {
		t.Errorf("VerifyEvidenceChain on legacy records: %v", err)
	}
}

func TestVerifyEvidenceChain_MixedLegacyAndHashedRecords(t *testing.T) {
	// Legacy prefix (no hashes) followed by 1.1 records (hashed fresh
	// via AppendEvidence) — the hashed suffix should verify on its own.
	snap := domain.SessionSnapshot{
		Schema:     "1",
		ID:         "mixed-1",
		ActionName: "act",
		Status:     string(domain.StatusPending),
		Evidence: []domain.EvidenceSnapshot{
			{Kind: "legacy", Value: "old1"},
			{Kind: "legacy", Value: "old2"},
		},
	}
	s, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}
	if err := s.MarkValidated(); err != nil {
		t.Fatalf("MarkValidated: %v", err)
	}
	// Append a new hashed record after the legacy prefix.
	s.AppendEvidence(domain.EvidenceRecord{Kind: "new", Value: "fresh"})

	if err := s.VerifyEvidenceChain(); err != nil {
		t.Errorf("mixed legacy/hashed chain should verify, got: %v", err)
	}
}

// --- Round-trip ---

func TestEvidenceChain_SurvivesSnapshotRoundTrip(t *testing.T) {
	s := newValidatedSession(t, "s-rt")
	appendEvidence(t, s,
		domain.EvidenceRecord{Kind: "a", Value: "one"},
		domain.EvidenceRecord{Kind: "b", Value: "two"},
		domain.EvidenceRecord{Kind: "c", Value: "three"},
	)
	original := s.Evidence()

	snap := s.ToSnapshot()
	restored, err := domain.SessionFromSnapshot(snap)
	if err != nil {
		t.Fatalf("SessionFromSnapshot: %v", err)
	}

	got := restored.Evidence()
	if len(got) != len(original) {
		t.Fatalf("round-trip length = %d, want %d", len(got), len(original))
	}
	for i := range got {
		if got[i].Hash != original[i].Hash {
			t.Errorf("record %d Hash drifted: got %q, want %q", i, got[i].Hash, original[i].Hash)
		}
		if got[i].PreviousHash != original[i].PreviousHash {
			t.Errorf("record %d PreviousHash drifted: got %q, want %q", i, got[i].PreviousHash, original[i].PreviousHash)
		}
	}
	if err := restored.VerifyEvidenceChain(); err != nil {
		t.Errorf("round-tripped chain fails verification: %v", err)
	}
}

// --- EvidenceRecorded event carries Hash ---

func TestEvidenceRecordedEvent_CarriesHash(t *testing.T) {
	s := newValidatedSession(t, "s-ev")
	s.AppendEvidence(domain.EvidenceRecord{Kind: "a", Value: "payload"})

	events := s.PullEvents()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	er, ok := events[0].(domain.EvidenceRecorded)
	if !ok {
		t.Fatalf("event = %T, want EvidenceRecorded", events[0])
	}
	if er.Hash == "" {
		t.Error("EvidenceRecorded.Hash is empty")
	}
	if er.Hash != s.Evidence()[0].Hash {
		t.Errorf("EvidenceRecorded.Hash = %q, record.Hash = %q", er.Hash, s.Evidence()[0].Hash)
	}
}

// --- ErrChainBroken ---

func TestErrChainBroken_ErrorMessage(t *testing.T) {
	err := &domain.ErrChainBroken{Index: 2, Reason: "Hash mismatch"}
	got := err.Error()
	want := "evidence chain broken at index 2: Hash mismatch"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrChainBroken_IsMatchesRegardlessOfIndex(t *testing.T) {
	// errors.Is should match any ErrChainBroken, not just equal ones.
	err := &domain.ErrChainBroken{Index: 0, Reason: "anything"}
	if !errors.Is(err, &domain.ErrChainBroken{}) {
		t.Error("errors.Is(ErrChainBroken, &ErrChainBroken{}) = false, want true")
	}
}
