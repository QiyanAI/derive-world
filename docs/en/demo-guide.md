# Two-user demo guide

This is the reproducible Derive Loop that the Reference World acceptance uses. It runs against
the real Compose stack: PostgreSQL, the Go API, the production web bundle, and
two independent browser identities. Fixture mode is deterministic and labelled;
it is not real AI.

## Prerequisites

- Docker Engine with Compose v2.
- Two independent browser profiles, or two private windows. The automated
  suite uses two isolated Playwright browser contexts instead.
- No model API key is required in fixture mode.

## Start the stack

```bash
cp .env.example .env
docker compose up --build -d
curl -fsS http://127.0.0.1:8080/readyz
```

Open <http://127.0.0.1:8080> in both profiles. The header shows `FIXTURE`.

## Scenario A — collaborative object

1. **A** registers at **Create your Digital Self**. The welcome page confirms
   the Digital Self and Personal AI created by the real registration transaction.
2. **A** opens **World → Create your first object**, chooses **Collaborative
   object**, and creates a document.
3. **A** edits the body. The header version increments from v1 to v2.
4. **A** opens **People & access** and creates an invitation with **Read,
   Contribute, Derive**. Copy the link; it is shown once.
5. **B** opens the link in the second profile, chooses **Create an account**,
   registers, returns to the invitation, and accepts it.
6. **B** edits the body from the separate session.
7. **A** clicks **Refresh**. The body change and the new version are visible;
   they came from PostgreSQL, not from a local echo.
8. **B** clicks **Derive a branch**, names the child, and submits. The child is
   an independent object at v1. **B** opens **View lineage** and sees the source
   object.

Expected result: two independent identities, one authoritative object, a
persisted v3 edit by B, and one derived child with a recorded source version.

## Scenario B — interactive object

1. **A** creates an **Interactive object** with three options.
2. **A** invites **B** with read, contribute, and derive.
3. **B** votes for one option; the selected option is marked **You** and the
   tally is 1.
4. **B** votes for another option; the first tally returns to 0 and the second
   becomes 1. One receipt moved.
5. **A** refreshes and sees the same tally, then closes the poll.
6. **B** refreshes; the options are disabled and the poll is closed.
7. **B** derives a child; **A** opens **History & lineage** and sees the vote,
   close, and version groups.

## Scenario C — human and AI relay (fixture)

1. **A** invites **B** with read, contribute, and derive; **B** joins.
2. **B** opens **AI collaboration**. The panel states that the AI acts inside
   B's membership and delegation, and that fixture replies are scripted.
3. **B** clicks **Ask AI to propose a body edit**. One structured proposal
   appears. The assistant text is marked `FIXTURE` and `structured`.
4. **B** clicks **Apply proposal** and confirms. The object advances one version.
5. **B** clicks **Ask AI to propose a derive**. A confirmation card appears,
   tied to this object, with the proposed child title, inherited fields, and
   **Bound to v2**.
6. **B** clicks **Approve and execute** and confirms. The server creates the
   child; the UI shows the created title and a direct link. **B** opens it.
7. **B** opens the child's lineage and sees the source object. **A** cannot read
   B's private child, so A's lineage shows that no branches are visible rather
   than leaking the title.

Expected result: a real Handoff, a real proposal, a real version-bound human
confirmation, a server-side execution, and a derive edge. No model provider was
called.

## Scenario D — security and honest failures

Perform these checks in the two profiles:

| Check | Expected |
|---|---|
| B opens A's private object URL without an invitation | **No access**; no body content |
| A revokes a pending invitation, then B opens the link | **This invitation is not available.** |
| A removes B from the object while B's workspace is open; B refreshes | **Your access to this object has ended.** No further edits |
| B has read+contribute but no derive; B opens the derive URL | **Your role does not include derive.** |
| A edits the same version in two sessions; the second saves | **version_conflict** alert; no success notice; draft retained |
| A pastes more than 8 KiB of Chinese into private context | Byte counter exceeds 8192; save is disabled |
| The API returns 5xx during auth bootstrap | A service error with request ID and **Try again**; not a fake sign-out |

## Automated acceptance

The blocking suite runs Chromium and Firefox with two independent contexts,
against the real Compose stack. It also runs a 390 px mobile project.

```bash
npm ci
npx playwright install --with-deps chromium firefox
npm run test:e2e
```

Locally, raise only the test-stack rate limits if needed:

```text
RATE_LIMIT_AUTH_PER_MIN=1000
RATE_LIMIT_API_PER_MIN=600
```

CI applies those overrides to `.env` inside the workflow; production defaults
are unchanged. The required check is **E2E**.

## Evidence

The screenshots and recordings in this repository were produced by the running
application:

- `docs/assets/g05-*.png` — desktop, mobile, Chinese, object, AI, lineage.
- `docs/assets/g05-demo-a.webm` — A's browser during the two-user run.
- `docs/assets/g05-demo-b.webm` — B's independent browser context.
- `tests/e2e/capture/capture.spec.ts` — the reproducible capture script.

Fixture evidence never claims real model capability. Real AI is VERIFIED_REAL
only for the approved two-call dual Personal AI run; it is not a general
model/provider claim.
