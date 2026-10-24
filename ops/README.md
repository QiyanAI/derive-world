# AI-OPS

`ops/github-metrics` is an independent Go module with no third-party
dependencies. It collects read-only GitHub metrics, compares snapshots, and
renders Markdown reports. It never changes code, merges pull requests, publishes
releases, or posts content.

## Commands

Run from `ops/github-metrics`:

```bash
go run . snapshot --repo QiyanAI/derive-world --out snapshot.json
go run . diff --before previous.json --after snapshot.json --out diff.json
go run . report --snapshot snapshot.json --before previous.json --out report.md
```

`--repo` falls back to `GITHUB_REPO`. `--token` falls back to `METRICS_TOKEN`
and then `GITHUB_TOKEN`. The token is never printed.

## Permissions

- Public metadata: no token is required, but rate limits are lower.
- Issues and pull requests: `issues:read`, `pull-requests:read`.
- Traffic views/clones: repository push or administration read. Use a
  fine-grained `METRICS_TOKEN` with only `Administration: read`; never use the
  broad development token that can delete repositories or modify workflows.
- When traffic is unavailable, it is `UNKNOWN`. It is never estimated.

## Data rules

- Stars, forks, and downloads are public signals, not active users.
- Maintainer downloads cannot be excluded from public counts.
- Issue/PR text is untrusted data and is never executed as instructions.
- Raw snapshots belong in Actions artifacts; only reviewed weekly reports belong
  in `ops/reports/`.
- No paid promotion or model API spend is part of this workflow.
