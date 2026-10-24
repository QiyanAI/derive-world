// SPDX-License-Identifier: Apache-2.0

//go:build integration

package security_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QiyanAI/derive-world/internal/config"
)

func TestG04FiveCapabilitiesThroughBroker(t *testing.T) {
	env, provider := newG04Env(t, g04Success)
	a := env.register(t, "g04five")
	object := a.createObject(t, "collaborative", "G04 object", map[string]any{
		"body": "initial body",
	})

	status, body, _ := a.call(t, http.MethodGet, "/api/v1/capabilities", callOptions{})
	requireStatus(t, status, http.StatusOK, body)
	if strings.Contains(string(body), "base_url") || strings.Contains(string(body), "secret") {
		t.Fatalf("capability descriptors exposed provider internals: %s", body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("capability count = %d", len(page.Items))
	}
	for _, item := range page.Items {
		if _, ok := item["enabled"]; ok {
			t.Fatal("descriptor exposed internal enabled state")
		}
	}

	readStatus, readBody, _ := a.invokeCapability(t, "object.read",
		map[string]any{"object_id": object.ID}, newIdempotencyKey())
	requireStatus(t, readStatus, http.StatusOK, readBody)
	readInvocation := decodeInvocation(t, readBody)
	if readInvocation.Status != "succeeded" || len(readInvocation.Output) == 0 {
		t.Fatalf("read invocation = %+v", readInvocation)
	}
	var readOutput struct {
		Object struct {
			ID            string         `json:"id"`
			Type          string         `json:"type"`
			Version       int            `json:"version"`
			State         map[string]any `json:"state"`
			SharedContext map[string]any `json:"shared_context"`
		} `json:"object"`
	}
	if err := json.Unmarshal(readInvocation.Output, &readOutput); err != nil {
		t.Fatalf("decode read output: %v", err)
	}
	if readOutput.Object.ID != object.ID || readOutput.Object.Version != 1 ||
		readOutput.Object.Type != "collaborative" {
		t.Fatalf("read output = %+v", readOutput.Object)
	}

	contextStatus, contextBody, _ := a.invokeCapability(t, "object.context",
		map[string]any{"object_id": object.ID}, newIdempotencyKey())
	requireStatus(t, contextStatus, http.StatusOK, contextBody)
	contextInvocation := decodeInvocation(t, contextBody)
	var projection map[string]any
	if err := json.Unmarshal(contextInvocation.Output, &projection); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if projection["object_id"] != object.ID ||
		projection["context_schema_version"] != float64(1) {
		t.Fatalf("projection = %v", projection)
	}
	actions, _ := projection["permitted_actions"].([]any)
	if len(actions) < 3 {
		t.Fatalf("permitted_actions = %v", actions)
	}

	proposeStatus, proposeBody, _ := a.invokeCapability(t, "object.propose_change",
		map[string]any{
			"object_id": object.ID, "action": "set_summary",
			"input": map[string]any{"summary": "proposal"}, "expected_version": 1,
		}, newIdempotencyKey())
	requireStatus(t, proposeStatus, http.StatusOK, proposeBody)
	proposeInvocation := decodeInvocation(t, proposeBody)
	var proposalOutput struct {
		ProposalID string `json:"proposal_id"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(proposeInvocation.Output, &proposalOutput); err != nil {
		t.Fatalf("decode proposal output: %v", err)
	}
	if proposalOutput.Status != "pending" || proposalOutput.ProposalID == "" {
		t.Fatalf("proposal output = %+v", proposalOutput)
	}
	versionStatus, versionBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, versionStatus, http.StatusOK, versionBody)
	var unchanged objectResponse
	if err := json.Unmarshal(versionBody, &unchanged); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	if unchanged.Version != 1 {
		t.Fatalf("proposal changed the object version to %d", unchanged.Version)
	}

	deriveStatus, deriveBody, _ := a.invokeCapability(t, "object.derive",
		map[string]any{
			"object_id": object.ID, "title": "Capability child",
			"inheritance_fields": []string{"summary"},
		}, newIdempotencyKey())
	requireStatus(t, deriveStatus, http.StatusOK, deriveBody)
	deriveInvocation := decodeInvocation(t, deriveBody)
	var deriveOutput struct {
		ChildObjectID string `json:"child_object_id"`
	}
	if err := json.Unmarshal(deriveInvocation.Output, &deriveOutput); err != nil {
		t.Fatalf("decode derive output: %v", err)
	}
	childStatus, childBody, _ := a.call(t, http.MethodGet,
		"/api/v1/objects/"+deriveOutput.ChildObjectID, callOptions{})
	requireStatus(t, childStatus, http.StatusOK, childBody)
	var child objectResponse
	if err := json.Unmarshal(childBody, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if child.Version != 1 || child.OwnerID != a.id.String() || child.Lineage == nil ||
		child.Lineage.Parent.ObjectID != object.ID || child.Lineage.Parent.Version != 1 {
		t.Fatalf("child = %+v", child)
	}

	contentInput := map[string]any{
		"object_id": object.ID, "text": "sensitive structure text", "format": "plain_text",
	}
	contentKey := newIdempotencyKey()
	contentStatus, contentBody, _ := a.invokeCapability(t, "content.structure",
		contentInput, contentKey)
	requireStatus(t, contentStatus, http.StatusOK, contentBody)
	contentInvocation := decodeInvocation(t, contentBody)
	var structure struct {
		Items     []map[string]any `json:"items"`
		ItemCount int              `json:"item_count"`
		Truncated bool             `json:"truncated"`
		Format    string           `json:"format"`
	}
	if err := json.Unmarshal(contentInvocation.Output, &structure); err != nil {
		t.Fatalf("decode structure: %v", err)
	}
	if structure.ItemCount != 1 || structure.Format != "plain_text" || structure.Truncated {
		t.Fatalf("structure = %+v", structure)
	}
	if provider.requests.Load() != 1 {
		t.Fatalf("provider requests = %d", provider.requests.Load())
	}

	inspectionStatus, inspectionBody, _ := a.call(t, http.MethodGet,
		"/api/v1/capability-invocations/"+contentInvocation.ID, callOptions{})
	requireStatus(t, inspectionStatus, http.StatusOK, inspectionBody)
	inspected := decodeInvocation(t, inspectionBody)
	if inspected.Status != "succeeded" || len(inspected.Output) == 0 ||
		string(inspected.Output) == "null" ||
		inspected.ProviderID != "reference.content-structure" ||
		inspected.ContractVersion != "1.0" {
		t.Fatalf("inspected invocation = %+v", inspected)
	}
	var readAudit int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'capability.invoke' AND resource_id = $1`,
		mustUUID(t, contentInvocation.ID)).Scan(&readAudit); err != nil {
		t.Fatalf("count invocation read audit: %v", err)
	}
	if readAudit < 2 {
		t.Fatalf("invocation read audit count = %d", readAudit)
	}

	replayStatus, replayBody, replayHeaders := a.invokeCapability(t, "content.structure",
		contentInput, contentKey)
	requireStatus(t, replayStatus, http.StatusOK, replayBody)
	if replayHeaders.Get("Idempotency-Replayed") != "true" {
		t.Fatal("idempotent capability replay was not marked")
	}
	if provider.requests.Load() != 1 {
		t.Fatalf("replay re-executed the provider: %d", provider.requests.Load())
	}

	var definitionCount, syncedAudit int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM capability_definitions WHERE enabled = true`).Scan(&definitionCount); err != nil {
		t.Fatalf("count definitions: %v", err)
	}
	if definitionCount != 5 {
		t.Fatalf("enabled capability definitions = %d", definitionCount)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE action = 'capability.definition_synced'`).Scan(&syncedAudit); err != nil {
		t.Fatalf("count definition sync audit: %v", err)
	}
	if syncedAudit == 0 {
		t.Fatal("capability.definition_synced audit is missing")
	}

	var invocationCount, auditInvoke, auditCompleted int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM capability_invocations WHERE actor_user_id = $1`,
		a.id.String()).Scan(&invocationCount); err != nil {
		t.Fatalf("count invocations: %v", err)
	}
	if invocationCount != 5 {
		t.Fatalf("invocation count = %d, want 5", invocationCount)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE actor_user_id = $1 AND action = 'capability.invoke'`,
		a.id.String()).Scan(&auditInvoke); err != nil {
		t.Fatalf("count invoke audit: %v", err)
	}
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE actor_user_id = $1 AND action = 'capability.completed'`,
		a.id.String()).Scan(&auditCompleted); err != nil {
		t.Fatalf("count completed audit: %v", err)
	}
	if auditInvoke < 5 || auditCompleted < 5 {
		t.Fatalf("invoke audit=%d completed audit=%d", auditInvoke, auditCompleted)
	}

	var redacted []byte
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT input_redacted FROM capability_invocations
 WHERE id = $1`, mustUUID(t, contentInvocation.ID)).Scan(&redacted); err != nil {
		t.Fatalf("read redacted input: %v", err)
	}
	if strings.Contains(string(redacted), "sensitive structure text") {
		t.Fatalf("redacted input leaked content: %s", redacted)
	}
}

func createObjectWithSharedContext(t *testing.T, user *testUser, title string,
	content, shared map[string]any) objectResponse {
	t.Helper()
	status, body, _ := user.call(t, http.MethodPost, "/api/v1/objects", callOptions{
		IdempotencyKey: newIdempotencyKey(),
		Body: map[string]any{
			"object_type": "collaborative", "title": title,
			"content": content, "shared_context": shared,
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create object status = %d body = %s", status, body)
	}
	var object objectResponse
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	return object
}

func TestG04ContextTruncationConformsToSchema(t *testing.T) {
	type provenance struct {
		Field     string `json:"field"`
		Truncated *bool  `json:"truncated"`
	}
	type projection struct {
		Truncated     bool         `json:"truncated"`
		Provenance    []provenance `json:"data_provenance"`
		Permitted     []string     `json:"permitted_actions"`
		ObjectVersion int          `json:"object_version"`
	}
	invokeContext := func(t *testing.T, user *testUser, objectID string) (int, capabilityInvocationResponse, projection) {
		t.Helper()
		status, body, _ := user.invokeCapability(t, "object.context",
			map[string]any{"object_id": objectID}, newIdempotencyKey())
		if status != http.StatusOK {
			return status, capabilityInvocationResponse{}, projection{}
		}
		invocation := decodeInvocation(t, body)
		var value projection
		if err := json.Unmarshal(invocation.Output, &value); err != nil {
			t.Fatalf("decode projection: %v", err)
		}
		return status, invocation, value
	}
	truncatedFields := func(value projection) map[string]bool {
		out := map[string]bool{}
		for _, entry := range value.Provenance {
			if entry.Truncated != nil && *entry.Truncated {
				out[entry.Field] = true
			}
		}
		return out
	}

	t.Run("untruncated", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-none")
		object := a.createObject(t, "collaborative", "Small", map[string]any{"body": "small"})
		status, _, value := invokeContext(t, a, object.ID)
		requireStatus(t, status, http.StatusOK, nil)
		if value.Truncated || len(truncatedFields(value)) != 0 {
			t.Fatalf("unexpected truncation: %+v", value)
		}
	})

	t.Run("state body", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-body")
		checklist := make([]map[string]any, 0, 40)
		for index := 0; index < 40; index++ {
			checklist = append(checklist, map[string]any{"text": strings.Repeat("y", 500), "done": false})
		}
		object := a.createObject(t, "collaborative", "Body truncation", map[string]any{
			"body": strings.Repeat("z", 20000), "checklist": checklist,
		})
		status, invocation, value := invokeContext(t, a, object.ID)
		requireStatus(t, status, http.StatusOK, nil)
		if !value.Truncated || !truncatedFields(value)["state.body"] {
			t.Fatalf("state.body truncation not marked: %+v", value)
		}
		if value.ObjectVersion < 1 || len(invocation.Output) == 0 {
			t.Fatalf("projection version/output invalid: version=%d", value.ObjectVersion)
		}
	})

	t.Run("shared context", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-shared")
		checklist := make([]map[string]any, 0, 50)
		for index := 0; index < 50; index++ {
			checklist = append(checklist, map[string]any{"text": strings.Repeat("q", 500), "done": false})
		}
		object := createObjectWithSharedContext(t, a, "Shared truncation",
			map[string]any{"body": "b", "checklist": checklist},
			map[string]any{"note": strings.Repeat("s", 7000)})
		status, _, value := invokeContext(t, a, object.ID)
		requireStatus(t, status, http.StatusOK, nil)
		fields := truncatedFields(value)
		if !value.Truncated || !fields["shared_context"] {
			t.Fatalf("shared_context truncation not marked: %+v", value)
		}
	})

	t.Run("recent events", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-events")
		checklist := make([]map[string]any, 0, 40)
		for index := 0; index < 40; index++ {
			checklist = append(checklist, map[string]any{"text": strings.Repeat("e", 500), "done": false})
		}
		object := a.createObject(t, "collaborative", "Event truncation", map[string]any{
			"body": strings.Repeat("z", 20000), "checklist": checklist,
		})
		for index := 0; index < 6; index++ {
			status, body, _ := a.postAction(t, object.ID, "set_summary",
				map[string]any{"summary": "s"}, 1+index)
			requireStatus(t, status, http.StatusOK, body)
		}
		status, _, value := invokeContext(t, a, object.ID)
		requireStatus(t, status, http.StatusOK, nil)
		if !value.Truncated || !truncatedFields(value)["permitted_object_context.recent_events"] {
			t.Fatalf("recent_events truncation not marked: %+v", value)
		}
	})

	t.Run("multiple fields", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-multi")
		checklist := make([]map[string]any, 0, 40)
		for index := 0; index < 40; index++ {
			checklist = append(checklist, map[string]any{"text": strings.Repeat("m", 500), "done": false})
		}
		object := createObjectWithSharedContext(t, a, "Multi truncation",
			map[string]any{"body": strings.Repeat("z", 20000), "checklist": checklist},
			map[string]any{"note": strings.Repeat("s", 7000)})
		status, _, value := invokeContext(t, a, object.ID)
		requireStatus(t, status, http.StatusOK, nil)
		fields := truncatedFields(value)
		if !fields["shared_context"] || !fields["state.body"] {
			t.Fatalf("multiple truncation markers missing: %+v", fields)
		}
	})

	t.Run("unprojectable maximum", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-max")
		checklist := make([]map[string]any, 0, 80)
		for index := 0; index < 80; index++ {
			checklist = append(checklist, map[string]any{"text": strings.Repeat("x", 500), "done": false})
		}
		object := a.createObject(t, "collaborative", "Max object", map[string]any{
			"body": strings.Repeat("z", 14000), "checklist": checklist,
		})
		status, body, _ := a.invokeCapability(t, "object.context",
			map[string]any{"object_id": object.ID}, newIdempotencyKey())
		requireStatus(t, status, http.StatusInternalServerError, body)
		code, _ := decodeCapabilityError(t, body)
		if code != "internal" {
			t.Fatalf("code = %s", code)
		}
	})

	t.Run("viewer permission clipping", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04trunc-viewer-a")
		b := env.register(t, "g04trunc-viewer-b")
		object := a.createObject(t, "collaborative", "Viewer projection", map[string]any{"body": "b"})
		invitation := a.createInvitation(t, object.ID, []string{"read"}, 1)
		status, body := b.acceptInvitation(t, *invitation.Token)
		requireStatus(t, status, http.StatusOK, body)
		status, _, value := invokeContext(t, b, object.ID)
		requireStatus(t, status, http.StatusOK, nil)
		if len(value.Permitted) != 1 || value.Permitted[0] != "read" {
			t.Fatalf("permitted_actions = %v", value.Permitted)
		}
	})
}

func TestG04AuthorizationAndPATScopes(t *testing.T) {
	env, provider := newG04Env(t, g04Success)
	a := env.register(t, "g04auth-a")
	b := env.register(t, "g04auth-b")
	object := a.createObject(t, "collaborative", "Authorization object", map[string]any{
		"body": "body",
	})

	status, body, _ := b.invokeCapability(t, "object.read",
		map[string]any{"object_id": object.ID}, newIdempotencyKey())
	requireStatus(t, status, http.StatusNotFound, body)
	code, responseInvocationID := decodeCapabilityError(t, body)
	if code != "not_found" || responseInvocationID == "" {
		t.Fatalf("code = %s invocation = %s", code, responseInvocationID)
	}
	invocationID, deniedStatus, _, errorCode := env.latestInvocation(t, b.id, "object.read")
	if deniedStatus != "denied" || errorCode == nil || *errorCode != "not_found" {
		t.Fatalf("denied invocation = %s %v", deniedStatus, errorCode)
	}
	if responseInvocationID != invocationID {
		t.Fatalf("denial invocation id = %s, recorded = %s", responseInvocationID, invocationID)
	}
	var redacted []byte
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT input_redacted FROM capability_invocations WHERE id = $1`,
		mustUUID(t, invocationID)).Scan(&redacted); err != nil {
		t.Fatalf("read denied redaction: %v", err)
	}
	if strings.Contains(string(redacted), object.ID) {
		t.Fatalf("denied input_redacted leaked the probed object id: %s", redacted)
	}
	if !strings.Contains(string(redacted), "sha256") {
		t.Fatalf("denied input_redacted did not hash the probed object id: %s", redacted)
	}
	var requestedHash string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT COALESCE(metadata->>'requested_object_id_sha256','')
  FROM audit_events
 WHERE action = 'capability.denied' AND resource_id = $1`,
		mustUUID(t, invocationID)).Scan(&requestedHash); err != nil {
		t.Fatalf("read denied audit metadata: %v", err)
	}
	if requestedHash == "" {
		t.Fatal("denied audit metadata omitted the requested object hash")
	}
	_, _, _, objectID := env.invocationRow(t, invocationID)
	if objectID != nil {
		t.Fatalf("unreadable denial recorded object_id %s", *objectID)
	}

	invitation := a.createInvitation(t, object.ID, []string{"read"}, 1)
	acceptStatus, acceptBody := b.acceptInvitation(t, *invitation.Token)
	requireStatus(t, acceptStatus, http.StatusOK, acceptBody)
	proposeStatus, proposeBody, _ := b.invokeCapability(t, "object.propose_change",
		map[string]any{
			"object_id": object.ID, "action": "set_summary",
			"input": map[string]any{"summary": "viewer"}, "expected_version": 1,
		}, newIdempotencyKey())
	requireStatus(t, proposeStatus, http.StatusForbidden, proposeBody)
	_, viewerStatus, _, viewerCode := env.latestInvocation(t, b.id, "object.propose_change")
	if viewerStatus != "denied" || viewerCode == nil || *viewerCode != "forbidden" {
		t.Fatalf("viewer denial = %s %v", viewerStatus, viewerCode)
	}

	readOnly := a.createToken(t, "read only", []string{"objects:read"})
	before := env.countInvocations(t, a.id, "object.read")
	patStatus, patBody, _ := env.bearerCall(t, readOnly.Plaintext, http.MethodPost,
		"/api/v1/capabilities/object.read/invoke", callOptions{
			Body:           map[string]any{"object_id": object.ID},
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, patStatus, http.StatusForbidden, patBody)
	if env.countInvocations(t, a.id, "object.read") != before+1 {
		t.Fatal("missing capabilities:invoke did not record a denied invocation")
	}

	capsOnly := a.createToken(t, "caps only", []string{"capabilities:invoke"})
	before = env.countInvocations(t, a.id, "object.read")
	capsStatus, capsBody, _ := env.bearerCall(t, capsOnly.Plaintext, http.MethodPost,
		"/api/v1/capabilities/object.read/invoke", callOptions{
			Body:           map[string]any{"object_id": object.ID},
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, capsStatus, http.StatusForbidden, capsBody)
	if env.countInvocations(t, a.id, "object.read") != before+1 {
		t.Fatal("operation-scope denial did not record a denied invocation")
	}

	full := a.createToken(t, "full", []string{"capabilities:invoke", "objects:read"})
	fullStatus, fullBody, _ := env.bearerCall(t, full.Plaintext, http.MethodPost,
		"/api/v1/capabilities/object.read/invoke", callOptions{
			Body:           map[string]any{"object_id": object.ID},
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, fullStatus, http.StatusOK, fullBody)
	fullInvocation := decodeInvocation(t, fullBody)

	overrideStatus, overrideBody, _ := a.invokeCapability(t, "object.read",
		map[string]any{"object_id": object.ID, "actor_user_id": b.id.String()},
		newIdempotencyKey())
	requireStatus(t, overrideStatus, http.StatusBadRequest, overrideBody)
	if env.auditCount(t, "authz.actor_override_attempt", a.id) < 1 {
		t.Fatal("actor override was not audited")
	}

	providerBefore := provider.requests.Load()
	urlStatus, urlBody, _ := a.invokeCapability(t, "content.structure",
		map[string]any{
			"object_id": object.ID, "text": "x", "format": "plain_text",
			"provider_url": "http://169.254.169.254/latest/meta-data",
		}, newIdempotencyKey())
	requireStatus(t, urlStatus, http.StatusBadRequest, urlBody)
	if provider.requests.Load() != providerBefore {
		t.Fatal("a request-supplied provider URL reached the provider")
	}

	unknownStatus, unknownBody, _ := a.invokeCapability(t, "object.unknown",
		map[string]any{"object_id": object.ID}, newIdempotencyKey())
	requireStatus(t, unknownStatus, http.StatusNotFound, unknownBody)

	crossStatus, crossBody, _ := b.call(t, http.MethodGet,
		"/api/v1/capability-invocations/"+fullInvocation.ID, callOptions{})
	requireStatus(t, crossStatus, http.StatusNotFound, crossBody)
	var notFoundAudit int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'authz.not_found' AND actor_user_id = $1
   AND resource_type = 'capability_invocation'`, b.id.String()).Scan(&notFoundAudit); err != nil {
		t.Fatalf("count invocation not-found audit: %v", err)
	}
	if notFoundAudit == 0 {
		t.Fatal("cross-user invocation read was not audited")
	}
}

func TestG04ProviderFailureModes(t *testing.T) {
	t.Run("unknown result", func(t *testing.T) {
		env, _ := newG04Env(t, g04UnknownResult)
		a := env.register(t, "g04unknown")
		object := a.createObject(t, "collaborative", "Unknown object", map[string]any{"body": "b"})
		status, body, _ := a.invokeCapability(t, "content.structure",
			map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
			newIdempotencyKey())
		requireStatus(t, status, http.StatusBadGateway, body)
		code, invocationID := decodeCapabilityError(t, body)
		if code != "capability_unknown" || invocationID == "" {
			t.Fatalf("code=%s invocation=%s body=%s", code, invocationID, body)
		}
		rowStatus, output, errorCode, _ := env.invocationRow(t, invocationID)
		if rowStatus != "unknown" || len(output) != 0 || errorCode == nil || *errorCode != "capability_unknown" {
			t.Fatalf("row = %s output=%s error=%v", rowStatus, output, errorCode)
		}
		getStatus, getBody, _ := a.call(t, http.MethodGet,
			"/api/v1/capability-invocations/"+invocationID, callOptions{})
		requireStatus(t, getStatus, http.StatusOK, getBody)
		view := decodeInvocation(t, getBody)
		if view.Status != "unknown" || string(view.Output) != "null" {
			t.Fatalf("view = %+v output=%s", view, view.Output)
		}
	})

	t.Run("invalid output", func(t *testing.T) {
		env, _ := newG04Env(t, g04InvalidOutput)
		a := env.register(t, "g04invalid")
		object := a.createObject(t, "collaborative", "Invalid object", map[string]any{"body": "b"})
		status, body, _ := a.invokeCapability(t, "content.structure",
			map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
			newIdempotencyKey())
		requireStatus(t, status, http.StatusBadGateway, body)
		code, invocationID := decodeCapabilityError(t, body)
		if code != "capability_failed" {
			t.Fatalf("code = %s", code)
		}
		rowStatus, output, errorCode, _ := env.invocationRow(t, invocationID)
		if rowStatus != "failed" || len(output) != 0 || errorCode == nil || *errorCode != "capability_failed" {
			t.Fatalf("row = %s output=%s error=%v", rowStatus, output, errorCode)
		}
	})

	t.Run("semantic output violation", func(t *testing.T) {
		env, _ := newG04Env(t, g04SemanticInvalid)
		a := env.register(t, "g04semantic")
		object := a.createObject(t, "collaborative", "Semantic object", map[string]any{"body": "b"})
		status, body, _ := a.invokeCapability(t, "content.structure",
			map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
			newIdempotencyKey())
		requireStatus(t, status, http.StatusBadGateway, body)
		code, invocationID := decodeCapabilityError(t, body)
		if code != "capability_failed" {
			t.Fatalf("code = %s", code)
		}
		rowStatus, output, errorCode, _ := env.invocationRow(t, invocationID)
		if rowStatus != "failed" || len(output) != 0 || errorCode == nil || *errorCode != "capability_failed" {
			t.Fatalf("row = %s output=%s error=%v", rowStatus, output, errorCode)
		}
	})

	t.Run("oversized output", func(t *testing.T) {
		env, _ := newG04Env(t, g04Oversized)
		a := env.register(t, "g04oversize")
		object := a.createObject(t, "collaborative", "Oversize object", map[string]any{"body": "b"})
		status, body, _ := a.invokeCapability(t, "content.structure",
			map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
			newIdempotencyKey())
		requireStatus(t, status, http.StatusBadGateway, body)
		code, invocationID := decodeCapabilityError(t, body)
		if code != "capability_failed" {
			t.Fatalf("code = %s", code)
		}
		rowStatus, output, _, _ := env.invocationRow(t, invocationID)
		if rowStatus != "failed" || len(output) != 0 {
			t.Fatalf("row = %s output=%s", rowStatus, output)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		env, _ := newG04Env(t, g04Slow)
		a := env.register(t, "g04timeout")
		object := a.createObject(t, "collaborative", "Timeout object", map[string]any{"body": "b"})
		status, body, _ := a.invokeCapability(t, "content.structure",
			map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
			newIdempotencyKey())
		requireStatus(t, status, http.StatusGatewayTimeout, body)
		code, invocationID := decodeCapabilityError(t, body)
		if code != "capability_timeout" {
			t.Fatalf("code = %s", code)
		}
		rowStatus, output, errorCode, _ := env.invocationRow(t, invocationID)
		if rowStatus != "timeout" || len(output) != 0 || errorCode == nil || *errorCode != "capability_timeout" {
			t.Fatalf("row = %s output=%s error=%v", rowStatus, output, errorCode)
		}
	})
}

func TestG04RateLimitAndConcurrency(t *testing.T) {
	t.Run("hourly limit", func(t *testing.T) {
		provider := newG04Provider(t)
		env := newTestEnvWithCapabilities(t, func(cfg *config.Config) {
			cfg.RateLimitCapPerHour = 3
		}, nil, provider.appOptions())
		a := env.register(t, "g04rate")
		object := a.createObject(t, "collaborative", "Rate object", map[string]any{"body": "b"})
		// Configuration may lower the frozen 60/hour ceiling.
		for index := 0; index < 3; index++ {
			status, body, _ := a.invokeCapability(t, "object.read",
				map[string]any{"object_id": object.ID}, newIdempotencyKey())
			requireStatus(t, status, http.StatusOK, body)
		}
		status, body, _ := a.invokeCapability(t, "object.read",
			map[string]any{"object_id": object.ID}, newIdempotencyKey())
		requireStatus(t, status, http.StatusTooManyRequests, body)
		requireCode(t, body, "rate_limited")
	})

	t.Run("concurrent revocation", func(t *testing.T) {
		env, provider := newG04Env(t, g04Hold)
		a := env.register(t, "g04revoke-a")
		b := env.register(t, "g04revoke-b")
		object := a.createObject(t, "collaborative", "Revocation object", map[string]any{"body": "b"})
		invitation := a.createInvitation(t, object.ID, []string{"read"}, 1)
		acceptStatus, acceptBody := b.acceptInvitation(t, *invitation.Token)
		requireStatus(t, acceptStatus, http.StatusOK, acceptBody)

		type outcome struct {
			status int
			body   []byte
		}
		result := make(chan outcome, 1)
		go func() {
			status, body, _ := b.invokeCapability(t, "content.structure",
				map[string]any{"object_id": object.ID, "text": "in flight", "format": "plain_text"},
				newIdempotencyKey())
			result <- outcome{status: status, body: body}
		}()
		deadline := time.Now().Add(3 * time.Second)
		for provider.requests.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if provider.requests.Load() == 0 {
			t.Fatal("in-flight call never reached the provider")
		}
		removeStatus, removeBody, _ := a.call(t, http.MethodDelete,
			"/api/v1/objects/"+object.ID+"/members/"+b.id.String(), callOptions{})
		requireStatus(t, removeStatus, http.StatusNoContent, removeBody)
		completed := <-result
		requireStatus(t, completed.status, http.StatusOK, completed.body)
		nextStatus, nextBody, _ := b.invokeCapability(t, "object.read",
			map[string]any{"object_id": object.ID}, newIdempotencyKey())
		requireStatus(t, nextStatus, http.StatusNotFound, nextBody)
	})

	t.Run("burst ten", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04burst")
		object := a.createObject(t, "collaborative", "Burst object", map[string]any{"body": "b"})
		for index := 0; index < 10; index++ {
			status, body, _ := a.invokeCapability(t, "object.read",
				map[string]any{"object_id": object.ID}, newIdempotencyKey())
			requireStatus(t, status, http.StatusOK, body)
		}
		status, body, _ := a.invokeCapability(t, "object.read",
			map[string]any{"object_id": object.ID}, newIdempotencyKey())
		requireStatus(t, status, http.StatusTooManyRequests, body)
		requireCode(t, body, "rate_limited")
	})

	t.Run("concurrency", func(t *testing.T) {
		env, provider := newG04Env(t, g04Hold)
		a := env.register(t, "g04concurrent")
		object := a.createObject(t, "collaborative", "Concurrency object", map[string]any{"body": "b"})
		type result struct {
			status int
			body   []byte
		}
		start := make(chan struct{})
		results := make(chan result, 3)
		var wait sync.WaitGroup
		for index := 0; index < 3; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				status, body, _ := a.invokeCapability(t, "content.structure",
					map[string]any{"object_id": object.ID, "text": "text", "format": "plain_text"},
					newIdempotencyKey())
				results <- result{status: status, body: body}
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		successes, limited := 0, 0
		for item := range results {
			switch item.status {
			case http.StatusOK:
				successes++
			case http.StatusTooManyRequests:
				limited++
				requireCode(t, item.body, "rate_limited")
			default:
				t.Fatalf("unexpected status %d body=%s", item.status, item.body)
			}
		}
		if successes != 2 || limited != 1 {
			t.Fatalf("successes=%d limited=%d", successes, limited)
		}
		if provider.requests.Load() != 2 {
			t.Fatalf("provider requests = %d, want 2", provider.requests.Load())
		}
		afterStatus, afterBody, _ := a.invokeCapability(t, "object.read",
			map[string]any{"object_id": object.ID}, newIdempotencyKey())
		requireStatus(t, afterStatus, http.StatusOK, afterBody)
	})
}

func TestG04AIDeriveConfirmationRecordsInvocation(t *testing.T) {
	model := &scriptedProvider{replies: []string{
		deriveReply("capability child one"),
		deriveReply("capability child two"),
		deriveReply("capability child three"),
		deriveReply("capability child four"),
	}}
	g04 := newG04Provider(t)
	env := newTestEnvWithCapabilities(t, nil, model, g04.appOptions())
	a := env.register(t, "g04ai")
	object := a.createObject(t, "collaborative", "AI capability object", map[string]any{
		"body": "body",
	})
	conversation := a.createConversation(t, "AI capability chat", &object.ID)

	send := func(content string) string {
		status, body := a.sendMessage(t, conversation, content)
		requireStatus(t, status, http.StatusOK, body)
		turn := decodeTurn(t, body)
		if len(turn.ProposedActions) != 1 || turn.ProposedActions[0].Kind != "confirmation" {
			t.Fatalf("expected one confirmation: %s", body)
		}
		return turn.ProposedActions[0].ConfirmationID
	}

	confirmationID := send("derive one")
	var invocationID string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT id FROM capability_invocations
 WHERE actor_user_id = $1 AND capability_id = 'object.derive' AND status = 'pending'
 ORDER BY created_at DESC, id DESC LIMIT 1`, a.id.String()).Scan(&invocationID); err != nil {
		t.Fatalf("query pending invocation: %v", err)
	}
	getStatus, getBody, _ := a.call(t, http.MethodGet,
		"/api/v1/capability-invocations/"+invocationID, callOptions{})
	requireStatus(t, getStatus, http.StatusOK, getBody)
	pending := decodeInvocation(t, getBody)
	if pending.Status != "pending" || string(pending.Output) != "null" {
		t.Fatalf("pending invocation = %+v", pending)
	}
	approveStatus, approveBody, _ := a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+confirmationID+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, approveStatus, http.StatusOK, approveBody)
	var child objectResponse
	if err := json.Unmarshal(approveBody, &child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	statusDB, output, errorCode, _ := env.invocationRow(t, invocationID)
	if statusDB != "succeeded" || len(output) == 0 || errorCode != nil {
		t.Fatalf("approved invocation = %s output=%s error=%v", statusDB, output, errorCode)
	}
	var stored struct {
		ChildObjectID string `json:"child_object_id"`
	}
	if err := json.Unmarshal(output, &stored); err != nil || stored.ChildObjectID != child.ID {
		t.Fatalf("stored output = %s child=%s", output, child.ID)
	}
	var resourceType string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT resource_type FROM confirmations WHERE id = $1`, mustUUID(t, confirmationID)).Scan(&resourceType); err != nil {
		t.Fatalf("query confirmation: %v", err)
	}
	if resourceType != "capability_invocation" {
		t.Fatalf("confirmation resource_type = %s", resourceType)
	}

	deniedConfirmation := send("derive two")
	denyStatus, denyBody, _ := a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+deniedConfirmation+"/deny", callOptions{})
	requireStatus(t, denyStatus, http.StatusOK, denyBody)
	var deniedInvocation string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT id FROM capability_invocations
 WHERE actor_user_id = $1 AND capability_id = 'object.derive' AND status = 'denied'
 ORDER BY created_at DESC, id DESC LIMIT 1`, a.id.String()).Scan(&deniedInvocation); err != nil {
		t.Fatalf("query denied invocation: %v", err)
	}
	deniedStatus, deniedOutput, deniedCode, _ := env.invocationRow(t, deniedInvocation)
	if deniedStatus != "denied" || len(deniedOutput) != 0 || deniedCode == nil || *deniedCode != "capability_denied" {
		t.Fatalf("denied invocation = %s output=%s code=%v", deniedStatus, deniedOutput, deniedCode)
	}
	var deniedAudit int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events
 WHERE action = 'capability.denied' AND resource_id = $1`,
		mustUUID(t, deniedInvocation)).Scan(&deniedAudit); err != nil {
		t.Fatalf("count denied audit: %v", err)
	}
	if deniedAudit == 0 {
		t.Fatal("confirmation denial was not audited as capability.denied")
	}

	expiredConfirmation := send("derive three")
	var expiredInvocation string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT id FROM capability_invocations
 WHERE actor_user_id = $1 AND capability_id = 'object.derive' AND status = 'pending'
 ORDER BY created_at DESC, id DESC LIMIT 1`, a.id.String()).Scan(&expiredInvocation); err != nil {
		t.Fatalf("query expiring invocation: %v", err)
	}
	env.exec(t, `UPDATE confirmations
 SET created_at = now() - interval '10 minutes',
     expires_at = now() - interval '5 minutes'
 WHERE id = $1`, mustUUID(t, expiredConfirmation))
	expiredReadStatus, expiredReadBody, _ := a.call(t, http.MethodGet,
		"/api/v1/capability-invocations/"+expiredInvocation, callOptions{})
	requireStatus(t, expiredReadStatus, http.StatusOK, expiredReadBody)
	expiredView := decodeInvocation(t, expiredReadBody)
	if expiredView.Status != "denied" || expiredView.ErrorCode == nil ||
		*expiredView.ErrorCode != "confirmation_expired" {
		t.Fatalf("lazy-expired invocation view = %+v code=%v", expiredView, expiredView.ErrorCode)
	}
	expiredStatus, expiredBody, _ := a.call(t, http.MethodGet,
		"/api/v1/confirmations/"+expiredConfirmation, callOptions{})
	requireStatus(t, expiredStatus, http.StatusGone, expiredBody)
	expiredRowStatus, expiredOutput, expiredCode, _ := env.invocationRow(t, expiredInvocation)
	if expiredRowStatus != "denied" || len(expiredOutput) != 0 ||
		expiredCode == nil || *expiredCode != "confirmation_expired" {
		t.Fatalf("expired invocation = %s output=%s code=%v", expiredRowStatus, expiredOutput, expiredCode)
	}

	revokedConfirmation := send("derive four")
	var revokedInvocation string
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT id FROM capability_invocations
 WHERE actor_user_id = $1 AND capability_id = 'object.derive' AND status = 'pending'
 ORDER BY created_at DESC, id DESC LIMIT 1`, a.id.String()).Scan(&revokedInvocation); err != nil {
		t.Fatalf("query revoked invocation: %v", err)
	}
	delegationStatus, delegationBody, _ := a.call(t, http.MethodGet, "/api/v1/ai/delegations", callOptions{})
	requireStatus(t, delegationStatus, http.StatusOK, delegationBody)
	var delegations struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(delegationBody, &delegations); err != nil || len(delegations.Items) == 0 {
		t.Fatalf("delegations = %s", delegationBody)
	}
	revokeStatus, revokeBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/ai/delegations/"+delegations.Items[0].ID, callOptions{})
	requireStatus(t, revokeStatus, http.StatusNoContent, revokeBody)
	approveRevokedStatus, approveRevokedBody, _ := a.call(t, http.MethodPost,
		"/api/v1/confirmations/"+revokedConfirmation+"/approve", callOptions{
			IdempotencyKey: newIdempotencyKey(),
		})
	requireStatus(t, approveRevokedStatus, http.StatusForbidden, approveRevokedBody)
	revokedRowStatus, revokedOutput, _, _ := env.invocationRow(t, revokedInvocation)
	if revokedRowStatus != "pending" || len(revokedOutput) != 0 {
		t.Fatalf("revoked execution invocation = %s output=%s", revokedRowStatus, revokedOutput)
	}
}

func TestG04ExternalInputBoundaries(t *testing.T) {
	env, provider := newG04Env(t, g04Success)
	a := env.register(t, "g04boundary")
	object := a.createObject(t, "collaborative", "Boundary object", map[string]any{"body": "b"})
	token := a.createToken(t, "boundary", []string{"capabilities:invoke", "objects:read"})
	invokeRaw := func(input []byte, key string) (int, []byte, http.Header) {
		return env.bearerCall(t, token.Plaintext, http.MethodPost,
			"/api/v1/capabilities/content.structure/invoke",
			callOptions{RawBody: input, IdempotencyKey: key})
	}
	// The helper above uses a fixed object id; use the real object id here.
	exactInput := func(target int, base string) []byte {
		raw := exactContentInput(t, target, base)
		return bytes.ReplaceAll(raw, []byte("0192f0c4-5a6b-7c8d-9e0f-123456789abc"), []byte(object.ID))
	}
	cases := []struct {
		name   string
		input  []byte
		status int
	}{
		{"chinese 65535", exactInput(65535, "汉"), http.StatusOK},
		{"chinese 65536", exactInput(65536, "汉"), http.StatusOK},
		{"chinese 65537", exactInput(65537, "汉"), http.StatusRequestEntityTooLarge},
		{"emoji 65536", exactInput(65536, "😀"), http.StatusOK},
		{"newline escape 65536", exactInput(65536, "\n"), http.StatusOK},
		{"control escape 65536", exactInput(65536, "\x01"), http.StatusOK},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			started := time.Now()
			status, body, _ := invokeRaw(item.input, newIdempotencyKey())
			elapsed := time.Since(started)
			requireStatus(t, status, item.status, body)
			if item.status == http.StatusOK {
				if elapsed > 10*time.Second {
					t.Fatalf("input took %s", elapsed)
				}
				invocation := decodeInvocation(t, body)
				if invocation.Status != "succeeded" {
					t.Fatalf("invocation = %+v", invocation)
				}
			} else {
				requireCode(t, body, "payload_too_large")
			}
		})
	}

	// ASCII cannot reach 64 KiB under the 48,000-rune text schema bound.
	asciiMax := []byte(`{"object_id":"` + object.ID + `","text":"` + strings.Repeat("a", 48000) + `","format":"markdown"}`)
	status, body, _ := invokeRaw(asciiMax, newIdempotencyKey())
	requireStatus(t, status, http.StatusOK, body)
	asciiOver := []byte(`{"object_id":"` + object.ID + `","text":"` + strings.Repeat("a", 48001) + `","format":"markdown"}`)
	status, body, _ = invokeRaw(asciiOver, newIdempotencyKey())
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")
	if provider.requests.Load() != 6 {
		t.Fatalf("provider requests = %d", provider.requests.Load())
	}
}

func TestG04ProposalReferencesMustExist(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	a := env.register(t, "g04references")
	collaborative := a.createObject(t, "collaborative", "Reference object", map[string]any{
		"checklist": []map[string]any{{"text": "one", "done": false}},
	})
	randomID := "0192f0c4-5a6b-7c8d-9e0f-123456789abc"
	status, body, _ := a.invokeCapability(t, "object.propose_change",
		map[string]any{
			"object_id": collaborative.ID, "action": "toggle_checklist_item",
			"input":            map[string]any{"item_id": randomID, "done": true},
			"expected_version": 1,
		}, newIdempotencyKey())
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")

	interactive := a.createObject(t, "interactive", "Reference poll", map[string]any{
		"prompt": "pick", "options": []map[string]any{{"label": "A"}, {"label": "B"}},
	})
	status, body, _ = a.invokeCapability(t, "object.propose_change",
		map[string]any{
			"object_id": interactive.ID, "action": "cast_vote",
			"input":            map[string]any{"option_id": randomID},
			"expected_version": 1,
		}, newIdempotencyKey())
	requireStatus(t, status, http.StatusBadRequest, body)
	requireCode(t, body, "invalid_request")

	var proposals int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM object_proposals WHERE actor_user_id = $1`,
		a.id.String()).Scan(&proposals); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if proposals != 0 {
		t.Fatalf("invalid proposals were persisted: %d", proposals)
	}
}

func TestG04ProposalInputBoundaries(t *testing.T) {
	applyProposal := func(t *testing.T, user *testUser, objectID, proposalID string) objectResponse {
		t.Helper()
		status, body, _ := user.call(t, http.MethodPost,
			"/api/v1/objects/"+objectID+"/proposals/"+proposalID+"/apply",
			callOptions{Body: map[string]any{"expected_version": 1}})
		requireStatus(t, status, http.StatusOK, body)
		var applied struct {
			Proposal struct {
				Status string `json:"status"`
			} `json:"proposal"`
			Object objectResponse `json:"object"`
		}
		if err := json.Unmarshal(body, &applied); err != nil {
			t.Fatalf("decode apply: %v", err)
		}
		if applied.Proposal.Status != "applied" {
			t.Fatalf("proposal status = %s", applied.Proposal.Status)
		}
		return applied.Object
	}
	proposeBody := func(t *testing.T, user *testUser, objectID string, body string, key string) (int, []byte, http.Header) {
		t.Helper()
		return user.invokeCapability(t, "object.propose_change", map[string]any{
			"object_id": objectID, "action": "set_body",
			"input": map[string]any{"body": body}, "expected_version": 1,
		}, key)
	}

	t.Run("former 16 KiB boundary is no longer the proposal cap", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04prop-16k")
		object := a.createObject(t, "collaborative", "16 KiB proposal", map[string]any{"body": "b"})
		body := strings.Repeat("a", 16384)
		status, response, _ := proposeBody(t, a, object.ID, body, newIdempotencyKey())
		requireStatus(t, status, http.StatusOK, response)
		var output struct {
			ProposalID string `json:"proposal_id"`
		}
		invocation := decodeInvocation(t, response)
		if err := json.Unmarshal(invocation.Output, &output); err != nil {
			t.Fatalf("decode proposal: %v", err)
		}
		applied := applyProposal(t, a, object.ID, output.ProposalID)
		if applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("applied object version=%d bodyBytes=%d", applied.Version, len([]byte(applied.State["body"].(string))))
		}
	})

	t.Run("20,000 ASCII characters with idempotent creation", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04prop-20k-ascii")
		object := a.createObject(t, "collaborative", "20k ASCII", map[string]any{"body": "b"})
		body := strings.Repeat("b", 20000)
		key := newIdempotencyKey()
		status, response, _ := proposeBody(t, a, object.ID, body, key)
		requireStatus(t, status, http.StatusOK, response)
		status, replay, replayHeaders := proposeBody(t, a, object.ID, body, key)
		requireStatus(t, status, http.StatusOK, replay)
		if replayHeaders.Get("Idempotency-Replayed") != "true" {
			t.Fatal("proposal replay was not marked")
		}
		var output struct {
			ProposalID string `json:"proposal_id"`
		}
		invocation := decodeInvocation(t, response)
		if err := json.Unmarshal(invocation.Output, &output); err != nil {
			t.Fatalf("decode proposal: %v", err)
		}
		replayed := decodeInvocation(t, replay)
		var replayOutput struct {
			ProposalID string `json:"proposal_id"`
		}
		if err := json.Unmarshal(replayed.Output, &replayOutput); err != nil {
			t.Fatalf("decode replay: %v", err)
		}
		if replayOutput.ProposalID != output.ProposalID {
			t.Fatalf("idempotent replay returned a different proposal")
		}
		var proposals int
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM object_proposals WHERE object_id = $1`,
			mustUUID(t, object.ID)).Scan(&proposals); err != nil {
			t.Fatalf("count proposals: %v", err)
		}
		if proposals != 1 {
			t.Fatalf("proposals = %d", proposals)
		}
		status, objectBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
		requireStatus(t, status, http.StatusOK, objectBody)
		var before objectResponse
		if err := json.Unmarshal(objectBody, &before); err != nil {
			t.Fatalf("decode object: %v", err)
		}
		if before.Version != 1 {
			t.Fatalf("proposal modified the object to version %d", before.Version)
		}
		applied := applyProposal(t, a, object.ID, output.ProposalID)
		if applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("apply did not modify the object correctly")
		}
		var createAudit, applyAudit int
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE action = 'object.proposal_create' AND actor_user_id = $1`,
			a.id.String()).Scan(&createAudit); err != nil {
			t.Fatalf("count create audit: %v", err)
		}
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM audit_events WHERE action = 'object.proposal_apply' AND actor_user_id = $1`,
			a.id.String()).Scan(&applyAudit); err != nil {
			t.Fatalf("count apply audit: %v", err)
		}
		if createAudit == 0 || applyAudit == 0 {
			t.Fatalf("proposal audit create=%d apply=%d", createAudit, applyAudit)
		}
	})

	t.Run("20,000 Chinese characters", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04prop-20k-cn")
		object := a.createObject(t, "collaborative", "20k Chinese", map[string]any{"body": "b"})
		body := strings.Repeat("汉", 20000)
		status, response, _ := proposeBody(t, a, object.ID, body, newIdempotencyKey())
		requireStatus(t, status, http.StatusOK, response)
		invocation := decodeInvocation(t, response)
		var output struct {
			ProposalID string `json:"proposal_id"`
		}
		if err := json.Unmarshal(invocation.Output, &output); err != nil {
			t.Fatalf("decode proposal: %v", err)
		}
		// Proposal creation succeeds: 60,000 UTF-8 bytes fit the 64 KiB
		// proposal input cap. Applying duplicates input + state_after in the
		// object.state_changed event. C-G04-04 raised that event's bound to
		// 132,352 bytes, so the legal 20,000-character Chinese body now
		// applies, changes the object, and persists the full event.
		applied := applyProposal(t, a, object.ID, output.ProposalID)
		if applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("Chinese proposal did not apply correctly")
		}
		var (
			payloadBytes int
			stateBytes   int
			action       string
			inputBody    string
		)
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT octet_length(payload::text),
       octet_length((payload->'state_after')::text),
       payload->>'action',
       payload->'input'->>'body'
  FROM object_events
 WHERE object_id = $1 AND event_type = 'object.state_changed' AND object_version = 2`,
			mustUUID(t, object.ID)).Scan(&payloadBytes, &stateBytes, &action, &inputBody); err != nil {
			t.Fatalf("read state_changed event: %v", err)
		}
		if payloadBytes <= 98304 || payloadBytes > 132352 {
			t.Fatalf("payload bytes = %d, want (98304,132352]", payloadBytes)
		}
		if stateBytes > 65536 {
			t.Fatalf("state_after bytes = %d", stateBytes)
		}
		if action != "set_body" || inputBody != body {
			t.Fatalf("event action=%q inputBodyBytes=%d", action, len(inputBody))
		}
		if got := env.count(t, `
SELECT count(*) FROM object_events
 WHERE object_id = $1 AND object_version = 2
   AND event_type IN ('object.state_changed','ai.proposal_applied')`,
			mustUUID(t, object.ID)); got != 2 {
			t.Fatalf("version 2 events = %d, want 2", got)
		}
		if got := env.count(t, `
SELECT count(*) FROM audit_events
 WHERE actor_user_id = $1 AND action IN ('object.proposal_create','object.proposal_apply')`,
			a.id); got != 2 {
			t.Fatalf("proposal audit rows = %d, want 2", got)
		}
	})

	t.Run("20,000 mixed characters", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04prop-20k-mixed")
		object := a.createObject(t, "collaborative", "20k mixed", map[string]any{"body": "b"})
		body := strings.Repeat("a", 10000) + strings.Repeat("汉", 5000) + strings.Repeat("😀", 5000)
		if len([]rune(body)) != 20000 {
			t.Fatalf("rune count = %d", len([]rune(body)))
		}
		status, response, _ := proposeBody(t, a, object.ID, body, newIdempotencyKey())
		requireStatus(t, status, http.StatusOK, response)
		invocation := decodeInvocation(t, response)
		var output struct {
			ProposalID string `json:"proposal_id"`
		}
		if err := json.Unmarshal(invocation.Output, &output); err != nil {
			t.Fatalf("decode proposal: %v", err)
		}
		applied := applyProposal(t, a, object.ID, output.ProposalID)
		if applied.Version != 2 || applied.State["body"] != body {
			t.Fatalf("mixed proposal did not apply correctly")
		}
	})

	t.Run("20,000 emoji exceed the 64 KiB proposal bound", func(t *testing.T) {
		env, _ := newG04Env(t, g04Success)
		a := env.register(t, "g04prop-20k-emoji")
		object := a.createObject(t, "collaborative", "20k emoji", map[string]any{"body": "b"})
		body := strings.Repeat("😀", 20000)
		status, response, _ := proposeBody(t, a, object.ID, body, newIdempotencyKey())
		requireStatus(t, status, http.StatusRequestEntityTooLarge, response)
		requireCode(t, response, "payload_too_large")
		var proposals, events int
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM object_proposals WHERE object_id = $1`,
			mustUUID(t, object.ID)).Scan(&proposals); err != nil {
			t.Fatalf("count proposals: %v", err)
		}
		if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM object_events WHERE object_id = $1 AND event_type = 'ai.proposal_created'`,
			mustUUID(t, object.ID)).Scan(&events); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if proposals != 0 || events != 0 {
			t.Fatalf("oversized proposal had side effects: proposals=%d events=%d", proposals, events)
		}
		status, objectBody, _ := a.call(t, http.MethodGet, "/api/v1/objects/"+object.ID, callOptions{})
		requireStatus(t, status, http.StatusOK, objectBody)
		var current objectResponse
		if err := json.Unmarshal(objectBody, &current); err != nil {
			t.Fatalf("decode object: %v", err)
		}
		if current.Version != 1 {
			t.Fatalf("object version = %d", current.Version)
		}
	})
}

func TestG04IdempotencyReplayAndPermissionLoss(t *testing.T) {
	env, _ := newG04Env(t, g04Success)
	a := env.register(t, "g04idem")
	object := a.createObject(t, "collaborative", "Idempotency object", map[string]any{"body": "b"})
	beforeMissing := env.countInvocations(t, a.id, "object.read")
	missingStatus, missingBody, _ := a.invokeCapability(t, "object.read",
		map[string]any{"object_id": object.ID}, "")
	requireStatus(t, missingStatus, http.StatusBadRequest, missingBody)
	requireCode(t, missingBody, "invalid_request")
	if env.countInvocations(t, a.id, "object.read") != beforeMissing {
		t.Fatal("missing Idempotency-Key wrote an invocation")
	}
	unknownStatus, unknownBody, _ := a.invokeCapability(t, "object.unknown",
		map[string]any{"object_id": object.ID}, "")
	requireStatus(t, unknownStatus, http.StatusNotFound, unknownBody)

	key := newIdempotencyKey()
	input := map[string]any{
		"object_id": object.ID, "action": "set_summary",
		"input": map[string]any{"summary": "concurrent"}, "expected_version": 1,
	}

	type outcome struct {
		status int
		body   []byte
	}
	results := make(chan outcome, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			status, body, _ := a.invokeCapability(t, "object.propose_change", input, key)
			results <- outcome{status: status, body: body}
		}()
	}
	wait.Wait()
	close(results)
	successes, inProgress := 0, 0
	for item := range results {
		switch item.status {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			inProgress++
			requireCode(t, item.body, "request_in_progress")
		default:
			t.Fatalf("unexpected status %d body=%s", item.status, item.body)
		}
	}
	if successes+inProgress != 2 {
		t.Fatalf("successes=%d in-progress=%d", successes, inProgress)
	}
	var proposals int
	if err := env.store.Pool().QueryRow(t.Context(), `
SELECT count(*) FROM object_proposals WHERE object_id = $1 AND actor_user_id = $2`,
		mustUUID(t, object.ID), a.id.String()).Scan(&proposals); err != nil {
		t.Fatalf("count proposals: %v", err)
	}
	if proposals != 1 {
		t.Fatalf("proposals = %d", proposals)
	}
	if env.countInvocations(t, a.id, "object.propose_change") != 1 {
		t.Fatal("concurrent idempotent call created more than one invocation")
	}

	status, body, headers := a.invokeCapability(t, "object.propose_change", input, key)
	requireStatus(t, status, http.StatusOK, body)
	if headers.Get("Idempotency-Replayed") != "true" {
		t.Fatal("completed invocation did not replay")
	}
	conflictStatus, conflictBody, _ := a.invokeCapability(t, "object.propose_change",
		map[string]any{
			"object_id": object.ID, "action": "set_summary",
			"input": map[string]any{"summary": "different"}, "expected_version": 1,
		}, key)
	requireStatus(t, conflictStatus, http.StatusConflict, conflictBody)
	requireCode(t, conflictBody, "idempotency_conflict")

	readKey := newIdempotencyKey()
	readInput := map[string]any{"object_id": object.ID}
	readStatus, readBody, _ := a.invokeCapability(t, "object.read", readInput, readKey)
	requireStatus(t, readStatus, http.StatusOK, readBody)
	crossCapabilityStatus, crossCapabilityBody, _ := a.invokeCapability(t, "object.context", readInput, readKey)
	requireStatus(t, crossCapabilityStatus, http.StatusConflict, crossCapabilityBody)
	requireCode(t, crossCapabilityBody, "idempotency_conflict")
	deleteStatus, deleteBody, _ := a.call(t, http.MethodDelete,
		"/api/v1/objects/"+object.ID, callOptions{})
	requireStatus(t, deleteStatus, http.StatusNoContent, deleteBody)
	lostStatus, lostBody, _ := a.invokeCapability(t, "object.read", readInput, readKey)
	requireStatus(t, lostStatus, http.StatusNotFound, lostBody)
	code, _ := decodeCapabilityError(t, lostBody)
	if code != "not_found" {
		t.Fatalf("post-deletion replay code = %s", code)
	}

	limitsUser := env.register(t, "g04limits")
	object2 := limitsUser.createObject(t, "collaborative", "Limits object", map[string]any{"body": "b"})
	before := env.countInvocations(t, limitsUser.id, "content.structure")
	large := strings.Repeat("x", 70000)
	largeStatus, largeBody, _ := limitsUser.invokeCapability(t, "content.structure",
		map[string]any{"object_id": object2.ID, "text": large, "format": "plain_text"},
		newIdempotencyKey())
	requireStatus(t, largeStatus, http.StatusRequestEntityTooLarge, largeBody)
	if env.countInvocations(t, limitsUser.id, "content.structure") != before {
		t.Fatal("oversized input wrote an invocation")
	}

	unsupportedStatus, unsupportedBody, _ := limitsUser.call(t, http.MethodPost,
		"/api/v1/capabilities/object.read/invoke", callOptions{
			RawBody:        []byte(`{"object_id":"` + object2.ID + `"}`),
			IdempotencyKey: newIdempotencyKey(),
			Headers:        map[string]string{"Content-Type": "text/plain"},
		})
	requireStatus(t, unsupportedStatus, http.StatusUnsupportedMediaType, unsupportedBody)
	requireCode(t, unsupportedBody, "unsupported_media_type")
}
