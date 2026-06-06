package application

import (
	"context"
	"fmt"

	"go.klarlabs.de/axi/domain"
)

// IDGenerator generates unique execution session IDs.
type IDGenerator interface {
	GenerateSessionID() domain.ExecutionSessionID
}

// ExecuteActionUseCase orchestrates action execution.
type ExecuteActionUseCase struct {
	SessionRepo      domain.SessionRepository
	ExecutionService *domain.ActionExecutionService
	IDGen            IDGenerator
}

// ExecuteActionInput is the input for the use case.
type ExecuteActionInput struct {
	ActionName domain.ActionName
	Input      any
}

// ExecuteActionOutput is the output of the use case.
type ExecuteActionOutput struct {
	SessionID        domain.ExecutionSessionID
	Status           domain.ExecutionStatus
	RequiresApproval bool
	Result           *domain.ExecutionResult
	Failure          *domain.FailureReason
	Evidence         []domain.EvidenceRecord
	ApprovalDecision *domain.ApprovalDecision
	Suggestions      []domain.Suggestion
}

// Execute runs an action. If the action requires approval (external effects),
// the session pauses at AwaitingApproval and the output indicates this.
func (uc *ExecuteActionUseCase) Execute(ctx context.Context, input ExecuteActionInput) (*ExecuteActionOutput, error) {
	sessionID := uc.IDGen.GenerateSessionID()

	session, err := domain.NewExecutionSession(sessionID, input.ActionName, input.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to create execution session: %w", err)
	}

	if err := uc.ExecutionService.Execute(ctx, session); err != nil {
		if saveErr := uc.SessionRepo.Save(session); saveErr != nil {
			return nil, fmt.Errorf("execution failed: %w (also failed to persist session: %v)", err, saveErr)
		}
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	if err := uc.SessionRepo.Save(session); err != nil {
		return nil, fmt.Errorf("failed to persist session: %w", err)
	}

	return outputFromSession(session), nil
}

// ExecuteAsync creates a session and runs execution in the background.
// Returns immediately with the session in Pending status.
// Poll GET /sessions/{id} for the result.
func (uc *ExecuteActionUseCase) ExecuteAsync(ctx context.Context, input ExecuteActionInput) (*ExecuteActionOutput, error) {
	sessionID := uc.IDGen.GenerateSessionID()

	session, err := domain.NewExecutionSession(sessionID, input.ActionName, input.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to create execution session: %w", err)
	}

	// Capture the initial snapshot before any mutation.
	output := outputFromSession(session)

	// Persist the pending session immediately so it's pollable.
	if err := uc.SessionRepo.Save(session); err != nil {
		return nil, fmt.Errorf("failed to persist session: %w", err)
	}

	// Execute in background. The goroutine owns the session exclusively.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = session.Fail(domain.FailureReason{Code: "PANIC", Message: fmt.Sprintf("%v", r)})
			}
			_ = uc.SessionRepo.Save(session)
		}()
		bgCtx := context.WithoutCancel(ctx)
		_ = uc.ExecutionService.Execute(bgCtx, session)
	}()

	return output, nil
}

// ApproveSession approves a session awaiting approval and resumes execution.
func (uc *ExecuteActionUseCase) ApproveSession(ctx context.Context, id domain.ExecutionSessionID, decision domain.ApprovalDecision) (*ExecuteActionOutput, error) {
	session, err := uc.SessionRepo.Get(id)
	if err != nil {
		return nil, err
	}

	if err := session.Approve(decision); err != nil {
		return nil, &domain.ErrValidation{Message: err.Error()}
	}

	if err := uc.ExecutionService.Resume(ctx, session); err != nil {
		_ = uc.SessionRepo.Save(session)
		return nil, fmt.Errorf("execution failed after approval: %w", err)
	}

	if err := uc.SessionRepo.Save(session); err != nil {
		return nil, fmt.Errorf("failed to persist session: %w", err)
	}

	return outputFromSession(session), nil
}

// RejectSession rejects a session awaiting approval. The decision's
// Rationale becomes the FailureReason.Message.
func (uc *ExecuteActionUseCase) RejectSession(_ context.Context, id domain.ExecutionSessionID, decision domain.ApprovalDecision) (*ExecuteActionOutput, error) {
	session, err := uc.SessionRepo.Get(id)
	if err != nil {
		return nil, err
	}

	if err := session.Reject(domain.FailureReason{Code: "REJECTED", Message: decision.Rationale}, decision); err != nil {
		return nil, &domain.ErrValidation{Message: err.Error()}
	}

	if err := uc.SessionRepo.Save(session); err != nil {
		return nil, fmt.Errorf("failed to persist session: %w", err)
	}

	return outputFromSession(session), nil
}

func outputFromSession(session *domain.ExecutionSession) *ExecuteActionOutput {
	out := &ExecuteActionOutput{
		SessionID:        session.ID(),
		Status:           session.Status(),
		RequiresApproval: session.RequiresApproval(),
		Result:           session.Result(),
		Failure:          session.Failure(),
		Evidence:         session.Evidence(),
		ApprovalDecision: session.ApprovalDecision(),
	}
	if out.Result != nil {
		out.Suggestions = out.Result.Suggestions
	}
	return out
}
