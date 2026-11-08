# Offline product tour

`index.html` is a self-contained, five-step tour of Derive World. It uses real
product screenshots from `docs/assets/` and one clearly labelled simulation of
the derivation rule. It is not a hosted product and not a live AI service.

## What it is

- Static HTML, CSS and JavaScript with no dependencies, no build step, no
  analytics, and no network requests.
- Responsive from 360 px to desktop widths.
- Honest labels: every screenshot is marked Fixture, and the derivation panel
  is marked Simulation. Fixture output is deterministic demo data, not real AI.
- No personal data, tokens, or private objects are included.

## View it locally

From the repository root:

```bash
python3 -m http.server 8090
```

Then open <http://127.0.0.1:8090/showcase/tour/>. Opening `index.html`
directly from disk also works in current browsers.

## Validate it

From `ops/growth-engine`:

```bash
go run . tour-check
```

The check verifies the SPDX header, the Fixture label, offline-only assets,
resolvable image paths and a responsive rule.

## Deployment

The page is a deployable artifact, not a deployed service. A maintainer may
serve the repository root with any static host, or publish it through GitHub
Pages after the owner authorizes that setting. This repository does not
purchase a domain, a server, or paid hosting, and no public tour URL is claimed
until one is actually deployed and verified.

## Scope

Single instance. There is no cross-instance federation, no public object API
for visitors, and no cloud sharing service. See
[Limitations](../../docs/en/limitations.md).
