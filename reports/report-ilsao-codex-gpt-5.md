## [2026-07-28 14:28] Task Record

### Task Description
- Review backend PR #58 against the Notion “SciEdu User API & Permission Control” technical spec and API PR #22.
- Verify API and permission behavior, buildability, robustness, and defects.
- Produce the review in `review.md` without modifying implementation files.

### Actions Taken
- Modified `review.md`: recorded six P2 findings, overall assessment, verification evidence, test gaps, and source links.
- Created this mandatory task record at `reports/report-ilsao-codex-gpt-5.md`; no implementation or test files were modified.
- Read the specified Notion page through the connected Notion app.
- Read API PR #22 metadata, patch, and the reviewed `questions.tsp` content through the connected GitHub app.
- Resolved backend PR #58 to head `d9132d794e8e4c08f876bd95cb3de9b50d6d0729` and merge base `1f0b40edd5e9f101f64e35938b4c8ddda12c66bb`.
- Inspected the full 36-file merge diff, relevant tests, generated-code workflow, Summer middleware/error behavior, and call sites.
- Tested a clean `git archive` of the PR head under `/tmp`:
  - `make gen`
  - `go test ./...`
  - `go test -race ./internal/auth ./internal/question ./internal/user ./internal/config`
  - `go test -tags integration ./internal/user -run '^$'`
  - `go vet ./...`
  - `gofmt -l cmd internal`
  - `make build`
  - golangci-lint v2.4.0, matching CI
- Checked `git diff --check` and confirmed the pre-existing dirty sqlc outputs and untracked Go tarball were not treated as PR changes.

### Attempted Methods
- The first clean-archive `go test ./...` before generation failed because query outputs are intentionally ignored and must be produced by `make gen`; rerunning through the documented generation workflow fixed compilation.
- Sandbox test runs initially failed when existing `httptest` cases could not bind a loopback port. The same tests passed outside the network sandbox.
- Local `make lint` could not start because `golangci-lint` was absent from PATH. Downloaded the exact CI version (v2.4.0) to `/tmp`; its first run needed a writable cache and `-buildvcs=false` because the clean archive had no `.git`. With those environment-only adjustments it reported `0 issues`.
- GitHub CLI inspection was unavailable because `gh` is not installed; used the connected GitHub app for PR metadata, patches, comments, review threads, and status instead.

### Issues & Blockers
- Six actionable P2 findings remain in PR #58:
  - Optional Answer fields are serialized as `null` instead of omitted.
  - Disabled callers receive 404 rather than 401 from `/api/users/me`.
  - Search treats `%` and `_` as SQL wildcards rather than literal substring characters.
  - Search length is measured in UTF-8 bytes rather than Unicode characters and explicit empty values are treated as absent.
  - Pagination parsing/offset arithmetic can overflow.
  - Role invariants are enforced only in the HTTP handler, not the service.
- Database integration tests were not executed because `AUTH_INTEGRATION_DATABASE_URL` is unset; no existing Docker volume was destroyed during the review.
- The Notion spec remains Draft and API PR #22 remains open.

### Next Steps
- Fix the six findings in `review.md`, add focused regression tests, regenerate sqlc output, and rerun build, unit/race tests, database integration tests, and the API contract suite.
- Re-review if the Notion document or API PR #22 changes before merge.
