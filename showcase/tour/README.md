# v0.2.0 product tour

`index.html` is the static, offline product tour deployed at
<https://qiyanai.github.io/derive-world/showcase/tour/>.

It uses real `v0.2.0` Fixture-mode screenshots and a real Fixture product
recording (create → approve → enter → change → derive). The
"Describe. Change. Branch." panel is a scripted simulation: it creates no
object and calls no model. Every demo surface is labelled Fixture or
Simulation; this is not a live AI service.

## What it contains

- A visual first screen with the playable, story and working worlds.
- A one-world-many-forms grid: playable, story, workbench, motion.
- A three-step interactive simulation: pick a world, add a surface, branch it.
- The derivation and human-approval story, using the real product UI.
- Links to the repository, quick start, Showcase and Limitations on GitHub.

No backend, analytics, cookies, external scripts or network requests. All
images and the recording are local files under `assets/`.

## View locally

From the repository root:

```bash
python3 -m http.server 8090
```

Open <http://127.0.0.1:8090/showcase/tour/>.

## Validate

From `ops/growth-engine`:

```bash
go run . tour-check
```

The check verifies the SPDX header, the Fixture/“not a live AI service” labels,
offline asset references, local image existence and the responsive rule.

## Deployment

GitHub Pages serves a deployment branch containing only this tour, its assets,
`LICENSE`, `NOTICE`, a root redirect and `.nojekyll`. It does not serve the
repository root. See the owner notes for the exact branch and settings.

## Scope

Single instance. No cross-instance federation, no public object API for
visitors, and no cloud sharing service. See
[Limitations](../../docs/en/limitations.md).
