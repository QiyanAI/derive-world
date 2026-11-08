<!-- Prepared draft — not published. Review the facts, channel rules and authorization before posting. -->

# Derive World release update

Derive World is an open-source foundation for Living Objects. [[fact:release]] [[fact:open-source]]

## What changed

- Free-intent world creation: describe a world in your own words; the Personal AI writes a compact seed and the server composes a runnable world. [[fact:what-it-is]]
- Worlds can be changed, derived and traced with a human approval gate. [[fact:human-approval]] [[fact:lineage]]
- Fixture mode remains a deterministic demo and is not real AI. [[fact:fixture-not-real-ai]]

## How to inspect it

Use the repository release tag and its notes. Verify assets with the published checksums and provenance attestation shown in the release notes.

## Facts used

- `what-it-is` — Derive World is an open-source foundation for Living Objects: shared, versioned objects that people and their Personal AI can read, contribute to, derive from, and trace through an explicit lineage.
- `human-approval` — A Personal AI can propose changes and derive children, but only a human can approve; a derived world is an independent child from an immutable snapshot.
- `lineage` — Derive creates an independent child from an immutable snapshot of the source; later edits to the parent do not propagate and the ancestry is recorded.
- `open-source` — The project is licensed under Apache-2.0.
- `fixture-not-real-ai` — Fixture mode is a deterministic demo: it calls no model and is not real AI.
- `release` — The current public release is v0.2.0.

## Channel

- Target: GitHub repository, releases and issue forms (`github`), authorization `maintainer-controlled`.
- Audience: Developers evaluating a self-hostable, open-source world runtime.
- This draft has not been posted anywhere.
