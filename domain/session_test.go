package domain_test

import (
	"testing"

	"go.klarlabs.de/axi/domain"
)

func TestExecutionSession_HappyPath(t *testing.T) {
	session, err := domain.NewExecutionSession("s1", "greet", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if session.Status() != domain.StatusPending {
		t.Fatalf("expected Pending, got %s", session.Status())
	}

	if err := session.MarkValidated(); err != nil {
		t.Fatalf("MarkValidated: %v", err)
	}
	if session.Status() != domain.StatusValidated {
		t.Fatalf("expected Validated, got %s", session.Status())
	}

	if err := session.MarkResolved([]domain.CapabilityName{"cap.a"}); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}
	if session.Status() != domain.StatusResolved {
		t.Fatalf("expected Resolved, got %s", session.Status())
	}
	if len(session.ResolvedCapabilities()) != 1 {
		t.Fatalf("expected 1 resolved capability")
	}

	if err := session.MarkRunning(); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if session.Status() != domain.StatusRunning {
		t.Fatalf("expected Running, got %s", session.Status())
	}

	session.AppendEvidence(domain.EvidenceRecord{Kind: "log", Source: "test", Value: "ran"})
	if len(session.Evidence()) != 1 {
		t.Fatalf("expected 1 evidence record")
	}

	if err := session.Succeed(domain.ExecutionResult{Data: "hello", Summary: "greeted"}); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if session.Status() != domain.StatusSucceeded {
		t.Fatalf("expected Succeeded, got %s", session.Status())
	}
	if session.Result() == nil {
		t.Fatal("expected result")
	}
	if session.Failure() != nil {
		t.Fatal("expected no failure on success")
	}
}

func TestExecutionSession_FailPath(t *testing.T) {
	session, _ := domain.NewExecutionSession("s2", "fail-action", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkRunning()

	if err := session.Fail(domain.FailureReason{Code: "ERR", Message: "boom"}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if session.Status() != domain.StatusFailed {
		t.Fatalf("expected Failed, got %s", session.Status())
	}
	if session.Failure() == nil {
		t.Fatal("expected failure reason")
	}
	if session.Result() != nil {
		t.Fatal("expected no result on failure")
	}
}

func TestExecutionSession_InvalidTransitions(t *testing.T) {
	tests := []struct {
		name string
		fn   func(s *domain.ExecutionSession) error
	}{
		{"Pending → Resolved", func(s *domain.ExecutionSession) error { return s.MarkResolved(nil) }},
		{"Pending → Running", func(s *domain.ExecutionSession) error { return s.MarkRunning() }},
		{"Pending → Succeed", func(s *domain.ExecutionSession) error {
			return s.Succeed(domain.ExecutionResult{})
		}},
		{"Pending → Fail", func(s *domain.ExecutionSession) error {
			return s.Fail(domain.FailureReason{})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, _ := domain.NewExecutionSession("s", "action", nil)
			if err := tt.fn(session); err == nil {
				t.Error("expected error for invalid transition")
			}
		})
	}
}

func TestExecutionSession_NoBackwardTransition(t *testing.T) {
	session, _ := domain.NewExecutionSession("s", "action", nil)
	_ = session.MarkValidated()

	// Cannot go back to Validated from Validated (it's not Pending → Validated).
	if err := session.MarkValidated(); err == nil {
		t.Error("expected error: cannot re-validate")
	}
}

func TestExecutionSession_CreationValidation(t *testing.T) {
	_, err := domain.NewExecutionSession("", "action", nil)
	if err == nil {
		t.Error("expected error for empty session ID")
	}

	_, err = domain.NewExecutionSession("s1", "", nil)
	if err == nil {
		t.Error("expected error for empty action name")
	}
}

func TestExecutionSession_EvidenceAppendOnly(t *testing.T) {
	session, _ := domain.NewExecutionSession("s", "action", nil)
	session.AppendEvidence(domain.EvidenceRecord{Kind: "a"})
	session.AppendEvidence(domain.EvidenceRecord{Kind: "b"})

	if len(session.Evidence()) != 2 {
		t.Fatalf("expected 2 evidence records, got %d", len(session.Evidence()))
	}
	if session.Evidence()[0].Kind != "a" || session.Evidence()[1].Kind != "b" {
		t.Error("evidence order not preserved")
	}
}
