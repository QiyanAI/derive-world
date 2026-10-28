// SPDX-License-Identifier: Apache-2.0
package audit

// Action is the closed audit-action registry from G00 contract §A.4.6.
// The database enforces the same set through audit_actions(action) and the
// audit_events foreign key. Adding an action requires a migration and a G00
// change record; it must never be added here alone.
type Action string

const (
	ActionAuthRegister           Action = "auth.register"
	ActionAuthLogin              Action = "auth.login"
	ActionAuthLoginFailed        Action = "auth.login_failed"
	ActionAuthLogout             Action = "auth.logout"
	ActionAuthSessionRevoked     Action = "auth.session_revoked"
	ActionAuthCSRFFailed         Action = "auth.csrf_failed"
	ActionAuthOriginRejected     Action = "auth.origin_rejected"
	ActionPATCreate              Action = "pat.create"
	ActionPATRevoke              Action = "pat.revoke"
	ActionMeExport               Action = "me.export"
	ActionAuthzDenied            Action = "authz.denied"
	ActionAuthzActorOverride     Action = "authz.actor_override_attempt"
	ActionAuthzNotFound          Action = "authz.not_found"
	ActionRateLimitHit           Action = "rate.limit_hit"
	ActionObjectCreate           Action = "object.create"
	ActionObjectAction           Action = "object.action"
	ActionObjectDerive           Action = "object.derive"
	ActionObjectDelete           Action = "object.delete"
	ActionObjectMemberAdd        Action = "object.member_add"
	ActionObjectMemberRemove     Action = "object.member_remove"
	ActionObjectInvitationCreate Action = "object.invitation_create"
	ActionObjectInvitationRevoke Action = "object.invitation_revoke"
	ActionObjectInvitationAccept Action = "object.invitation_accept"
	ActionObjectProposalCreate   Action = "object.proposal_create"
	ActionObjectProposalApply    Action = "object.proposal_apply"
	ActionObjectProposalDeny     Action = "object.proposal_deny"
	ActionAIMessage              Action = "ai.message"
	ActionAIHandoffIssue         Action = "ai.handoff_issue"
	ActionAIDelegationCreate     Action = "ai.delegation_create"
	ActionAIDelegationRevoke     Action = "ai.delegation_revoke"
	ActionAIConfirmationCreate   Action = "ai.confirmation_create"
	ActionAIConfirmationApprove  Action = "ai.confirmation_approve"
	ActionAIConfirmationDeny     Action = "ai.confirmation_deny"
	ActionAIProfileUpdate        Action = "ai.profile_update"
	ActionAIContextUpdate        Action = "ai.context_update"
	ActionAIConversationCreate   Action = "ai.conversation_create"
	ActionAIConversationDelete   Action = "ai.conversation_delete"
	ActionCapabilityInvoke       Action = "capability.invoke"
	ActionCapabilityCompleted    Action = "capability.completed"
	ActionCapabilityFailed       Action = "capability.failed"
	ActionCapabilityDenied       Action = "capability.denied"
	ActionCapabilityTimeout      Action = "capability.timeout"
	ActionCapabilityUnknown      Action = "capability.unknown"
	ActionCapabilityDefSynced    Action = "capability.definition_synced"
	ActionCapabilityReconciled   Action = "capability.reconciled"
	ActionSystemStartup          Action = "system.startup"
	ActionSystemShutdown         Action = "system.shutdown"
	ActionSystemMigration        Action = "system.migration"
	ActionSystemConfigError      Action = "system.config_error"

	// living-world Social and Publication actions. They are registered by migration
	// 00006 and expand the frozen G00 registry without changing old actions.
	ActionSocialFriendRequest  Action = "social.friend_request"
	ActionSocialFriendAccept   Action = "social.friend_accept"
	ActionSocialFriendDecline  Action = "social.friend_decline"
	ActionSocialFriendRemove   Action = "social.friend_remove"
	ActionSocialBlock          Action = "social.block"
	ActionSocialUnblock        Action = "social.unblock"
	ActionSocialMessageSend    Action = "social.message_send"
	ActionSocialDiscussionPost Action = "social.discussion_post"
	// ActionSocialDiscoverabilityUpdate is registered by migration 00007. It
	// records a deliberate account-discovery privacy change.
	ActionSocialDiscoverabilityUpdate Action = "social.discoverability_update"
	ActionPublicationCreate           Action = "publication.create"
	ActionPublicationActivate         Action = "publication.activate"
	ActionPublicationRevoke           Action = "publication.revoke"
	ActionPublicationConsent          Action = "publication.consent"
	ActionPublicationDerive           Action = "publication.derive"
	ActionPublicationReport           Action = "publication.report"

	// World asset actions. They are registered by migration 00009 and expand
	// the frozen registry without changing any existing action.
	ActionAssetUpload Action = "asset.upload"
	ActionAssetRemove Action = "asset.remove"
)

// AllActions is the normative G00 49-action set plus the living-world additions
// registered by migration 00006 and the discovery-privacy action registered by
// migration 00007.
func AllActions() []Action {
	return []Action{
		ActionAuthRegister, ActionAuthLogin, ActionAuthLoginFailed, ActionAuthLogout,
		ActionAuthSessionRevoked, ActionAuthCSRFFailed, ActionAuthOriginRejected,
		ActionPATCreate, ActionPATRevoke, ActionMeExport,
		ActionAuthzDenied, ActionAuthzActorOverride, ActionAuthzNotFound, ActionRateLimitHit,
		ActionObjectCreate, ActionObjectAction, ActionObjectDerive, ActionObjectDelete,
		ActionObjectMemberAdd, ActionObjectMemberRemove, ActionObjectInvitationCreate,
		ActionObjectInvitationRevoke, ActionObjectInvitationAccept,
		ActionObjectProposalCreate, ActionObjectProposalApply, ActionObjectProposalDeny,
		ActionAIMessage, ActionAIHandoffIssue, ActionAIDelegationCreate, ActionAIDelegationRevoke,
		ActionAIConfirmationCreate, ActionAIConfirmationApprove, ActionAIConfirmationDeny,
		ActionAIProfileUpdate, ActionAIContextUpdate, ActionAIConversationCreate,
		ActionAIConversationDelete,
		ActionCapabilityInvoke, ActionCapabilityCompleted, ActionCapabilityFailed,
		ActionCapabilityDenied, ActionCapabilityTimeout, ActionCapabilityUnknown,
		ActionCapabilityDefSynced, ActionCapabilityReconciled,
		ActionSystemStartup, ActionSystemShutdown, ActionSystemMigration, ActionSystemConfigError,
		ActionSocialFriendRequest, ActionSocialFriendAccept, ActionSocialFriendDecline,
		ActionSocialFriendRemove, ActionSocialBlock, ActionSocialUnblock,
		ActionSocialMessageSend, ActionSocialDiscussionPost,
		ActionSocialDiscoverabilityUpdate,
		ActionPublicationCreate, ActionPublicationActivate, ActionPublicationRevoke,
		ActionPublicationConsent, ActionPublicationDerive, ActionPublicationReport,
		ActionAssetUpload, ActionAssetRemove,
	}
}

// IsRegistered reports whether the action is part of the frozen registry.
func IsRegistered(action Action) bool {
	for _, candidate := range AllActions() {
		if candidate == action {
			return true
		}
	}
	return false
}
