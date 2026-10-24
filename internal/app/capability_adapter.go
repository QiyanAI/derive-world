// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/QiyanAI/derive-world/internal/capability"
	"github.com/QiyanAI/derive-world/internal/derive"
)

// deriveAdapter connects the capability Broker to the derivation domain
// service without capability importing derive (A.12).
type deriveAdapter struct {
	service *derive.Service
}

func (a *deriveAdapter) CreateCapability(ctx context.Context, actorID, parentID uuid.UUID,
	request capability.DeriveRequest, requestID string, ipHash []byte) (*capability.DeriveResult, error) {
	result, err := a.service.CreateCapability(ctx, actorID, parentID, derive.Request{
		Title: request.Title, SourceVersion: request.SourceVersion,
		InheritanceFields: request.InheritanceFields,
	}, requestID, ipHash)
	if err != nil {
		return nil, err
	}
	return deriveResult(result)
}

func (a *deriveAdapter) CreateConfirmedTx(ctx context.Context, tx pgx.Tx,
	actorID, parentID uuid.UUID, expectedVersion int, request capability.DeriveRequest,
	aiProfileID, delegationID, confirmationID uuid.UUID, requestID string,
	ipHash []byte) (*capability.DeriveResult, error) {
	result, err := a.service.CreateConfirmedWithResultTx(ctx, tx, actorID, parentID,
		expectedVersion, derive.Request{
			Title: request.Title, SourceVersion: request.SourceVersion,
			InheritanceFields: request.InheritanceFields,
		}, aiProfileID, delegationID, confirmationID, requestID, ipHash)
	if err != nil {
		return nil, err
	}
	return deriveResult(result)
}

func deriveResult(result *derive.Result) (*capability.DeriveResult, error) {
	viewJSON, err := json.Marshal(result.View)
	if err != nil {
		return nil, err
	}
	return &capability.DeriveResult{
		ChildID: result.ChildID, SourceVersion: result.SourceVersion,
		ParentVersion: result.ParentVersion, ViewJSON: viewJSON,
	}, nil
}

// personalAICapabilityWriter adapts the Broker to the interface Personal AI
// declares. It keeps personalai free of a capability import.
type personalAICapabilityWriter struct {
	service *capability.Service
}

func (w *personalAICapabilityWriter) ProposeAITx(ctx context.Context, tx pgx.Tx,
	userID, profileID, objectID uuid.UUID, action string, input json.RawMessage,
	expectedVersion int, requestID string, ipHash []byte) (uuid.UUID, uuid.UUID, bool, error) {
	return w.service.ProposeAITx(ctx, tx, capability.AICall{
		UserID: userID, ProfileID: profileID, ObjectID: objectID,
		CapabilityID: capability.IDObjectProposeChange, Action: action,
		Input: input, ExpectedVersion: expectedVersion,
		RequestID: requestID, IPHash: ipHash,
	})
}

func (w *personalAICapabilityWriter) DeriveConfirmationAITx(ctx context.Context, tx pgx.Tx,
	userID, profileID, objectID uuid.UUID, input json.RawMessage,
	expectedVersion int, requestID string, ipHash []byte) (uuid.UUID, uuid.UUID, bool, error) {
	return w.service.DeriveConfirmationAITx(ctx, tx, capability.AICall{
		UserID: userID, ProfileID: profileID, ObjectID: objectID,
		CapabilityID: capability.IDObjectDerive, Input: input,
		ExpectedVersion: expectedVersion, RequestID: requestID, IPHash: ipHash,
	})
}

func (w *personalAICapabilityWriter) AllowAI(userID uuid.UUID) bool {
	_, _, ok := w.service.Allow(userID)
	return ok
}
