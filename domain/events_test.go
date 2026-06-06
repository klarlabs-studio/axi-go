package domain_test

import (
	"testing"
	"time"

	"go.klarlabs.de/axi/domain"
)

func TestDomainEvent_EventTypes(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		ev   domain.DomainEvent
		want string
	}{
		{"SessionStarted", domain.SessionStarted{At: now}, "session.started"},
		{"SessionAwaitingApproval", domain.SessionAwaitingApproval{At: now}, "session.awaiting_approval"},
		{"SessionCompleted", domain.SessionCompleted{At: now}, "session.completed"},
		{"CapabilityInvoked", domain.CapabilityInvoked{At: now}, "capability.invoked"},
		{"CapabilityRetried", domain.CapabilityRetried{At: now}, "capability.retried"},
		{"BudgetExceeded", domain.BudgetExceeded{At: now}, "budget.exceeded"},
		{"EvidenceRecorded", domain.EvidenceRecorded{At: now}, "evidence.recorded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ev.EventType(); got != tt.want {
				t.Errorf("EventType() = %q, want %q", got, tt.want)
			}
			if !tt.ev.OccurredAt().Equal(now) {
				t.Errorf("OccurredAt() = %v, want %v", tt.ev.OccurredAt(), now)
			}
		})
	}
}

func TestNopDomainEventPublisher_DiscardsEvents(t *testing.T) {
	// The contract is that Nop accepts every event without panicking
	// and without retaining any state. There's nothing to assert beyond
	// "this doesn't crash" — but exercising it locks the no-op contract.
	var p domain.DomainEventPublisher = domain.NopDomainEventPublisher{}
	p.Publish(domain.SessionStarted{SessionID: "s1", ActionName: "a", At: time.Now()})
	p.Publish(domain.BudgetExceeded{Kind: domain.BudgetKindTokens, At: time.Now()})
	p.Publish(nil) // even nil is silently accepted
}

func TestExecutionSession_PullEvents_RaisesAndDrains(t *testing.T) {
	session, err := domain.NewExecutionSession("s-1", "act", nil)
	if err != nil {
		t.Fatalf("NewExecutionSession: %v", err)
	}

	// No events yet.
	if got := session.PullEvents(); got != nil {
		t.Errorf("PullEvents on fresh session = %v, want nil", got)
	}

	// Validated → SessionStarted is buffered.
	if err := session.MarkValidated(); err != nil {
		t.Fatalf("MarkValidated: %v", err)
	}
	events := session.PullEvents()
	if len(events) != 1 {
		t.Fatalf("PullEvents after MarkValidated = %d events, want 1", len(events))
	}
	if _, ok := events[0].(domain.SessionStarted); !ok {
		t.Errorf("event[0] type = %T, want domain.SessionStarted", events[0])
	}
	if events[0].EventType() != "session.started" {
		t.Errorf("event[0].EventType() = %q, want session.started", events[0].EventType())
	}

	// Drained — second pull returns nil.
	if got := session.PullEvents(); got != nil {
		t.Errorf("second PullEvents = %v, want nil after drain", got)
	}
}

func TestExecutionSession_LifecycleEvents_FullPath(t *testing.T) {
	session, _ := domain.NewExecutionSession("s-2", "act", nil)
	// Pending → Validated → Resolved → Running → Succeeded
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkRunning()
	time.Sleep(2 * time.Millisecond) // ensure non-zero duration
	_ = session.Succeed(domain.ExecutionResult{Summary: "ok"})

	events := session.PullEvents()
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (started + completed)", len(events))
	}
	started, ok := events[0].(domain.SessionStarted)
	if !ok {
		t.Fatalf("events[0] = %T, want SessionStarted", events[0])
	}
	if started.SessionID != "s-2" || started.ActionName != "act" {
		t.Errorf("SessionStarted fields wrong: %+v", started)
	}

	completed, ok := events[1].(domain.SessionCompleted)
	if !ok {
		t.Fatalf("events[1] = %T, want SessionCompleted", events[1])
	}
	if completed.Status != domain.StatusSucceeded {
		t.Errorf("Status = %s, want succeeded", completed.Status)
	}
	if completed.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", completed.Duration)
	}
}

func TestExecutionSession_AppendEvidence_RaisesEvidenceRecorded(t *testing.T) {
	session, _ := domain.NewExecutionSession("s-3", "act", nil)
	_ = session.MarkValidated()
	_ = session.PullEvents() // drop SessionStarted

	session.AppendEvidence(domain.EvidenceRecord{
		Kind:       "summary",
		Source:     "tester",
		Value:      "hi",
		TokensUsed: 42,
	})

	events := session.PullEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	er, ok := events[0].(domain.EvidenceRecorded)
	if !ok {
		t.Fatalf("events[0] = %T, want EvidenceRecorded", events[0])
	}
	if er.EvidenceKind != "summary" || er.Tokens != 42 {
		t.Errorf("EvidenceRecorded fields wrong: %+v", er)
	}
}

func TestExecutionSession_RejectRaisesSessionCompleted(t *testing.T) {
	session, _ := domain.NewExecutionSession("s-4", "act", nil)
	_ = session.MarkValidated()
	_ = session.MarkResolved(nil)
	_ = session.MarkAwaitingApproval()
	_ = session.PullEvents() // drop earlier events

	err := session.Reject(
		domain.FailureReason{Code: "DENIED", Message: "no"},
		domain.ApprovalDecision{Principal: "human"},
	)
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}

	events := session.PullEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	completed, ok := events[0].(domain.SessionCompleted)
	if !ok {
		t.Fatalf("events[0] = %T, want SessionCompleted", events[0])
	}
	if completed.Status != domain.StatusRejected {
		t.Errorf("Status = %s, want rejected", completed.Status)
	}
}
