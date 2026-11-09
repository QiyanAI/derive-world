# Reproducible example worlds

`relay-base.json` is the base world for the **Derive Relay** challenge. It is a
complete `POST /api/v1/objects` body for an `open_world`, not a screenshot and
not a fixture shortcut. The `internal/objects` test suite validates it with the
same `ValidateCreate` path the server uses, so a change that makes it invalid
fails CI.

## Create it on a local instance

Start the reference stack in Fixture mode (see the [README](../../README.md)),
then run:

```bash
BASE_URL=http://127.0.0.1:8080 bash examples/worlds/create-world.sh
```

The script registers a local test account, creates the world from the JSON and
prints the new object id. Registration and creation are idempotent only within
one account; rerunning creates a new local account and a new world, which is
intended for a local demo.

To also publish the world for anonymous reading and derivation:

```bash
PUBLISH=1 bash examples/worlds/create-world.sh
```

Publishing uses `cc_by_4.0` and allows derivation. Only publish content you
intend to make public.

## Derive Relay prompts

Start from the world you created (or any public world whose owner allows
derivation) and ask the in-world Personal AI in your own words. These are
starters, not scripts that promise a fixed result:

1. “Turn this coast into a game I can walk around, where lifting all three
   lights calms the tide.”
2. “Turn it into an interactive story told by the tide pool, with two choices
   at every chapter.”
3. “Make it a workbench where a keeper records which lights were lifted and
   what the coast remembered.”
4. “Turn the same content into a three-dimensional scene I can move through.”
5. “Keep the premise but change what each light remembers. Derive it instead
   of editing the original.”

Every AI proposal is inert until a human applies it. A derive creates an
independent child and keeps the recorded ancestry. If you use Fixture mode,
label your work Fixture — it is deterministic demo data, not real AI.

## Share

Submit through the [showcase issue form](https://github.com/QiyanAI/derive-world/issues/new?template=showcase.yml).
Read the safety checklist in [SHOWCASE.md](../../SHOWCASE.md) first. Never
include private context, conversations, tokens, or another person's data.

## Scope

Publication and derivation are scoped to one instance. There is no
cross-instance federation or public import.
