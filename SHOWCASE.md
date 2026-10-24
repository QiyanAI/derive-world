# Showcase — build something that derives

Derive World is a foundation for **Living Objects**: shared, versioned objects
that people and their Personal AI can read, contribute to, derive from, and
trace through an explicit lineage. This page is the launch showcase and a
voluntary, GitHub-native place to share what you build. There is no separate
community platform, and the project never collects your objects, conversations,
Handoff projections, or AI context automatically.

## The 17-second tour

![17-second Derive World demo](docs/assets/demo-preview.gif)

The [full recording](docs/assets/demo-17s.webm) is a 17-second tour of the
reference world: a Living Object, its version history and lineage, and an
interactive poll. It runs in **Fixture mode** — deterministic demo
data, no model call, no API key.

## Three ways to see what is different

### 1. A Living Object keeps its history and can derive

![Lineage map](docs/assets/g05-lineage.png)

![Version history](docs/assets/g05-history.png)

A Living Object is a versioned collaborative document or an interactive poll.
Each accepted action creates a version. **Derive** creates an independent child
from an immutable snapshot of the source: later edits to the parent do not
propagate, and the child does not inherit members, votes, or private data.
Lineage is permission-clipped — objects you cannot read are never named.

**Try it:** create a collaborative object, edit it twice, derive one child from
v1 and another from v2, then open **History & lineage** and compare the
ancestry. See the [Reference World guide](docs/en/reference-world.md).

### 2. Human + Personal AI + Human

![Personal AI](docs/assets/g05-personal-ai.png)

![AI proposal](docs/assets/g05-ai-proposal.png)

![Human confirmation](docs/assets/g05-ai-confirmation.png)

Two independent users, each with a Personal AI, share an object. A Handoff is a
read-only projection: it never carries private context, conversations, tokens,
or another member's delegation. The receiving Personal AI can propose a change
or a derive. The proposal is inert until a human applies it, and a derive
requires a separate five-minute, single-use confirmation bound to an object
version. **The AI never authorizes itself.**

**Try it:** use two browser profiles, invite the second person, open **AI
collaboration**, apply a proposal, then approve a derive and inspect the new
child. See the [two-user demo guide](docs/en/demo-guide.md).

**Accurate label:** these screenshots and the demo are **Fixture mode** —
deterministic demo output, not real model intelligence. The only Real AI
acceptance evidence in `v0.1.0` is the approved two-call API-level DeepSeek
`deepseek-flash` run. It was not a browser demo, was not
re-run on the release SHA, and is not a general provider claim.

### 3. A secure, reproducible OSS foundation

- **Open API:** OpenAPI 3.1 is served at `/api/v1/openapi.yaml`; the
  [API guide](docs/en/api.md) documents the public contract.
- **TypeScript SDK:** a typed, zero-runtime-dependency client for
  PAT-authorized capability list/detail/invoke and invocation status.
- **Five frozen capabilities** through one broker: `object.read`,
  `object.context`, `object.propose_change`, `object.derive`, and
  `content.structure`.
- **Security negatives you can run:** a caller without `read` receives 404, a
  revoked delegation stops the next call, a PAT scope never substitutes for
  object membership, a tampered or unsigned provider response is an unknown
  result, and model calls stop at the configured budgets or kill switch.
- **Reproduce the checks:**

  ```bash
  gofmt -l .
  go vet ./...
  go test ./...
  npm ci
  npm run typecheck
  npm run build
  docker compose up --build -d
  npm run test:e2e
  ```

- **Verify the release:** `SHA256SUMS` covers every asset, and the release
  workflow generates GitHub OIDC/Sigstore build-provenance attestations. Use
  `gh attestation verify` as shown in the
  [release notes](docs/en/release-notes.md#verification).

## Make your own (five steps)

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
```

1. Open <http://127.0.0.1:8080> and create a local account.
2. Create a collaborative object or an interactive poll.
3. Invite a second person from **People & access**.
4. Open **AI collaboration**; apply a scripted proposal, then approve a derive.
5. Open **History & lineage** and inspect the child's ancestry.

Fixture mode needs no model account. If you want a real model, set
`MODEL_MODE=real` and your own `MODEL_BASE_URL`, `MODEL_NAME`, and
`MODEL_API_KEY`; your provider bills you directly. Never commit your key.

## Share what you built

Use the **[Showcase submission](https://github.com/QiyanAI/derive-world/issues/new?template=showcase.yml)**
issue form. Tell us:

- what Living Object you made and why;
- what derivation or lineage you created;
- how a human and a Personal AI participated;
- a public, reproducible link and the exact version or commit;
- whether it is **Fixture** or **Real AI** (your own endpoint/key);
- what problem it solved, or what integration or example you built.

We link to public, reproducible work. **You do not have to star, share, or
submit anything to use the software.** The project is Apache-2.0.

### Before you submit

- Publish only what you intend to be public.
- Remove private context, conversations, Handoff projections, tokens, personal
  data, and third-party material you cannot share.
- Verify the link works without authentication.
- State the Fixture/Real AI label accurately.
- By opening a public showcase issue, you consent to the maintainers linking to
  that issue from the project. Your content remains yours; the repository's
  Apache-2.0 license covers the software, not your submission.

The project will **not** automatically collect or publish private objects,
conversations, Handoff projections, or AI context. `v0.1.0` adds no public
object API, cross-deployment import, or cloud sharing service for the showcase.

## Honest labels

| Label | Meaning |
|---|---|
| **Fixture** | Deterministic demo data. No model is called. Not real AI. |
| **Real AI** | The user configures an OpenAI-compatible endpoint and key, and pays the provider. `v0.1.0` acceptance evidence is the bounded two-call API-level run described above. |
| **Not production** | `v0.1.0` is an OSS MVP. No production readiness, security certification, user adoption, stars, downloads, or funding is claimed. |

See [Limitations](docs/en/limitations.md) for the complete list.
