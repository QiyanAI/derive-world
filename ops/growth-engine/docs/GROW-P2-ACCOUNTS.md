# GROW Phase II — Channel Account Manager

The account manager prepares a truthful public presence and records
authorization. It never registers an account for the owner and never stores a
credential.

## State

`state/accounts.json` holds one record per channel:

- `status`: `owner-action-required`, `prepared`, `active`, `suspended`,
  `retired`.
- `handle`, `display_name`, `bio`: public identity once the owner confirms it.
- `authorization.scope`: `read`, `publish` or `all`; with `granted_by`,
  `granted_at` and optional `expires_at`.
- `security`: recorded booleans for two-factor, recovery email and offline
  recovery codes, plus a non-secret description of the token scope.
- `api`: availability, kind and the environment variable that holds the
  credential.
- `owner_steps`: actions only the owner can complete.

No record has a password field. The loader rejects unknown statuses and
duplicate channels.

## Platform registry

`state/platforms.json` records, for GitHub, DEV Community, Hacker News, Reddit
and Product Hunt:

- registration requirements;
- automation policy (`api_with_approval`, `manual_only`, `prohibited`);
- API kind and whether a posting API exists;
- content rules and security requirements;
- owner-only steps;
- official source URLs and the date they were retrieved.

The registry is curated and dated. An AI research run can propose changes in a
review file, but a human decides what enters the registry. The engine never
rewrites platform rules from model output.

## Commands

```bash
go run . accounts list
go run . accounts show --channel github
go run . accounts prepare --channel github           # accounts/kits/github.md
go run . accounts check --channel github            # recorded gaps
go run . accounts preflight --channel github --scope publish
go run . accounts record --channel github --status active --scope publish \
  --authorized-by owner --authorized-at 2026-11-08 --two-factor --recovery-email \
  --token-scope "fine-grained, contents read" --date 2026-11-08 --write
go run . accounts owner-todo --out docs/GROW-P2-OWNER-TODO.md
```

- `prepare` writes an account kit: name candidates, short and long bios, links,
  platform policy, registration checklist, security checklist, owner-only steps
  and the exact facts used. Every kit says it is prepared, not registered.
- `check` reports missing recorded facts; it does not attempt a login.
- `preflight` is the publish gate: active status, requested scope, grantor and
  date, unexpired authorization, two-factor and recovery email.
- `record` updates non-secret facts and appends one JSON line to
  `var/growth-engine/audit.jsonl`.
- `owner-todo` aggregates the minimum owner actions across all channels.

## Owner-only steps

The following are never automated:

- account registration and email/phone verification;
- CAPTCHA, identity checks and platform onboarding;
- enabling two-factor authentication and storing recovery codes;
- OAuth consent screens;
- the first public post on any platform;
- platform-specific human verification or rate-limit challenges.

Passwords, one-time codes, recovery codes, session cookies and API keys must
stay in the owner's password manager or secret store. The engine never asks for
them.

## Security checks

`preflight` requires recorded two-factor authentication and a recovery email,
and refuses an expired authorization. The account kit also lists platform
security requirements. A recorded boolean is an owner assertion, not a
technical verification of the platform account.

## Automation status by platform

| Platform | Policy | Adapter |
|---|---|---|
| GitHub | `api_with_approval` | Discussion creation implemented; requires approval artifact |
| DEV Community | `api_with_approval` | Article creation implemented; requires approval artifact |
| Hacker News | `manual_only` | No publisher; prepare material |
| Reddit | `manual_only` | No publisher; per-subreddit rules |
| Product Hunt | `manual_only` | No publisher; manual launch |

## Honest status

- Account kits and owner todo: **actually generated**.
- Account state, preflight and audit: **implemented and tested**.
- Registration assistance: **not implemented**. No browser automation, no
  credential entry, no CAPTCHA or 2FA handling.
- No official account is claimed. The owner todo lists what is missing.
