# GROW-04 — Developer and creator outreach

Two audiences, two honest tracks. Neither track is a bulk campaign, and no
external account is used under this authorization.

## Track A — developers

**What they need to know**

- The object, not the chat, is the product: versions, members, permissions,
  lineage (`README.md`).
- A Personal AI proposes; a human approves (`README.md`, `internal/trust`).
- Derivation is an independent child from an immutable snapshot
  (`SHOWCASE.md`, `internal/derive`).
- The permission model returns 404 for callers without read, and a PAT scope
  never substitutes for membership (`SHOWCASE.md`, `internal/identity`).
- Five frozen capabilities through one broker (`capabilities.yaml`).
- Self-host with Docker Compose; Apache-2.0 (`README.md`, `LICENSE`).

**Best formats**: a technical introduction, a reproducible tutorial, an
implementation note that cites repository paths, a real answer in an existing
thread.

**What not to say**: production-ready, certified, enterprise-grade, “real AI”
without the Fixture distinction, or any user/star count.

## Track B — creators

**What they need to know**

- Describe a world in your own words; the Personal AI writes a brief and the
  server composes a runnable world (`docs/en/limitations.md`).
- Any form is valid: playable, readable, operable, visual, 3D or a custom
  surface. There is no fixed category to match.
- Derived worlds are independent and keep their ancestry.
- Sharing is voluntary and scoped to one instance.
- Fixture mode is deterministic demo data, not real AI.

**Best formats**: a short showcase with a public link, a derivation story, the
Derive Relay challenge, a screenshot clearly labelled Fixture.

## Channel strategy

| Channel | Audience | Authorization | First action |
|---|---|---|---|
| GitHub (release notes, issue forms) | developers and existing users | maintainer-controlled | Ship accurate release notes and the relay material |
| Hacker News | engineers who reward honesty | owner account required | Prepare a technical Show HN; post only after owner review |
| Reddit | subreddit-specific | owner account required, per-subreddit rules | Answer existing threads first; no cross-posting |
| Product Hunt | product-curious users | owner account required | One launch, only for a testable release |
| Technical blog / dev.to | implementation-minded developers | owner account required | Publish a path-cited article |
| Ecosystem lists | adjacent maintainers | owner requires consent | Ask first; never open an unrelated PR |

Preflight:

```bash
go run . channels check --channel github --kind technical-intro
```

A non-zero result means the material may exist but the run must not post.

## Prepared material (not published)

| File | Audience | Channel | State |
|---|---|---|---|
| `drafts/technical-intro-github.md` | developers | GitHub | Prepared |
| `drafts/technical-intro-hacker-news.md` | engineers | Hacker News | Prepared; owner account required |
| `drafts/tutorial-github.md` | developers | GitHub | Prepared |
| `drafts/release-notes-github.md` | existing users | GitHub | Prepared |
| `drafts/showcase-github.md` | creators | GitHub | Prepared |
| `drafts/community-reddit.md` | community | Reddit | Prepared; per-subreddit rules required |
| `drafts/community-github.md` | builders | GitHub | Prepared |
| `drafts/relay-github.md` | creators | GitHub | Prepared |
| `drafts/product-update-github.md` | both | GitHub | Prepared |

Every draft is verified by `verify-content` and starts with a “not published”
notice. No external post was made under this authorization; there is no
publication record to claim.

## Publication checklist (human)

1. Confirm the channel's current rules and authorization.
2. Re-run `facts verify`; fix any changed source before using a draft.
3. Re-run `verify-content` on the exact final text.
4. Remove the `[[fact:ID]]` markers only after a human has checked each claim;
   keep the “Facts used” appendix in the review copy.
5. Confirm the Fixture/Real AI label.
6. Post from an authorized account.
7. Record date, material and observable outcome in `state/channels.json`.
   If no outcome is observable, leave it empty.

## Measurement

- Stars, forks and downloads are public signals, not users.
- Traffic requires administration read and is `UNKNOWN` until then.
- An external contribution is meaningful when it carries information, not when
  it is a one-line drive-by.
- No channel is scaled before it has a recorded observation.

## Not done, deliberately

- No account registration, no paid placement, no paid promotion.
- No mass messages, no comments on unrelated projects, no vote requests.
- No fabricated launch, user quote, testimonial or usage number.
