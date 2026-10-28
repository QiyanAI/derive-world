// SPDX-License-Identifier: Apache-2.0
package derive

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/objects"
)

// CreateConfirmedTx executes an AI-originated derive inside the confirmation
// approval transaction. It uses the same prepareDeriveTx path as the human
// endpoint: the approver must still hold read+derive, the parent version must
// equal the confirmed payload version, and the child is written only through
// objects.CreateDerived.
func (s *Service) CreateConfirmedTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID, parentID uuid.UUID,
	expectedVersion int,
	request Request,
	aiProfileID, delegationID, confirmationID uuid.UUID,
	requestID string,
	ipHash []byte,
) (*objects.ObjectView, error) {
	result, err := s.CreateConfirmedWithResultTx(ctx, tx, actorID, parentID, expectedVersion,
		request, aiProfileID, delegationID, confirmationID, requestID, ipHash)
	if err != nil {
		return nil, err
	}
	return result.View, nil
}

// CreateConfirmedWithResultTx is the capability-facing confirmation executor.
// It returns the verified snapshot versions alongside the object view.
func (s *Service) CreateConfirmedWithResultTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID, parentID uuid.UUID,
	expectedVersion int,
	request Request,
	aiProfileID, delegationID, confirmationID uuid.UUID,
	requestID string,
	ipHash []byte,
) (*Result, error) {
	prepared, err := s.prepareDeriveTx(ctx, tx, actorID, parentID, request, &expectedVersion)
	if err != nil {
		return nil, err
	}
	childID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	confirmedBy := actorID
	aiActor := audit.Actor{
		Type: "personal_ai", UserID: &actorID,
		AIProfileID: &aiProfileID, DelegationID: &delegationID,
	}
	view, err := s.objects.CreateDerived(ctx, tx, objects.DerivedInput{
		ChildID: childID, ParentID: parentID, ParentType: prepared.Parent.ObjectType,
		Template: prepared.Parent.PresentationTemplate, ActorID: actorID, Actor: aiActor,
		State: prepared.ChildState, SourceVersion: prepared.SourceVersion,
		InheritedFields: request.InheritanceFields, Lineage: prepared.Lineage,
		RequestID: requestID, IPHash: ipHash, SkipIdempotency: true,
		CopyObjectAssets: true,
		ConfirmationID:   &confirmationID, ConfirmedBy: &confirmedBy,
	})
	if err != nil {
		return nil, err
	}
	return &Result{
		View: view, ChildID: childID, SourceVersion: prepared.SourceVersion,
		ParentVersion: prepared.Parent.Version,
	}, nil
}
