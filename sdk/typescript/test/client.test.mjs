// SPDX-License-Identifier: Apache-2.0
// Run after `npm run -w sdk/typescript build`.
import assert from "node:assert/strict";
import test from "node:test";

import {
  DeriveWorldApiError,
  DeriveWorldClient,
  newIdempotencyKey
} from "../dist/index.js";

function mockFetch(handler) {
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ url, init });
    return handler(url, init, calls.length);
  };
  fetchImpl.calls = calls;
  return fetchImpl;
}

function jsonResponse(body, status = 200, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers }
  });
}

test("bearer token and request id are sent", async () => {
  const fetchImpl = mockFetch(() => jsonResponse({ version: "0.1.0", commit: "c", build_time: "t", model_mode: "fixture" }));
  const client = new DeriveWorldClient({
    baseUrl: "http://derive.test",
    token: "dw_pat_test",
    fetch: fetchImpl,
    requestId: () => "request-id-0001"
  });
  const meta = await client.meta();
  assert.equal(meta.model_mode, "fixture");
  const call = fetchImpl.calls[0];
  assert.equal(call.url, "http://derive.test/api/v1/meta");
  assert.equal(call.init.headers.get("Authorization"), "Bearer dw_pat_test");
  assert.equal(call.init.headers.get("X-Request-Id"), "request-id-0001");
  assert.equal(call.init.credentials, "same-origin");
});

test("capability invoke sends idempotency key and typed body", async () => {
  const fetchImpl = mockFetch(() =>
    jsonResponse({
      id: "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
      capability_id: "object.read",
      status: "succeeded",
      output: { object: { id: "x", type: "collaborative", version: 1, state: {}, shared_context: {} } },
      duration_ms: 2,
      created_at: "2026-10-23T00:00:00Z",
      completed_at: "2026-10-23T00:00:00.002Z"
    })
  );
  const client = new DeriveWorldClient({
    baseUrl: "http://derive.test",
    token: "dw_pat_test",
    fetch: fetchImpl,
    requestId: () => "request-id-0002"
  });
  const result = await client.invokeCapability(
    "object.read",
    { object_id: "0192f0c4-5a6b-7c8d-9e0f-123456789abc" },
    { idempotencyKey: "idem-key-0000000001" }
  );
  assert.equal(result.status, "succeeded");
  const call = fetchImpl.calls[0];
  assert.equal(call.url, "http://derive.test/api/v1/capabilities/object.read/invoke");
  assert.equal(call.init.method, "POST");
  assert.equal(call.init.headers.get("Idempotency-Key"), "idem-key-0000000001");
  assert.equal(call.init.headers.get("Content-Type"), "application/json");
  assert.deepEqual(JSON.parse(call.init.body), {
    object_id: "0192f0c4-5a6b-7c8d-9e0f-123456789abc"
  });
});

test("pending confirmation is returned without being treated as an error", async () => {
  const fetchImpl = mockFetch(() =>
    jsonResponse(
      {
        id: "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
        capability_id: "object.derive",
        status: "pending",
        confirmation_id: "0192f0c4-5a6b-7c8d-9e0f-123456789abd",
        created_at: "2026-10-23T00:00:00Z"
      },
      202
    )
  );
  const client = new DeriveWorldClient({ baseUrl: "http://derive.test", fetch: fetchImpl });
  const result = await client.invokeCapability(
    "object.derive",
    {
      object_id: "0192f0c4-5a6b-7c8d-9e0f-123456789abc",
      title: "Child",
      inheritance_fields: ["summary"]
    },
    { idempotencyKey: "idem-key-0000000002" }
  );
  assert.equal(result.status, "pending");
  assert.equal(result.confirmation_id, "0192f0c4-5a6b-7c8d-9e0f-123456789abd");
});

test("error envelope becomes DeriveWorldApiError", async () => {
  const fetchImpl = mockFetch(() =>
    jsonResponse(
      {
        error: {
          code: "capability_denied",
          message: "capability is not available to this actor",
          request_id: "request-id-0003",
          details: { invocation_id: "0192f0c4-5a6b-7c8d-9e0f-123456789abc" }
        }
      },
      403
    )
  );
  const client = new DeriveWorldClient({ baseUrl: "http://derive.test", fetch: fetchImpl });
  await assert.rejects(
    () => client.invokeCapability("object.read", { object_id: "x" }, { idempotencyKey: "idem-key-0000000003" }),
    (error) => {
      assert.ok(error instanceof DeriveWorldApiError);
      assert.equal(error.status, 403);
      assert.equal(error.code, "capability_denied");
      assert.equal(error.details.invocation_id, "0192f0c4-5a6b-7c8d-9e0f-123456789abc");
      return true;
    }
  );
});

test("object list encodes query parameters", async () => {
  const fetchImpl = mockFetch(() => jsonResponse({ items: [], next_cursor: null }));
  const client = new DeriveWorldClient({ baseUrl: "http://derive.test/", fetch: fetchImpl });
  await client.listObjects({ scope: "shared", limit: 5, cursor: "abc" });
  assert.equal(
    fetchImpl.calls[0].url,
    "http://derive.test/api/v1/objects?scope=shared&limit=5&cursor=abc"
  );
});

test("idempotency keys match the frozen format", () => {
  for (let index = 0; index < 20; index++) {
    const key = newIdempotencyKey();
    assert.match(key, /^[A-Za-z0-9_-]{16,128}$/);
  }
});

test("no token means no Authorization header", async () => {
  const fetchImpl = mockFetch(() => jsonResponse({ version: "0.1.0", commit: "c", build_time: "t", model_mode: "fixture" }));
  const client = new DeriveWorldClient({ baseUrl: "http://derive.test", fetch: fetchImpl });
  await client.meta();
  assert.equal(fetchImpl.calls[0].init.headers.get("Authorization"), null);
});
