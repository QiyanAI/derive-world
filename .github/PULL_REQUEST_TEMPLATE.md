## Summary

Describe the change and why it is needed. Reference the relevant issue.

## Scope

- [ ] This change stays within the OSS MVP scope.
- [ ] No new dependency was added without a contract reason.
- [ ] No secret, token, or private data is included.
- [ ] English and Chinese user-facing docs are in sync.

## Licensing

- [ ] Any new dependency is on the allowed-license list (Apache-2.0, MIT,
      BSD-2-Clause, BSD-3-Clause, ISC, MPL-2.0); no copyleft or unknown license
      was introduced.
- [ ] My contribution is licensed under Apache-2.0.
- [ ] `THIRD_PARTY_NOTICES.md` was updated if the dependency set changed.

## Tests

List the commands you ran and their results:

```text
gofmt -l .
go vet ./...
go test ./...
npm run typecheck
npm run build
```

## Security

- [ ] Authorization, CSRF, token, log-redaction, and payload-limit behaviour was
      considered.
- [ ] No `pull_request_target`, `curl | bash`, unpinned Action, or unpinned
      image was introduced.
- [ ] Fixture output is still labelled and is not presented as real AI.

## DCO

- [ ] Each commit is signed off (`git commit -s`).
- [ ] I will verify the squash commit message at merge time. `main` currently
      accepts squash merges only and generates the message from branch commit
      messages (`squash_merge_commit_message=COMMIT_MESSAGES`); edit the merge
      message if needed so it retains a `Signed-off-by:` trailer matching the
      commit author. The post-merge push-range DCO check is authoritative.
