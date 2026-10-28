// SPDX-License-Identifier: Apache-2.0

/**
 * Derive World ESM SDK.
 *
 * The client targets the frozen OpenAPI 3.1 contract. It has zero runtime
 * dependencies, supports personal access tokens (`Authorization: Bearer`),
 * and never weakens CSRF, CORS, or the server's authorization rules. Browser
 * cookie sessions remain same-origin only; write calls should use a PAT.
 */

export type ModelMode = "fixture" | "real";
export type ObjectType = "collaborative" | "interactive" | "living_scene" | "open_world";
export type PresentationTemplate =
  | "collaborative_v1"
  | "interactive_v1"
  | "living_scene_v1"
  | "open_world_v1";
export type Permission = "read" | "contribute" | "derive" | "invite" | "manage";
export type CapabilityId =
  | "object.read"
  | "object.context"
  | "object.propose_change"
  | "object.derive"
  | "content.structure";
export type ProviderId = "trusted_core" | "reference.content-structure";
export type InvocationStatus =
  | "pending"
  | "succeeded"
  | "failed"
  | "denied"
  | "timeout"
  | "unknown";

export interface Meta {
  version: string;
  commit: string;
  build_time: string;
  model_mode: ModelMode;
}

export interface ApiErrorPayload {
  code: string;
  message: string;
  request_id: string;
  details?: Record<string, unknown>;
}

export interface ApiErrorEnvelope {
  error: ApiErrorPayload;
}

export class DeriveWorldApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly requestId: string;
  readonly details: Record<string, unknown>;

  constructor(status: number, payload: ApiErrorPayload) {
    super(payload.message);
    this.name = "DeriveWorldApiError";
    this.code = payload.code;
    this.status = status;
    this.requestId = payload.request_id;
    this.details = payload.details ?? {};
  }
}

export interface ClientOptions {
  /** Base URL without a trailing slash. Defaults to same-origin (""). */
  baseUrl?: string;
  /** Personal access token. Sent as `Authorization: Bearer`. */
  token?: string;
  /** Injectable fetch implementation for tests and non-browser runtimes. */
  fetch?: typeof fetch;
  /** Injectable request ID generator. */
  requestId?: () => string;
}

export interface WriteOptions {
  /** Required by the API; generated when omitted. */
  idempotencyKey?: string;
  signal?: AbortSignal;
}

export interface ReadOptions {
  signal?: AbortSignal;
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface ObjectSummary {
  id: string;
  object_type: ObjectType;
  title: string;
  version: number;
  owner_id: string;
  role: string;
  permissions: Permission[];
  created_at: string;
  updated_at: string;
}

export interface DeriveLineage {
  parent: { object_id: string; version: number };
  inherited_fields: string[];
  publication_id?: string;
  publication_title?: string;
  public?: boolean;
}

export interface ObjectView {
  id: string;
  object_type: ObjectType;
  presentation_template: PresentationTemplate;
  schema_version: number;
  creator_id: string;
  owner_id: string;
  version: number;
  state: Record<string, unknown>;
  shared_context: Record<string, unknown>;
  status: "active" | "deleted";
  permissions: Permission[];
  role: string;
  created_at: string;
  updated_at: string;
  lineage?: DeriveLineage;
}

export interface CreateObjectInput {
  object_type: ObjectType;
  title: string;
  content?: Record<string, unknown>;
  shared_context?: Record<string, unknown>;
}

export interface ObjectEvent {
  id: string;
  seq: number;
  object_version: number;
  event_type: string;
  actor: { type: string; user_id?: string; display_name?: string };
  payload: Record<string, unknown>;
  created_at: string;
}

export interface ActionInput {
  action: string;
  input: Record<string, unknown>;
  expected_version: number;
}

export interface LocalizedLabel {
  en: string;
  zh: string;
}

export interface WorldCondition {
  entity_id?: string;
  field?: string;
  op?: string;
  value?: unknown;
  all?: WorldCondition[];
  any?: WorldCondition[];
  not?: WorldCondition;
}

export interface WorldAction {
  capability: string;
  action: string;
  params: Record<string, unknown>;
}

export interface WorldBehavior {
  id: string;
  name?: string;
  condition?: WorldCondition;
  actions: WorldAction[];
}

export interface WorldViewNode {
  id: string;
  capability: string;
  props: Record<string, unknown>;
  children?: WorldViewNode[];
  visible_when?: WorldCondition;
}

export interface WorldExperience {
  protocol: 1;
  root: WorldViewNode;
  behaviors?: WorldBehavior[];
}

export interface WorldCapabilityProperty {
  type: "string" | "number" | "integer" | "boolean" | "array" | "object";
  title: LocalizedLabel;
  description?: LocalizedLabel;
  format?: string;
  enum?: string[];
  minimum?: number;
  maximum?: number;
  max_length?: number;
  items?: WorldCapabilityProperty;
  properties?: Record<string, WorldCapabilityProperty>;
  required?: string[];
}

export interface WorldCapabilityDescriptor {
  id: string;
  kind: "presentation" | "behavior";
  version: number;
  title: LocalizedLabel;
  description: LocalizedLabel;
  allows_children?: boolean;
  child_scope?: "item";
  child_kind_prop?: string;
  params?: {
    type: "object";
    properties: Record<string, WorldCapabilityProperty>;
    required?: string[];
  };
  actions?: Array<{
    id: string;
    title: LocalizedLabel;
    description: LocalizedLabel;
    params?: {
      type: "object";
      properties: Record<string, WorldCapabilityProperty>;
      required?: string[];
    };
  }>;
}

export interface DeriveInput {
  title: string;
  source_version?: number;
  inheritance_fields: string[];
}

export interface LineageResponse {
  object: { object_id: string; version: number };
  ancestors: Array<{
    object_id?: string;
    title?: string;
    version?: number;
    depth: number;
    redacted?: boolean;
  }>;
  descendants: LineageDescendant[];
  max_depth: number;
  truncated: boolean;
  source?: DeriveLineage;
}

export interface LineageDescendant {
  object_id: string;
  title: string;
  version: number;
  depth: number;
  source_version: number;
  children: LineageDescendant[];
}

export interface HandoffProjection {
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
      actor_type: "user" | "personal_ai" | "system";
      created_at: string;
      summary: string;
    }>;
  };
  permitted_actions: Array<"read" | "contribute" | "derive">;
  data_provenance: Array<{
    field: string;
    source: "object_state" | "shared_context" | "event";
    object_version: number;
    truncated?: boolean;
  }>;
  context_schema_version: 1;
  truncated: boolean;
}

export interface CapabilityExecutionConstraints {
  timeout_ms: number;
  max_input_bytes: number;
  max_output_bytes: number;
}

export interface CapabilityDescriptor {
  capability_id: CapabilityId;
  contract_version: "1.0";
  provider_id: ProviderId;
  description: string;
  input_schema: Record<string, unknown>;
  output_schema: Record<string, unknown>;
  required_scopes: Permission[];
  supported_operations: string[];
  execution_constraints: CapabilityExecutionConstraints;
}

export interface CapabilityInvocation<TOutput = unknown> {
  id: string;
  capability_id: CapabilityId;
  contract_version: "1.0";
  provider_id: ProviderId;
  status: InvocationStatus;
  output: TOutput | null;
  error_code: string | null;
  duration_ms: number | null;
  created_at: string;
  completed_at: string | null;
}

export interface CapabilityInvocationResult<TOutput = unknown> {
  id: string;
  capability_id: CapabilityId;
  status: "succeeded";
  output: TOutput;
  duration_ms: number;
  created_at: string;
  completed_at: string;
}

export interface CapabilityInvocationPending {
  id: string;
  capability_id: CapabilityId;
  status: "pending";
  confirmation_id: string;
  created_at: string;
}

export interface ObjectReadInput {
  object_id: string;
}
export interface ObjectContextInput {
  object_id: string;
}
export interface ObjectProposeChangeInput {
  object_id: string;
  action:
    | "set_title"
    | "set_summary"
    | "set_body"
    | "add_checklist_item"
    | "toggle_checklist_item"
    | "remove_checklist_item"
    | "add_tag"
    | "remove_tag"
    | "cast_vote"
    | "set_premise"
    | "add_element"
    | "update_element"
    | "remove_element"
    | "move_element"
    | "add_kind"
    | "update_kind"
    | "remove_kind"
    | "add_node"
    | "update_node"
    | "remove_node"
    | "move_node"
    | "set_node_field"
    | "set_node_status"
    | "touch_node"
    | "add_link"
    | "remove_link"
    | "add_rule"
    | "remove_rule"
    | "set_views";
  input: Record<string, unknown>;
  expected_version: number;
}
export interface ObjectDeriveCapabilityInput {
  object_id: string;
  source_version?: number;
  inheritance_fields: Array<
    "summary" | "body" | "checklist" | "tags" | "prompt" | "options"
    | "premise" | "elements" | "structure" | "views"
  >;
  title: string;
}
export interface ContentStructureInput {
  object_id: string;
  text: string;
  format: "plain_text" | "markdown";
}

export interface CapabilityInputMap {
  "object.read": ObjectReadInput;
  "object.context": ObjectContextInput;
  "object.propose_change": ObjectProposeChangeInput;
  "object.derive": ObjectDeriveCapabilityInput;
  "content.structure": ContentStructureInput;
}

export interface ObjectReadOutput {
  object: {
    id: string;
    type: ObjectType;
    version: number;
    state: Record<string, unknown>;
    shared_context: Record<string, unknown>;
  };
}
export interface ObjectProposeChangeOutput {
  proposal_id: string;
  status: "pending";
}
export interface ObjectDeriveOutput {
  child_object_id: string;
}
export interface ContentStructureOutput {
  items: Array<{
    index: number;
    kind: "heading" | "paragraph" | "list_item" | "code_block" | "quote";
    text: string;
    level?: number;
  }>;
  item_count: number;
  truncated: boolean;
  format: "plain_text" | "markdown";
}

export interface CapabilityOutputMap {
  "object.read": ObjectReadOutput;
  "object.context": HandoffProjection;
  "object.propose_change": ObjectProposeChangeOutput;
  "object.derive": ObjectDeriveOutput;
  "content.structure": ContentStructureOutput;
}

function defaultRequestId(): string {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) {
    return uuid;
  }
  return `dw-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/**
 * Generates a value for the required `Idempotency-Key` header. Hyphens are
 * permitted by the frozen `[A-Za-z0-9_-]{16,128}` format.
 */
export function newIdempotencyKey(): string {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) {
    return uuid;
  }
  return `idem-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 18)}`;
}

function trimBaseUrl(value: string): string {
  return value.replace(/\/+$/, "");
}

function queryString(values: Record<string, string | number | undefined | null>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== null && value !== "") {
      params.set(key, String(value));
    }
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : "";
}

export class DeriveWorldClient {
  private readonly baseUrl: string;
  private readonly token: string | undefined;
  private readonly fetchImpl: typeof fetch;
  private readonly requestId: () => string;

  constructor(options: ClientOptions = {}) {
    this.baseUrl = trimBaseUrl(options.baseUrl ?? "");
    this.token = options.token;
    this.fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.requestId = options.requestId ?? defaultRequestId;
  }

  async meta(): Promise<Meta> {
    return this.request<Meta>("GET", "/api/v1/meta");
  }

  async health(): Promise<string> {
    return this.requestText("GET", "/healthz");
  }

  async ready(): Promise<string> {
    return this.requestText("GET", "/readyz");
  }

  /** URL of the OpenAPI 3.1 contract served by the application. */
  openapiUrl(): string {
    return `${this.baseUrl}/api/v1/openapi.yaml`;
  }

  // ---- Objects ----

  async listObjects(
    query: { scope?: "owned" | "shared" | "all"; limit?: number; cursor?: string } = {},
    options: ReadOptions = {}
  ): Promise<Page<ObjectSummary>> {
    return this.request<Page<ObjectSummary>>(
      "GET",
      `/api/v1/objects${queryString(query)}`,
      { signal: options.signal }
    );
  }

  async createObject(input: CreateObjectInput, options: WriteOptions = {}): Promise<ObjectView> {
    return this.request<ObjectView>("POST", "/api/v1/objects", {
      body: input,
      idempotencyKey: options.idempotencyKey ?? newIdempotencyKey(),
      signal: options.signal
    });
  }

  async getObject(objectId: string, options: ReadOptions = {}): Promise<ObjectView> {
    return this.request<ObjectView>("GET", `/api/v1/objects/${encodeURIComponent(objectId)}`, {
      signal: options.signal
    });
  }

  async getSharedContext(
    objectId: string,
    options: ReadOptions = {}
  ): Promise<Record<string, unknown>> {
    const object = await this.getObject(objectId, options);
    return object.shared_context;
  }

  async listEvents(
    objectId: string,
    query: { limit?: number; cursor?: string } = {},
    options: ReadOptions = {}
  ): Promise<Page<ObjectEvent>> {
    return this.request<Page<ObjectEvent>>(
      "GET",
      `/api/v1/objects/${encodeURIComponent(objectId)}/events${queryString(query)}`,
      { signal: options.signal }
    );
  }

  async postAction(
    objectId: string,
    input: ActionInput,
    options: WriteOptions = {}
  ): Promise<ObjectView> {
    return this.request<ObjectView>(
      "POST",
      `/api/v1/objects/${encodeURIComponent(objectId)}/actions`,
      {
        body: input,
        idempotencyKey: options.idempotencyKey ?? newIdempotencyKey(),
        signal: options.signal
      }
    );
  }

  async derive(
    objectId: string,
    input: DeriveInput,
    options: WriteOptions = {}
  ): Promise<ObjectView> {
    return this.request<ObjectView>(
      "POST",
      `/api/v1/objects/${encodeURIComponent(objectId)}/derive`,
      {
        body: input,
        idempotencyKey: options.idempotencyKey ?? newIdempotencyKey(),
        signal: options.signal
      }
    );
  }

  async getLineage(
    objectId: string,
    depth?: number,
    options: ReadOptions = {}
  ): Promise<LineageResponse> {
    return this.request<LineageResponse>(
      "GET",
      `/api/v1/objects/${encodeURIComponent(objectId)}/lineage${queryString({ depth })}`,
      { signal: options.signal }
    );
  }

  async getHandoff(objectId: string, options: ReadOptions = {}): Promise<HandoffProjection> {
    return this.request<HandoffProjection>(
      "GET",
      `/api/v1/objects/${encodeURIComponent(objectId)}/handoff`,
      { signal: options.signal }
    );
  }

  // ---- World experience ----

  async listWorldCapabilities(
    options: ReadOptions = {}
  ): Promise<{ items: WorldCapabilityDescriptor[] }> {
    return this.request<{ items: WorldCapabilityDescriptor[] }>(
      "GET",
      "/api/v1/world-capabilities",
      { signal: options.signal }
    );
  }

  async setExperience(
    objectId: string,
    experience: WorldExperience | null,
    expectedVersion: number,
    options: WriteOptions = {}
  ): Promise<ObjectView> {
    return this.postAction(
      objectId,
      { action: "set_experience", input: { experience }, expected_version: expectedVersion },
      options
    );
  }

  async invokeBehavior(
    objectId: string,
    input: { behavior_id: string; entity_id?: string; value?: unknown },
    expectedVersion: number,
    options: WriteOptions = {}
  ): Promise<ObjectView> {
    return this.postAction(
      objectId,
      { action: "invoke_behavior", input, expected_version: expectedVersion },
      options
    );
  }

  // ---- Capabilities ----

  async listCapabilities(options: ReadOptions = {}): Promise<Page<CapabilityDescriptor>> {
    return this.request<Page<CapabilityDescriptor>>("GET", "/api/v1/capabilities", {
      signal: options.signal
    });
  }

  async getCapability(
    capabilityId: CapabilityId,
    options: ReadOptions = {}
  ): Promise<CapabilityDescriptor> {
    return this.request<CapabilityDescriptor>(
      "GET",
      `/api/v1/capabilities/${encodeURIComponent(capabilityId)}`,
      { signal: options.signal }
    );
  }

  async invokeCapability<K extends CapabilityId>(
    capabilityId: K,
    input: CapabilityInputMap[K],
    options: WriteOptions = {}
  ): Promise<CapabilityInvocationResult<CapabilityOutputMap[K]> | CapabilityInvocationPending> {
    return this.request<CapabilityInvocationResult<CapabilityOutputMap[K]> | CapabilityInvocationPending>(
      "POST",
      `/api/v1/capabilities/${encodeURIComponent(capabilityId)}/invoke`,
      {
        body: input,
        idempotencyKey: options.idempotencyKey ?? newIdempotencyKey(),
        signal: options.signal
      }
    );
  }

  async getCapabilityInvocation(
    invocationId: string,
    options: ReadOptions = {}
  ): Promise<CapabilityInvocation> {
    return this.request<CapabilityInvocation>(
      "GET",
      `/api/v1/capability-invocations/${encodeURIComponent(invocationId)}`,
      { signal: options.signal }
    );
  }

  // ---- Transport ----

  private async requestText(method: string, path: string): Promise<string> {
    const response = await this.doFetch(method, path, {});
    if (!response.ok) {
      await this.throwApiError(response);
    }
    return response.text();
  }

  private async request<T>(
    method: string,
    path: string,
    options: {
      body?: unknown;
      idempotencyKey?: string;
      signal?: AbortSignal;
    } = {}
  ): Promise<T> {
    const response = await this.doFetch(method, path, options);
    if (!response.ok) {
      await this.throwApiError(response);
    }
    if (response.status === 204) {
      return undefined as T;
    }
    return (await response.json()) as T;
  }

  private doFetch(
    method: string,
    path: string,
    options: { body?: unknown; idempotencyKey?: string; signal?: AbortSignal }
  ): Promise<Response> {
    const headers = new Headers();
    headers.set("Accept", "application/json");
    if (!headers.has("X-Request-Id")) {
      headers.set("X-Request-Id", this.requestId());
    }
    if (this.token) {
      headers.set("Authorization", `Bearer ${this.token}`);
    }
    if (options.idempotencyKey) {
      headers.set("Idempotency-Key", options.idempotencyKey);
    }
    let body: string | undefined;
    if (options.body !== undefined) {
      headers.set("Content-Type", "application/json");
      body = JSON.stringify(options.body);
    }
    return this.fetchImpl(`${this.baseUrl}${path}`, {
      method,
      headers,
      body,
      signal: options.signal,
      // Never include cross-origin credentials. Browser cookie sessions stay
      // same-origin and are not a substitute for a PAT.
      credentials: "same-origin"
    });
  }

  private async throwApiError(response: Response): Promise<never> {
    let payload: ApiErrorPayload = {
      code: "internal",
      message: `request failed with status ${response.status}`,
      request_id: response.headers.get("X-Request-Id") ?? ""
    };
    try {
      const body = (await response.json()) as Partial<ApiErrorEnvelope>;
      if (body.error && typeof body.error.code === "string") {
        payload = {
          code: body.error.code,
          message: body.error.message ?? payload.message,
          request_id: body.error.request_id ?? payload.request_id,
          details: body.error.details
        };
      }
    } catch {
      // Non-JSON error body: keep the status-based fallback.
    }
    throw new DeriveWorldApiError(response.status, payload);
  }
}
