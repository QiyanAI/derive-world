# Contributing to Derive World

[中文](CONTRIBUTING.zh-CN.md) | English

Thank you for your interest. This repository is the public `v0.2.0` OSS release.
It includes Digital Self sessions, Living Objects, object
authorization, versioned actions, event history, Derive, Lineage, audit,
idempotency, invitations and Derive Links, Handoff, Personal AI conversations,
the Trust Kernel, proposals and human confirmations, personal access tokens,
self-only export, the Capability Broker, all five first-release capabilities,
the Open API, the TypeScript SDK, and the bilingual Reference World.

## Developer Certificate of Origin (DCO)

This project uses the Developer Certificate of Origin 1.1 (DCO), not a CLA.
Every commit must be signed off:

```bash
git commit -s -m "your message"
```

The resulting trailer must be:

```text
Signed-off-by: Your Name <your@email>
```

Contributions are accepted under the Apache License 2.0. By signing off you
certify the DCO at <https://developercertificate.org/>.

### History

This repository starts from a clean, DCO-compliant initial commit. The
pre-public development history is retained in a separate private development
archive and is not part of this repository's Git history. No DCO exception
applies here.

## Workflow

1. Open an issue describing the change, unless it is a trivial fix.
2. Fork the repository and create a topic branch from `main`.
3. Keep changes small and scoped. The `v0.2.0` dependency set is frozen in
   `go.mod`, `go.sum`, and `package-lock.json`; do not add a dependency without
   a contract reason and maintainer review.
4. Run the local checks:

```bash
gofmt -l .
go vet ./...
go test ./...
npm ci
npm run typecheck
npm run build
```

5. Open a pull request. `main` accepts squash merges only and requires linear
   history. Every branch commit must be signed off:

   ```text
   Signed-off-by: Your Name <your@email>
   ```

   GitHub's squash message is generated from the branch commit messages
   (repository setting `squash_merge_commit_message=COMMIT_MESSAGES`) and can
   be edited at merge time. Verify the generated message and edit it if needed
   so the squash commit retains a `Signed-off-by` trailer matching the commit
   author. CI checks every commit in the PR, every commit introduced by a push
   to `main`, and the sign-off/author email match. The push-range check runs
   after the merge and is detective, not preventive: a lost squash trailer
   cannot be cured by a later signed commit. A failure must be disclosed and
   handled by a scoped Owner exception; never hide it or misrepresent it as
   fixed forward.

   This repository uses squash-only merges, linear history, and the DCO
   trailer check described above.

## Release process

- Release tags are immutable. A release requires maintainer authorization.
- The branch ruleset requires pull requests, linear history, squash merges, and
  the required checks. During the single-maintainer period the ruleset requires
  zero approving reviews; if a second maintainer gains write access, raise the
  review requirement to 1.
- Release archives are built by the `Release` workflow from an authorized tag,
  packaged deterministically, and accompanied by `SHA256SUMS` and a build
  manifest. A `workflow_dispatch` dry-run builds from `main` and never creates
  a tag, Release, or attestation. Tag mode requires the exact immutable tag at
  the current `main` tip, green checks on the push run, and a fail-closed
  release-authorization record. The workflow creates a draft only; publication
  is a separate manual action.
- On an authorized tag, the workflow generates and verifies GitHub
  build-provenance attestations for every archive, `BUILD-MANIFEST.json`, and
  `SHA256SUMS`, and writes `ATTESTATION.json`.
- Do not move, delete, or reuse a tag. If a candidate changes, create the next
  version.
- No prebuilt container images are published; Compose builds from source.

## Commit messages

Use a short imperative subject and a body explaining the why. Reference the
relevant issue or gate work package. Do not include secrets in commit messages
or test fixtures.

## Code style

- Go: standard library first; `gofmt`; no web framework; errors are wrapped with
  context; all source files carry `SPDX-License-Identifier: Apache-2.0`.
- TypeScript: `strict`; no `any` unless documented; no external UI, state, i18n,
  or date libraries in the first release.
- CSS: native CSS with design tokens; no Tailwind or CSS-in-JS.
- Documentation: English and Chinese must stay in sync for user-facing docs.

## Security

Do not open public issues for vulnerabilities. Use the private vulnerability
reporting channel described in `SECURITY.md`.

## Licensing

By contributing you agree that your contribution is licensed under
Apache-2.0. Do not contribute code you cannot license under those terms.
