<!-- Prepared draft — not published. Review the facts, channel rules and authorization before posting. -->

# Run Derive World locally and derive your first world

This is the supported local path from the repository README. It runs in Fixture mode, which calls no model; all AI output is deterministic demo data. [[fact:fixture-not-real-ai]]

```bash
git clone https://github.com/QiyanAI/derive-world.git
cd derive-world
cp .env.example .env
docker compose up --build -d
```

Open <http://127.0.0.1:8080>, register a local account, and create a free world. The top bar shows `FIXTURE` while the deterministic adapter is active.

Then change the world in your own words. The Personal AI proposes; you approve. Nothing changes until the human confirmation. [[fact:human-approval]]

The software is Apache-2.0. [[fact:open-source]] If you configure a real endpoint, you pay your provider directly; never commit the key.

## Facts used

- `human-approval` — A Personal AI can propose changes and derive children, but only a human can approve; a derived world is an independent child from an immutable snapshot.
- `open-source` — The project is licensed under Apache-2.0.
- `fixture-not-real-ai` — Fixture mode is a deterministic demo: it calls no model and is not real AI.

## Channel

- Target: GitHub repository, releases and issue forms (`github`), authorization `maintainer-controlled`.
- Audience: Developers evaluating a self-hostable, open-source world runtime.
- This draft has not been posted anywhere.
