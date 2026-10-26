// SPDX-License-Identifier: Apache-2.0

export interface MetaResponse {
  version: string;
  commit: string;
  build_time: string;
  model_mode: "fixture" | "real";
}

export interface User {
  id: string;
  email: string;
  handle: string;
  display_name: string;
  created_at: string;
}

export interface PersonalAI {
  id: string;
  name: string;
  persona: string;
}

export interface AuthResponse {
  user: User;
  personal_ai: PersonalAI;
  auth_method: "session" | "pat";
  csrf_token?: string;
  scopes?: string[];
}

export type ObjectType = "collaborative" | "interactive" | "living_scene";
export type PresentationTemplate = "collaborative_v1" | "interactive_v1" | "living_scene_v1";

export interface ObjectView {
  id: string;
  object_type: ObjectType;
  presentation_template: PresentationTemplate;
  schema_version: 1;
  creator_id: string;
  owner_id: string;
  version: number;
  state: Record<string, unknown>;
  shared_context: Record<string, unknown>;
  status: "active" | "deleted";
  permissions: Permission[];
  role: "owner" | "editor" | "viewer" | "custom";
  created_at: string;
  updated_at: string;
  lineage?: {
    parent: { object_id: string; version: number };
    inherited_fields: string[];
    publication_id?: string;
    publication_title?: string;
    public?: boolean;
  };
}

export type Permission = "read" | "contribute" | "derive" | "invite" | "manage";

export interface ObjectListItem {
  id: string;
  object_type: ObjectType;
  title: string;
  version: number;
  owner_id: string;
  role: "owner" | "editor" | "viewer" | "custom";
  permissions: Permission[];
  created_at: string;
  updated_at: string;
}

export interface EventView {
  id: string;
  seq: number;
  object_version: number;
  event_type: string;
  actor: { type: string; user_id?: string; display_name?: string };
  payload: Record<string, unknown>;
  created_at: string;
}

export interface Member {
  user_id: string;
  display_name: string;
  permissions: Permission[];
  role: "owner" | "editor" | "viewer" | "custom";
  granted_by: string | null;
  created_at: string;
}

export type InvitationPermission = "read" | "contribute" | "derive" | "invite";

export interface Invitation {
  id: string;
  object_id: string;
  token: string | null;
  token_redacted?: boolean;
  permissions: InvitationPermission[];
  max_uses: number;
  use_count: number;
  expires_at: string;
  revoked_at: string | null;
  created_at: string;
  status: string;
}

export interface InvitationPreview {
  invitation_id: string;
  object_id: string;
  object_type: ObjectType;
  title: string;
  inviter_name: string;
  permissions: InvitationPermission[];
  expires_at: string;
  max_uses: number;
  remaining_uses: number;
  already_member: boolean;
}

export interface AcceptedInvitation {
  object: {
    id: string;
    object_type: ObjectType;
    version: number;
    title: string;
    presentation_template: string;
  };
  permissions: Permission[];
  role_effective: "owner" | "editor" | "viewer" | "custom";
  already_member: boolean;
}

export interface Handoff {
  handoff_id: string;
  issued_at: string;
  expires_at: string;
  object_id: string;
  object_version: number;
  object_type: ObjectType;
  permitted_object_context: {
    state: Record<string, unknown>;
    shared_context: Record<string, unknown>;
    recent_events: Array<{
      seq: number;
      event_type: string;
      actor_type: string;
      created_at: string;
      summary: string;
    }>;
  };
  permitted_actions: Array<"read" | "contribute" | "derive">;
  data_provenance: Array<{
    field: string;
    source: string;
    object_version: number;
    truncated?: boolean;
  }>;
  context_schema_version: 1;
  truncated: boolean;
}

export type ProposalStatus = "pending" | "applied" | "denied" | "expired";

export interface Proposal {
  id: string;
  object_id: string;
  actor_user_id: string;
  ai_profile_id: string;
  delegation_id: string;
  capability_id: "object.propose_change";
  action: string;
  input: Record<string, unknown>;
  base_version: number;
  status: ProposalStatus;
  created_at: string;
  decided_at: string | null;
  applied_event_id: string | null;
}

export interface ConfirmationEffect {
  title?: string;
  inheritance_fields?: string[];
  source_version?: number;
}

export interface Confirmation {
  id: string;
  action: "derive.create" | "invitation.create" | "capability.invoke";
  resource_type: "living_object" | "capability_invocation";
  resource_id: string;
  status: "pending" | "approved" | "denied" | "expired";
  expires_at: string;
  created_at: string;
  decided_at: string | null;
  payload_hash_prefix: string;
  target_object_id?: string;
  target_object_title: string;
  target_object_version: number;
  target_object_type: string;
  bound_version?: number;
  effect?: ConfirmationEffect;
  permission_summary: string;
}

export interface PersonalAIUsage {
  calls: number;
  input_chars: number;
  daily_calls_limit: number;
  daily_input_chars_limit: number;
  model_mode: "fixture" | "real";
  model_name: string;
}

export interface PersonalAIProfile {
  id: string;
  name: string;
  persona: string;
  created_at: string;
  updated_at: string;
  usage?: PersonalAIUsage;
}

export interface PrivateContext {
  content: string;
  updated_at: string;
}

export interface Conversation {
  id: string;
  user_id: string;
  object_id: string | null;
  title: string;
  created_at: string;
  updated_at: string;
  deleted_at: string | null;
}

export interface AIMessage {
  id: string;
  conversation_id: string;
  role: "user" | "assistant" | "system";
  content: string;
  model_mode: "fixture" | "real";
  parse_status: "structured" | "fallback" | null;
  created_at: string;
}

export interface ConversationDetail {
  conversation: Conversation;
  messages: AIMessage[];
}

export interface AcceptedAIAction {
  kind: "proposal" | "confirmation";
  proposal_id?: string;
  confirmation_id?: string;
  action: string;
}

export interface SendMessageResult {
  user_message: AIMessage;
  assistant_message: AIMessage;
  model_mode: "fixture" | "real";
  parse_status: "structured" | "fallback";
  proposed_actions: AcceptedAIAction[];
  context_stale?: boolean;
  usage: { input_tokens: number; output_tokens: number };
}

export interface Delegation {
  id: string;
  user_id: string;
  ai_profile_id: string;
  object_id: string | null;
  permissions: Array<"read" | "contribute" | "derive">;
  constraints: Record<string, unknown>;
  expires_at: string | null;
  revoked_at: string | null;
  created_at: string;
  status: "active" | "expired" | "revoked";
}

export interface TokenMetadata {
  id: string;
  name: string;
  token_prefix: string;
  scopes: string[];
  expires_at: string | null;
  last_used_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

export interface CreatedToken extends TokenMetadata {
  plaintext: string;
}

export interface LineageAncestor {
  object_id?: string;
  title?: string;
  version?: number;
  depth: number;
  redacted?: boolean;
}

export interface LineageDescendant {
  object_id: string;
  title: string;
  version: number;
  depth: number;
  source_version: number;
  children: LineageDescendant[];
}

export interface Lineage {
  object: { object_id: string; version: number };
  ancestors: LineageAncestor[];
  descendants: LineageDescendant[];
  max_depth: number;
  truncated: boolean;
  source?: ObjectView["lineage"];
}

export interface CreateObjectInput {
  object_type: ObjectType;
  title: string;
  content: Record<string, unknown>;
  shared_context: Record<string, unknown>;
}

export interface DeriveInput {
  title: string;
  source_version?: number;
  inheritance_fields: string[];
}

export class APIError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: Record<string, unknown>;
  readonly requestId: string;

  constructor(
    status: number,
    code: string,
    message: string,
    details: Record<string, unknown> = {},
    requestId = ""
  ) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.code = code;
    this.details = details;
    this.requestId = requestId;
  }
}

export function readCookie(name: string): string {
  const prefix = `${name}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (value.startsWith(prefix)) {
      return decodeURIComponent(value.slice(prefix.length));
    }
  }
  return "";
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  idempotencyKey?: string;
  signal?: AbortSignal;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (method !== "GET" && method !== "HEAD") {
    const csrf = readCookie("dw_csrf");
    if (csrf) {
      headers["X-CSRF-Token"] = csrf;
    }
  }
  if (options.idempotencyKey) {
    headers["Idempotency-Key"] = options.idempotencyKey;
  }
  const response = await fetch(path, {
    method,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
    cache: "no-store",
    credentials: "same-origin"
  });
  if (response.status === 204) {
    return undefined as T;
  }
  const text = await response.text();
  let payload: unknown = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = null;
    }
  }
  if (response.status === 401 && typeof window !== "undefined") {
    window.dispatchEvent(new Event("dw:unauthorized"));
  }
  if (!response.ok) {
    const envelope = payload as
      | {
          error?: {
            code?: string;
            message?: string;
            request_id?: string;
            details?: Record<string, unknown>;
          };
        }
      | null;
    throw new APIError(
      response.status,
      envelope?.error?.code ?? "internal",
      envelope?.error?.message ?? `Request failed with status ${response.status}`,
      envelope?.error?.details ?? {},
      envelope?.error?.request_id ?? ""
    );
  }
  return payload as T;
}

export async function fetchMeta(signal?: AbortSignal): Promise<MetaResponse> {
  return request<MetaResponse>("/api/v1/meta", { signal });
}

export async function register(input: {
  email: string;
  handle: string;
  display_name: string;
  password: string;
}): Promise<AuthResponse> {
  return request<AuthResponse>("/api/v1/auth/register", { method: "POST", body: input });
}

export async function login(email: string, password: string): Promise<AuthResponse> {
  return request<AuthResponse>("/api/v1/auth/login", { method: "POST", body: { email, password } });
}

export async function logout(): Promise<void> {
  await request<void>("/api/v1/auth/logout", { method: "POST" });
}

export async function fetchMe(signal?: AbortSignal): Promise<AuthResponse> {
  return request<AuthResponse>("/api/v1/auth/me", { signal });
}

export async function listObjects(
  scope: "owned" | "shared" | "all",
  cursor?: string
): Promise<{ items: ObjectListItem[]; next_cursor: string | null }> {
  const params = new URLSearchParams({ scope });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return request(`/api/v1/objects?${params.toString()}`);
}

export async function createObject(input: CreateObjectInput): Promise<ObjectView> {
  return idempotentRequest<ObjectView>("POST", "/api/v1/objects", input);
}

export async function getObject(id: string, signal?: AbortSignal): Promise<ObjectView> {
  return request<ObjectView>(`/api/v1/objects/${encodeURIComponent(id)}`, { signal });
}

export async function deleteObject(id: string): Promise<void> {
  await request<void>(`/api/v1/objects/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listEvents(
  id: string,
  cursor?: string,
  signal?: AbortSignal
): Promise<{ items: EventView[]; next_cursor: string | null }> {
  const params = new URLSearchParams({ limit: "20" });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return request(`/api/v1/objects/${encodeURIComponent(id)}/events?${params.toString()}`, { signal });
}

export async function applyAction(
  id: string,
  action: string,
  input: Record<string, unknown>,
  expectedVersion: number
): Promise<ObjectView> {
  return idempotentRequest<ObjectView>(
    "POST",
    `/api/v1/objects/${encodeURIComponent(id)}/actions`,
    { action, input, expected_version: expectedVersion }
  );
}

export async function deriveObject(id: string, input: DeriveInput): Promise<ObjectView> {
  return idempotentRequest<ObjectView>(
    "POST",
    `/api/v1/objects/${encodeURIComponent(id)}/derive`,
    input
  );
}

export async function getLineage(id: string, signal?: AbortSignal): Promise<Lineage> {
  return request<Lineage>(`/api/v1/objects/${encodeURIComponent(id)}/lineage`, { signal });
}

export async function listMembers(id: string, signal?: AbortSignal): Promise<{ items: Member[] }> {
  return request(`/api/v1/objects/${encodeURIComponent(id)}/members`, { signal });
}

export async function removeMember(objectID: string, userID: string): Promise<void> {
  await request<void>(
    `/api/v1/objects/${encodeURIComponent(objectID)}/members/${encodeURIComponent(userID)}`,
    { method: "DELETE" }
  );
}

export async function createInvitation(
  objectID: string,
  input: { permissions?: InvitationPermission[]; max_uses?: number; expires_in_days?: number }
): Promise<Invitation> {
  return idempotentRequest<Invitation>(
    "POST",
    `/api/v1/objects/${encodeURIComponent(objectID)}/invitations`,
    input
  );
}

export async function listInvitations(id: string, signal?: AbortSignal): Promise<{ items: Invitation[] }> {
  return request(`/api/v1/objects/${encodeURIComponent(id)}/invitations`, { signal });
}

export async function revokeInvitation(objectID: string, invitationID: string): Promise<void> {
  await request<void>(
    `/api/v1/objects/${encodeURIComponent(objectID)}/invitations/${encodeURIComponent(invitationID)}`,
    { method: "DELETE" }
  );
}

export async function previewInvitation(token: string, signal?: AbortSignal): Promise<InvitationPreview> {
  return request<InvitationPreview>(`/api/v1/invitations/${encodeURIComponent(token)}`, { signal });
}

export async function acceptInvitation(token: string): Promise<AcceptedInvitation> {
  return request<AcceptedInvitation>(`/api/v1/invitations/${encodeURIComponent(token)}/accept`, {
    method: "POST"
  });
}

export async function getHandoff(id: string, signal?: AbortSignal): Promise<Handoff> {
  return request<Handoff>(`/api/v1/objects/${encodeURIComponent(id)}/handoff`, { signal });
}

export async function listProposals(id: string, signal?: AbortSignal): Promise<{ items: Proposal[] }> {
  return request(`/api/v1/objects/${encodeURIComponent(id)}/proposals`, { signal });
}

export async function applyProposal(
  objectID: string,
  proposalID: string,
  expectedVersion: number
): Promise<{ proposal: Proposal; object: ObjectView }> {
  return request(`/api/v1/objects/${encodeURIComponent(objectID)}/proposals/${encodeURIComponent(proposalID)}/apply`, {
    method: "POST",
    body: { expected_version: expectedVersion }
  });
}

export async function denyProposal(objectID: string, proposalID: string): Promise<Proposal> {
  return request<Proposal>(
    `/api/v1/objects/${encodeURIComponent(objectID)}/proposals/${encodeURIComponent(proposalID)}/deny`,
    { method: "POST" }
  );
}

export async function getAIProfile(signal?: AbortSignal): Promise<PersonalAIProfile> {
  return request<PersonalAIProfile>("/api/v1/ai/profile", { signal });
}

export async function updateAIProfile(input: {
  name?: string;
  persona?: string;
}): Promise<PersonalAIProfile> {
  return request<PersonalAIProfile>("/api/v1/ai/profile", { method: "PATCH", body: input });
}

export async function getPrivateContext(signal?: AbortSignal): Promise<PrivateContext> {
  return request<PrivateContext>("/api/v1/ai/context", { signal });
}

export async function updatePrivateContext(content: string): Promise<PrivateContext> {
  return request<PrivateContext>("/api/v1/ai/context", { method: "PUT", body: { content } });
}

export async function listConversations(
  cursor?: string,
  signal?: AbortSignal
): Promise<{ items: Conversation[]; next_cursor: string | null }> {
  const params = new URLSearchParams({ limit: "100" });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return request(`/api/v1/ai/conversations?${params.toString()}`, { signal });
}

export async function createConversation(input: {
  title: string;
  object_id?: string;
}): Promise<Conversation> {
  return idempotentRequest<Conversation>("POST", "/api/v1/ai/conversations", input);
}

export async function getConversation(id: string, signal?: AbortSignal): Promise<ConversationDetail> {
  return request<ConversationDetail>(`/api/v1/ai/conversations/${encodeURIComponent(id)}`, { signal });
}

export async function deleteConversation(id: string): Promise<void> {
  await request<void>(`/api/v1/ai/conversations/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function sendAIMessage(
  conversationID: string,
  content: string
): Promise<SendMessageResult> {
  return idempotentRequest<SendMessageResult>(
    "POST",
    `/api/v1/ai/conversations/${encodeURIComponent(conversationID)}/messages`,
    { content }
  );
}

export async function listDelegations(signal?: AbortSignal): Promise<{ items: Delegation[] }> {
  return request("/api/v1/ai/delegations", { signal });
}

export async function createDelegation(input: {
  object_id?: string;
  permissions: Array<"read" | "contribute" | "derive">;
  constraints?: Record<string, unknown>;
  expires_in_days?: number;
}): Promise<Delegation> {
  return request<Delegation>("/api/v1/ai/delegations", { method: "POST", body: input });
}

export async function revokeDelegation(id: string): Promise<void> {
  await request<void>(`/api/v1/ai/delegations/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listTokens(signal?: AbortSignal): Promise<{ items: TokenMetadata[] }> {
  return request("/api/v1/tokens", { signal });
}

export async function createToken(input: {
  name: string;
  scopes: string[];
  expires_in_days?: number;
}): Promise<CreatedToken> {
  return request<CreatedToken>("/api/v1/tokens", { method: "POST", body: input });
}

export async function revokeToken(id: string): Promise<void> {
  await request<void>(`/api/v1/tokens/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listConfirmations(
  status: "pending" | "all" = "pending",
  signal?: AbortSignal
): Promise<{ items: Confirmation[] }> {
  return request(`/api/v1/confirmations?status=${status}`, { signal });
}

export async function getConfirmation(id: string, signal?: AbortSignal): Promise<Confirmation> {
  return request<Confirmation>(`/api/v1/confirmations/${encodeURIComponent(id)}`, { signal });
}

export async function approveConfirmation(id: string): Promise<ObjectView> {
  return idempotentRequest<ObjectView>(
    "POST",
    `/api/v1/confirmations/${encodeURIComponent(id)}/approve`,
    {}
  );
}

export async function denyConfirmation(id: string): Promise<Confirmation> {
  return request<Confirmation>(`/api/v1/confirmations/${encodeURIComponent(id)}/deny`, {
    method: "POST"
  });
}

export async function exportMe(): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>("/api/v1/me/export");
}

// A retry of the same submission intent must reuse its first key. Keys are
// cleared only after a definitive response so a lost response can be retried
// without creating a second object or derivation edge.
const pendingIdempotency = new Map<string, string>();

function submissionIntent(method: string, path: string, body: unknown): string {
  return `${method}
${path}
${JSON.stringify(body)}`;
}

async function idempotentRequest<T>(method: string, path: string, body: unknown): Promise<T> {
  const intent = submissionIntent(method, path, body);
  let key = pendingIdempotency.get(intent);
  if (!key) {
    key = newIdempotencyKey();
    pendingIdempotency.set(intent, key);
  }
  try {
    const result = await request<T>(path, { method, body, idempotencyKey: key });
    pendingIdempotency.delete(intent);
    return result;
  } catch (failure) {
    if (
      failure instanceof APIError &&
      failure.status < 500 &&
      failure.code !== "request_in_progress"
    ) {
      pendingIdempotency.delete(intent);
    }
    throw failure;
  }
}

export function newIdempotencyKey(): string {
  const random = crypto.randomUUID().replaceAll("-", "");
  const second = crypto.randomUUID().replaceAll("-", "");
  return (random + second).slice(0, 48);
}

// Client message ids are UUIDs; the server treats (sender, id) as the
// idempotency key for a direct message or discussion post.
export function newClientMessageID(): string {
  return crypto.randomUUID();
}

export function errorMessage(error: unknown): string {
  if (error instanceof APIError) {
    return `${error.code}: ${error.message}`;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Unknown error";
}

// ---------------------------------------------------------------------------
// Living world: scene, world stage, pulse, lens, social, publication
// ---------------------------------------------------------------------------

export interface SceneElement {
  id: string;
  kind: "idea" | "step" | "scene" | "choice" | "outcome";
  text: string;
  status: "open" | "done";
}

export interface SceneState {
  title: string;
  purpose: "project" | "story";
  premise: string;
  elements: SceneElement[];
  tags?: string[];
}

export interface StageEvent {
  event_type: string;
  action?: string;
  actor_type: string;
  actor_name?: string;
  occurred_at: string;
}

export interface StageObject {
  id: string;
  object_type: ObjectType;
  presentation_template: PresentationTemplate;
  title: string;
  version: number;
  owner_id: string;
  permissions: Permission[];
  role: "owner" | "editor" | "viewer" | "custom";
  updated_at: string;
  member_count: number;
  branch_count: number;
  latest_event: StageEvent | null;
}

export interface StageEdge {
  parent_id: string;
  child_id: string;
}

export interface WorldStage {
  objects: StageObject[];
  edges: StageEdge[];
  generated_at: string;
}

export interface PulseItem {
  kind: string;
  object_id: string;
  title: string;
  event_type?: string;
  action?: string;
  actor_type?: string;
  actor_name?: string;
  summary?: string;
  occurred_at: string;
}

export interface Friend {
  user_id: string;
  handle: string;
  display_name: string;
  since: string;
  unread: number;
}

export interface FriendRequest {
  id: string;
  direction: "incoming" | "outgoing";
  user_id: string;
  handle: string;
  display_name: string;
  status: string;
  created_at: string;
}

export interface DirectMessage {
  id: string;
  seq: number;
  sender_id: string;
  recipient_id: string;
  body: string;
  client_message_id: string;
  created_at: string;
  read_at: string | null;
}

export interface Block {
  user_id: string;
  handle: string;
  display_name: string;
  created_at: string;
}

export interface SocialActivity {
  incoming_requests: FriendRequest[];
  unread_messages: number;
  recent_messages: DirectMessage[];
}

export interface Consent {
  id: string;
  publication_id: string;
  object_id: string;
  title: string;
  publisher_name: string;
  status: "pending" | "approved" | "declined";
  created_at: string;
  responded_at: string | null;
}

export interface LivingPulse {
  items: PulseItem[];
  social: SocialActivity | null;
  publication_consents: Consent[];
  generated_at: string;
}

export interface LensVersion {
  version: number;
  event_type: string;
  action?: string;
  actor: { type: string; user_id?: string; display_name?: string };
  created_at: string;
}

export interface BranchChild {
  object_id: string;
  title: string;
  version: number;
  source_version: number;
  created_at: string;
}

export interface DeriveLens {
  object: ObjectView;
  lineage: ObjectView["lineage"] | null;
  versions: LensVersion[];
  children: BranchChild[];
  inheritance_fields: string[];
  proposals: Proposal[];
}

export interface ObjectVersion {
  object_id: string;
  object_type: ObjectType;
  version: number;
  current_version: number;
  state: Record<string, unknown>;
}

export interface SystemStatus {
  version: string;
  commit: string;
  build_time: string;
  model_mode: "fixture" | "real";
  model_name: string;
  public_base_url: string;
  database_ok: boolean;
  capabilities_ready: boolean;
  quota: {
    daily_calls: number;
    daily_input_chars: number;
    concurrency: number;
    global_daily_calls: number;
    kill_switch: boolean;
  };
  rate_limits: { ai_per_hour: number; ai_burst: number; api_per_minute: number };
}

export interface Discussion {
  id: string;
  seq: number;
  object_id: string;
  author_id: string;
  author_name: string;
  body: string;
  client_message_id: string;
  created_at: string;
}

export interface Publication {
  id: string;
  object_id: string;
  source_version: number;
  publisher_id: string;
  publisher_name: string;
  title: string;
  summary: string;
  object_type: ObjectType;
  presentation_template: PresentationTemplate;
  status: "pending_consent" | "active" | "declined" | "revoked";
  derive_allowed: boolean;
  license: "all_rights_reserved" | "cc_by_4.0" | "cc_by_sa_4.0";
  created_at: string;
  updated_at: string;
  revoked_at: string | null;
}

export interface PublicWorld {
  id: string;
  title: string;
  summary: string;
  object_type: ObjectType;
  presentation_template: PresentationTemplate;
  source_version: number;
  publisher_name: string;
  snapshot?: Record<string, unknown>;
  derive_allowed: boolean;
  license: string;
  created_at: string;
}

export interface PublicLineage {
  source: {
    publication_id: string;
    title: string;
    source_version: number;
    inherited_fields: string[];
  } | null;
  derived: Array<{ publication_id: string; title: string; source_version: number }>;
}

export async function getWorldStage(signal?: AbortSignal): Promise<WorldStage> {
  return request<WorldStage>("/api/v1/world/stage", { signal });
}

export async function getPulse(signal?: AbortSignal): Promise<LivingPulse> {
  return request<LivingPulse>("/api/v1/pulse", { signal });
}

export async function getLens(id: string, signal?: AbortSignal): Promise<DeriveLens> {
  return request<DeriveLens>(`/api/v1/objects/${encodeURIComponent(id)}/lens`, { signal });
}

export async function getObjectVersion(
  id: string,
  version: number,
  signal?: AbortSignal
): Promise<ObjectVersion> {
  return request<ObjectVersion>(
    `/api/v1/objects/${encodeURIComponent(id)}/versions/${version}`,
    { signal }
  );
}

export async function listDiscussions(
  id: string,
  cursor?: string,
  signal?: AbortSignal
): Promise<{ items: Discussion[]; next_cursor: string | null }> {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return request(
    `/api/v1/objects/${encodeURIComponent(id)}/discussions?${params.toString()}`,
    { signal }
  );
}

export async function postDiscussion(
  id: string,
  body: string,
  clientMessageID: string
): Promise<Discussion> {
  return request<Discussion>(`/api/v1/objects/${encodeURIComponent(id)}/discussions`, {
    method: "POST",
    body: { body, client_message_id: clientMessageID }
  });
}

export async function listFriends(signal?: AbortSignal): Promise<{ items: Friend[] }> {
  return request("/api/v1/social/friends", { signal });
}

export async function listFriendRequests(
  direction: "incoming" | "outgoing",
  signal?: AbortSignal
): Promise<{ items: FriendRequest[] }> {
  return request(`/api/v1/social/friend-requests?direction=${direction}`, { signal });
}

export async function sendFriendRequest(handle: string): Promise<FriendRequest> {
  return request<FriendRequest>("/api/v1/social/friend-requests", {
    method: "POST",
    body: { handle }
  });
}

export async function respondFriendRequest(
  id: string,
  accept: boolean
): Promise<FriendRequest> {
  return request<FriendRequest>(
    `/api/v1/social/friend-requests/${encodeURIComponent(id)}/${accept ? "accept" : "decline"}`,
    { method: "POST" }
  );
}

export async function removeFriend(userID: string): Promise<void> {
  await request<void>(`/api/v1/social/friends/${encodeURIComponent(userID)}`, {
    method: "DELETE"
  });
}

export async function listDirectMessages(
  withUserID: string,
  cursor?: string,
  signal?: AbortSignal
): Promise<{ items: DirectMessage[]; next_cursor: string | null }> {
  const params = new URLSearchParams({ with: withUserID, limit: "50" });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return request(`/api/v1/social/messages?${params.toString()}`, { signal });
}

export async function sendDirectMessage(
  toUserID: string,
  body: string,
  clientMessageID: string
): Promise<DirectMessage> {
  return request<DirectMessage>("/api/v1/social/messages", {
    method: "POST",
    body: { to_user_id: toUserID, body, client_message_id: clientMessageID }
  });
}

export async function markDirectMessagesRead(withUserID: string): Promise<{ marked: number }> {
  return request("/api/v1/social/messages/read", {
    method: "POST",
    body: { with_user_id: withUserID }
  });
}

export async function listBlocks(signal?: AbortSignal): Promise<{ items: Block[] }> {
  return request("/api/v1/social/blocks", { signal });
}

export async function blockUser(userID: string): Promise<void> {
  await request<void>("/api/v1/social/blocks", {
    method: "POST",
    body: { blocked_user_id: userID }
  });
}

export async function unblockUser(userID: string): Promise<void> {
  await request<void>(`/api/v1/social/blocks/${encodeURIComponent(userID)}`, {
    method: "DELETE"
  });
}

export async function listPublications(
  objectID: string,
  signal?: AbortSignal
): Promise<{ items: Publication[] }> {
  return request(`/api/v1/objects/${encodeURIComponent(objectID)}/publications`, { signal });
}

export async function createPublication(
  objectID: string,
  input: {
    source_version?: number;
    derive_allowed: boolean;
    license: "all_rights_reserved" | "cc_by_4.0" | "cc_by_sa_4.0";
  }
): Promise<Publication> {
  return idempotentRequest<Publication>(
    "POST",
    `/api/v1/objects/${encodeURIComponent(objectID)}/publications`,
    input
  );
}

export async function revokePublication(publicationID: string): Promise<void> {
  await request<void>(`/api/v1/publications/${encodeURIComponent(publicationID)}`, {
    method: "DELETE"
  });
}

export async function listPublicationConsents(
  signal?: AbortSignal
): Promise<{ items: Consent[] }> {
  return request("/api/v1/publication-consents", { signal });
}

export async function respondPublicationConsent(
  id: string,
  approve: boolean
): Promise<Consent> {
  return request<Consent>(
    `/api/v1/publication-consents/${encodeURIComponent(id)}/${approve ? "approve" : "decline"}`,
    { method: "POST" }
  );
}

export async function listPublicWorlds(
  cursor?: string,
  signal?: AbortSignal
): Promise<{ items: PublicWorld[]; next_cursor: string | null }> {
  const params = new URLSearchParams({ limit: "20" });
  if (cursor) {
    params.set("cursor", cursor);
  }
  return request(`/api/v1/public/worlds?${params.toString()}`, { signal });
}

export async function getPublicWorld(
  id: string,
  signal?: AbortSignal
): Promise<PublicWorld> {
  return request<PublicWorld>(`/api/v1/public/worlds/${encodeURIComponent(id)}`, { signal });
}

export async function getPublicLineage(
  id: string,
  signal?: AbortSignal
): Promise<PublicLineage> {
  return request<PublicLineage>(
    `/api/v1/public/worlds/${encodeURIComponent(id)}/lineage`,
    { signal }
  );
}

export async function derivePublicWorld(
  publicationID: string,
  input: { title: string; inheritance_fields: string[] }
): Promise<ObjectView> {
  return idempotentRequest<ObjectView>(
    "POST",
    `/api/v1/public/worlds/${encodeURIComponent(publicationID)}/derive`,
    input
  );
}

export async function getSystemStatus(signal?: AbortSignal): Promise<SystemStatus> {
  return request<SystemStatus>("/api/v1/system/status", { signal });
}

export interface ModelCheck {
  status: "ok" | "fixture" | "unauthorized" | "unreachable" | "error";
  mode: "fixture" | "real";
  model_name: string;
  detail: string;
}

export async function checkModelConnection(): Promise<ModelCheck> {
  return request<ModelCheck>("/api/v1/system/model-check");
}

export async function reportPublicWorld(publicationID: string, reason: string): Promise<void> {
  await request<void>(`/api/v1/public/worlds/${encodeURIComponent(publicationID)}/report`, {
    method: "POST",
    body: { reason }
  });
}
