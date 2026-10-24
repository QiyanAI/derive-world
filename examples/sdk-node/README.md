# Node SDK example

This example has no npm dependencies and uses Node's built-in `fetch`. It
makes real requests against a running Derive World instance — it never mocks a
response.

## Prerequisites

1. Start Derive World (see [Quick Start](../../docs/en/quickstart.md)).
2. Register in the web UI and create at least one Living Object.
3. Create a personal access token in **Settings → Personal access tokens** with
   the scopes `capabilities:invoke`, `objects:read`, `objects:write`, and
   `derive:create`.

Build the SDK first from the repository root:

```bash
npm run -w sdk/typescript build
```

Then run the example:

```bash
DW_PAT=dw_pat_... node examples/sdk-node/example.mjs
```

Useful environment variables:

| Variable | Required | Meaning |
|---|---|---|
| `DW_PAT` | yes | Personal access token (plaintext is shown once). |
| `DW_BASE_URL` | no | Defaults to `http://127.0.0.1:8080`. |
| `DW_OBJECT_ID` | no | Object to use; otherwise the first visible object is selected. |

The example calls all five capabilities through the Broker, then reads one
invocation back by id. A `DW_PAT` without `capabilities:invoke` is rejected with
`403 forbidden`; a PAT without the operation scope for a specific capability is
also rejected. Object membership is still required; the token scope is a
ceiling, not an authorization.

## Independent provider

To deploy the reference `content.structure` Provider yourself, see
[Provider deployment](../../docs/en/providers.md). The Provider runs as a
separate process on an internal network and only accepts HMAC-signed requests
from the Broker.
