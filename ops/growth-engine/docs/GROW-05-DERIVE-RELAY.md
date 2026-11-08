# GROW-05 — Derive Relay

**One World. Infinite Possibilities.**

Derive Relay is a voluntary derivation challenge built on capabilities that
already exist in `v0.2.0`: free-world creation, human-approved modification,
independent derivation and lineage. It introduces no federation, no new
service and no new data collection.

## What is already real

- `examples/worlds/relay-base.json` is a complete `open_world` create body.
  `internal/objects/worlds_test.go` validates it with the same `ValidateCreate`
  path the server uses, so it cannot silently rot.
- `examples/worlds/create-world.sh` registers a local account, creates the
  world and can publish it with derivation allowed (`cc_by_4.0`).
- Publication and public derivation are implemented in
  `internal/publication` and `/api/v1/public/worlds/*`.
- A derived child is independent and keeps a recorded ancestry; lineage is
  permission-clipped.

## The base world

**The Lantern Coast.** A coast where every light lifted from the water changes
what the tide remembers.

- Four kinds: Traveler, Lantern light, Tide pool, Old beacon.
- Six things, seven links, three one-pass rules.
- Lifting any light adds memory to the tide pool and lights the old beacon.
- Views: scene, stage, cards, timeline.
- No uploaded media, no external URL, no executable code.

## How to participate

1. Start a local instance or use an instance you trust. Fixture mode needs no
   model account.
2. Create the base world:

   ```bash
   BASE_URL=http://127.0.0.1:8080 bash examples/worlds/create-world.sh
   ```

3. Ask the in-world Personal AI to change or derive the world in your own
   words. Apply the proposal; the change is inert until a human approves it.
4. Choose one direction and keep it honest:
   - a game with a goal and a real outcome;
   - an interactive story with chapters and choices;
   - a workbench with editable rows and live summaries;
   - a visual or three-dimensional space;
   - a custom surface with bounded interactions.
5. Derive rather than overwrite when you want to keep the original intact.
6. Publish the derived world with derivation allowed if you want others to
   continue from it.
7. Submit through the
   [showcase form](https://github.com/QiyanAI/derive-world/issues/new?template=showcase.yml).
   State the version or commit and label Fixture or Real AI.

## Starter prompts

- “Turn this coast into a game I can walk around, where lifting all three
  lights calms the tide.”
- “Turn it into an interactive story told by the tide pool, with two choices
  at every chapter.”
- “Make it a workbench where a keeper records which lights were lifted and
  what the coast remembered.”
- “Turn the same content into a three-dimensional scene I can move through.”
- “Keep the premise but change what each light remembers. Derive it instead
  of editing the original.”

Results depend on the mode and the model. In Fixture mode the output is
deterministic demo data, not real AI. Do not claim that a fixture result proves
model capability.

## Rules

- Participation is voluntary. There is no prize, no star requirement, no
  purchase and no ranking that rewards volume.
- Publish only worlds you intend to make public. Remove private context,
  conversations, Handoff projections, tokens, personal data and third-party
  material you cannot share.
- Do not submit another person's private world. Do not auto-collect worlds.
- Label Fixture or Real AI accurately.
- One honest submission is worth more than many duplicates. Do not submit the
  same world repeatedly.

## Scope

Publication and derivation are scoped to one instance. There is no
cross-instance federation, no public import and no cloud content network. The
challenge does not create a new social network.

## Success signal

The only measured success signal is a voluntary public showcase submission
that a reviewer can reproduce. Absence of submissions is reported as absence,
never filled with seeded or fabricated work.
