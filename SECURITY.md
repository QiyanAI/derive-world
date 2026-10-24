# Security Policy

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x | Yes, once released |
| < 0.1.0 | No |

This is the `v0.1.0` release candidate. It is not a production deployment and
no production use is supported or claimed. The repository remains private
until the public-release gate.

## Reporting a vulnerability

The designated long-term channel is GitHub Private Vulnerability Reporting.
It is **not enabled yet** for this release candidate; it will be enabled when
the repository enters the public-release gate, subject to the owner's release
authorization and the repository security settings.

Until then, contact the repository owner through GitHub and request a private
channel. Do **not** open a public issue, discussion, or pull request for a
suspected vulnerability. Do not include exploit details in public channels.
Do not send secrets or personal data in the first message.

## What to include

- Affected version or commit SHA.
- A minimal reproduction, if safe to share privately.
- Impact and affected component.
- Whether the issue is already public.

## Our process

- We aim to acknowledge a report within 3 business days.
- We will validate, reproduce, and agree on a disclosure timeline with you.
- We will credit reporters who wish to be credited.
- We do not promise a bounty or payment.

## Scope

In scope: authentication and session handling, authorization and object
membership checks, CSRF and origin checks, invitation/Derive Link handling,
Personal AI delegation and confirmation, Handoff projection, private-context
isolation, PAT scope boundaries, export visibility, capability broker isolation,
SQL injection, path traversal, secret leakage, unsafe default deployment, and
supply-chain issues in this repository.

Out of scope: vulnerabilities in third-party model providers, issues requiring
a compromised host or browser, social engineering, denial of service against
systems you do not own, and reports without a reproducible impact.

## Model data flow

In Real AI mode, when `AI_INCLUDE_PRIVATE_CONTEXT=true` (the default), the
user's private context is included in the prompt sent to the model provider
configured by the deployment. The current object Handoff projection and recent
conversation messages are also sent. The provider's retention and use of that
data are outside this system's control. Set the flag to `false` to omit private
context from prompts. The reference web client discloses this on the Digital
Self and Settings pages.

Fixture mode never sends data to a model provider. Fixture output is
deterministic demo data and must not be represented as real AI.

## Revocation and cannot-recall data

Revocation stops future access immediately. Content already received by a
legitimate recipient, exported, or processed by a third-party model provider
cannot be retroactively deleted. This system does not promise or implement
"revocation equals erasure".

## Token handling

- Invitation tokens and PAT plaintext are returned once. Only SHA-256 digests
  are stored.
- An invitation idempotency replay returns `token:null` with
  `token_redacted:true`; it never returns a usable token.
- Access logs record route templates such as `/api/v1/invitations/{token}` and
  never the token value, Cookie, or Authorization header.

## Safe defaults

- The service binds to `127.0.0.1:8080` by default.
- Compose publishes only loopback and places PostgreSQL and the reference
  capability provider on internal networks.
- No production secrets are stored in the repository.
- Fixture mode is deterministic and must never be presented as real AI.
