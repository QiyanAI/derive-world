// SPDX-License-Identifier: Apache-2.0
// Build the SDK first: npm run -w sdk/typescript build
//
// Required: DW_PAT (a personal access token with capabilities:invoke plus the
// object scopes used below). Optional: DW_BASE_URL and DW_OBJECT_ID.
//
// This example makes real HTTP requests. It never mocks a response.

import { DeriveWorldClient, DeriveWorldApiError, newIdempotencyKey } from "../../sdk/typescript/dist/index.js";

const baseUrl = process.env.DW_BASE_URL ?? "http://127.0.0.1:8080";
const token = process.env.DW_PAT;

if (!token) {
  console.error(
    [
      "DW_PAT is required.",
      "",
      "Create a personal access token in the Derive World web UI",
      "(Settings -> Personal access tokens) with these scopes:",
      "  capabilities:invoke, objects:read, objects:write, derive:create",
      "",
      "Then run:",
      "  DW_PAT=dw_pat_... node examples/sdk-node/example.mjs"
    ].join("\n")
  );
  process.exit(2);
}

const client = new DeriveWorldClient({ baseUrl, token });

function print(title, value) {
  console.log(`\n=== ${title} ===`);
  console.log(JSON.stringify(value, null, 2));
}

try {
  print("meta", await client.meta());

  console.log("\n=== health ===");
  console.log((await client.health()).trim());

  const capabilities = await client.listCapabilities();
  print(
    "capabilities",
    capabilities.items.map((item) => ({
      capability_id: item.capability_id,
      provider_id: item.provider_id,
      contract_version: item.contract_version,
      execution_constraints: item.execution_constraints
    }))
  );

  let objectId = process.env.DW_OBJECT_ID;
  if (!objectId) {
    const objects = await client.listObjects({ limit: 1 });
    objectId = objects.items[0]?.id;
  }
  if (!objectId) {
    console.error(
      "No object is available. Create a Living Object in the web UI or set DW_OBJECT_ID."
    );
    process.exit(1);
  }
  console.log(`\nUsing object ${objectId}`);

  const read = await client.invokeCapability(
    "object.read",
    { object_id: objectId },
    { idempotencyKey: newIdempotencyKey() }
  );
  print("object.read", read);

  const context = await client.invokeCapability(
    "object.context",
    { object_id: objectId },
    { idempotencyKey: newIdempotencyKey() }
  );
  print("object.context", context);

  const object = await client.getObject(objectId);
  const proposal = await client.invokeCapability(
    "object.propose_change",
    {
      object_id: objectId,
      action: "set_summary",
      input: { summary: "Created by the G04 SDK example" },
      expected_version: object.version
    },
    { idempotencyKey: newIdempotencyKey() }
  );
  print("object.propose_change", proposal);

  const derived = await client.invokeCapability(
    "object.derive",
    {
      object_id: objectId,
      title: `SDK child of ${String(object.state.title ?? "object")}`,
      inheritance_fields: object.object_type === "interactive" ? ["prompt", "options"] : ["summary"]
    },
    { idempotencyKey: newIdempotencyKey() }
  );
  print("object.derive", derived);

  const structured = await client.invokeCapability(
    "content.structure",
    {
      object_id: objectId,
      text: "# SDK example\n\nStructured by the independent provider.",
      format: "markdown"
    },
    { idempotencyKey: newIdempotencyKey() }
  );
  print("content.structure", structured);

  if ("id" in structured && structured.id) {
    const invocation = await client.getCapabilityInvocation(structured.id);
    print("capability invocation", invocation);
  }
} catch (error) {
  if (error instanceof DeriveWorldApiError) {
    console.error(
      `request failed: status=${error.status} code=${error.code} request_id=${error.requestId}`
    );
    console.error(JSON.stringify(error.details, null, 2));
  } else {
    console.error("request failed:", error instanceof Error ? error.message : error);
  }
  process.exitCode = 1;
}
