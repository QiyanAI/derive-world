# GROW-01 / GROW-02 — Presentation and low-barrier entry

## GROW-01 — Repository presentation

The README now leads with the product, not the infrastructure:

- Headline: **Create a world. Change it together. Derive something new.**
- Three differences, in the first screen:
  1. a human and a Personal AI create together, and the human approves;
  2. a world can keep being changed, played, read or operated under one model;
  3. derivation keeps independent children and traceable provenance.
- A direct path to the offline tour for visitors without Docker.
- A link to the Derive Relay base world.
- The existing honest labels are preserved: Fixture is not real AI, real-model
  evidence is scoped, this is not a production platform.

No capability is described that does not exist in the tagged tree. The existing
`docs/assets/` screenshots remain in use and are labelled by the page that
shows them.

## GROW-02 — Low-barrier entry

`showcase/tour/` is a static, offline tour:

- No build step, no dependencies, no analytics, no network requests.
- Five steps: create, change, derive, approve, share.
- Real product screenshots; the derivation panel is explicitly a **simulation**
  and says that it creates nothing.
- The page states that it is **not a live AI service** and that Fixture output
  is deterministic demo data.
- Responsive down to 360 px; keyboard-focusable controls; reduced-motion
  respected.
- All referenced images resolve inside the repository, so the page cannot
  silently depend on a third-party host.

Validation is part of the test suite:

```bash
cd ops/growth-engine
go run . tour-check
```

`tour-check` verifies the SPDX header, viewport meta, language, required honest
labels, absence of analytics/iframes, allowlisted links, resolvable image paths
and a responsive CSS rule.

### Deployment status

The tour is a **deployable artifact, not a deployed service**. No domain,
server or paid host was purchased. No public URL is claimed. A maintainer can
serve the repository root with any static host, or enable GitHub Pages after an
explicit owner decision. See `showcase/tour/README.md`.
