## [2026-08-25 03:23] Task Record

### Task Description
- Inspect SCIEDU-113, SCIEDU-114, and SCIEDU-115 branches, summarize their current state, and assess merge difficulty without performing the merge.

### Actions Taken
- Confirmed the three branches belong to `sciedu-backend` and verified their live remote SHAs plus `main` using read-only `git ls-remote`.
- Compared commits, common merge base, file lists, diff sizes, route wiring, schema/migrations, reports, and local-versus-remote SCIEDU-115 state.
- Simulated pairwise merges with `git merge-tree --write-tree` inside temporary shared clones; no branch or source file was changed.
- Inspected the Course schema divergence, generated-model conflicts, application wiring overlap, Experiment/Course integration gaps, and migration ordering.
- Attempted isolated branch build/test verification from temporary archives. The snapshots require ignored sqlc-generated files and the local environment has no `sqlc`, so current verification relies on committed source plus each branch's recorded test evidence.
- Added `.codebase-tutor/map.md` and this report only; product code was not modified.

### Attempted Methods
- A first remote query failed because sandbox DNS was blocked; the approved read-only retry succeeded.
- The first test batch accidentally ran from the current repository instead of each archive because the command did not change directories. Its identical failures were discarded as invalid evidence.
- The corrected archive tests reached branch snapshots but could not compile without ignored generated query files. Focused second commands also lost the temporary `GOCACHE` environment after the first command. These results were not treated as branch regressions.
- Used branch-authored verification reports and source-level inspection to separate known feature status from locally unreproducible generated-code checks.

### Issues & Blockers
- Remote SCIEDU-115 stops after database/schema/query generation; the local `feat/SCIEDU-115-pages` is four commits ahead and contains the Service, Handler, wiring, and tests.
- SCIEDU-114 and SCIEDU-115 independently add different Course schema/migration-12 implementations, creating the main semantic merge decision.
- Shared sqlc `models.go` files conflict across all branches but should be regenerated after merging source schemas rather than manually reconciled.
- SCIEDU-113 does not implement the Experiment-backed Student course-access checker or participant/course assignment APIs, and its `experiment_courses.course_id` currently has no Course foreign key.

### Next Steps
- Confirm whether the merge target should include local SCIEDU-115's four unpublished commits.
- Merge Course 114 and Pages 115 with Course 114 as the canonical Course schema/query implementation, preserve Page 13, then merge Experiment 113.
- Resolve `cmd/backend/main.go` by wiring all three domains, update the Course FK/integration decision, run `make gen`, and execute build/unit/integration tests against a disposable PostgreSQL database.

## [2026-08-25] Task Record — Integration verification planning

### Task Description
- Fully plan how to merge Experiment, Course, and Page into one independent backend PR and define what "verify correctness" must include.
- Read `_mynotes/2608_merge_exp-page-course/task.md`, source PR descriptions, TypeSpec, current tests, runtime setup, and Yaak/OpenAPI tooling.

### Actions Taken
- Read backend PR #60 and #61 bodies and current CI checks through the public GitHub API; both source PR check suites are green, while both PRs remain open.
- Confirmed there is no backend SCIEDU-115 PR and the complete Page implementation exists only in four local commits above the remote branch.
- Confirmed sciedu-api PR #23 was merged and live remote `main` is `4a79c11`; the local remote-tracking `origin/main` is stale.
- Audited all Experiment/Course/Page TypeSpec operations: 28 declared versus 21 implemented by the source branches, with seven Experiment current/assignment operations deferred by source PR scope.
- Audited current handler/service/integration test cases and identified concrete contract/integration gaps: Course empty search, Page required zero/false fields, Page malformed JSON, Student read authorization, display-order upper-bound mismatch, and missing Experiment-Course FK.
- Wrote `_mynotes/2608_merge_exp-page-course/plan.md` and `verification-matrix.md` covering scope decisions, merge order, conflict policy, cross-domain implementation, test layers, PostgreSQL migration checks, Prism contract proxying, Yaak E2E, and PR quality gates.
- Updated `.codebase-tutor/map.md`; no product code, branch, commit, or remote state was changed.

### Attempted Methods
- Attempted to use `gh pr view`, but the configured `slimuCS` GitHub token is invalid. Used unauthenticated public GitHub REST endpoints instead.
- Direct web-page fetching/search could not retrieve the PR pages reliably, so the public REST API and local git refs were used as the primary evidence.
- Local `sciedu-api/origin/main` appeared to omit the new specs; live `git ls-remote` showed that the tracking ref is stale and remote main is the API PR #23 merge commit.

### Issues & Blockers
- Task owner/reviewer must confirm whether the integration PR covers the 21 source-branch operations plus cross-domain internals, or expands to all 28 TypeSpec operations.
- Must confirm whether migration 14 has reached any shared environment before editing it directly; otherwise add a later ALTER migration.
- Must confirm the integration Jira task ID/branch name.
- GitHub CLI must be reauthenticated before creating/managing the eventual PR; repository instructions prohibit the agent from pushing.

### Next Steps
- Discuss and sign off scope decisions D1-D4 in the plan.
- Create the integration branch from updated backend main, merge Course -> full local Page -> Experiment, implement cross-domain access/FK and contract fixes, then execute every quality gate in the plan.

## [2026-08-25 04:42] Task Record — SCIEDU-119 phase and commit planning

### Task Description
- Stop teaching mode and split the Experiment/Course/Page integration into implementation phases with branch-wide commit provenance rules.

### Actions Taken
- Confirmed the persistent commit rule: imported source commits keep their messages; new SCIEDU-119 commits use non-closing `Refs: #60`, `#61`, and/or `#62` only when materially related.
- Created `_mynotes/2608_merge_exp-page-course/phases.md` with Phase 0–7, candidate commit slicing, entry decisions, exit gates, validation ownership, and source PR references.
- Updated `_mynotes/2608_merge_exp-page-course/plan.md` to make `phases.md` the execution source of truth and updated the Page source state in `PR.md`.
- Read `AGENTS.md`, the existing report, `.gitignore`, git status/history, and the local-versus-remote Page diff. No backend product code, branch, commit, or remote state was changed.

### Attempted Methods
- Kept later-phase commit counts provisional because exact boundaries depend on the merged diff; only Phase 0 is ready for immediate entry discussion.
- Chose a dedicated worktree as the recommended isolation strategy after a user-owned uncommitted change appeared in the current Page worktree.

### Issues & Blockers
- Page PR #62's current remote ref is `1c45ad3`, while local `feat/SCIEDU-115-pages` is `6b837d6`, two commits ahead: `71ca895` and `6b837d6`.
- The current Page worktree also contains an uncommitted change in `internal/page/handler.go`; it was not made or modified by Codex and must remain untouched.
- Before Phase 0 can finish, decide whether the Page source is updated PR #62 at `6b837d6` or PR #62 plus two explicitly local follow-up commits.

### Next Steps
- Approve the Phase 0 no-commit slice and dedicated-worktree strategy.
- Human developer either pushes the two Page commits to PR #62 or explicitly approves importing local `6b837d6` as an additional source.
- Then fetch/freeze all backend/API heads, create `feat/SCIEDU-119-integrate-experiment-course-page` in the dedicated worktree, and capture the baseline without entering Phase 1.

## [2026-08-25 04:56] Task Record — SCIEDU-119 Phase 0 complete

### Task Description
- Execute the approved no-commit Phase 0: update/freeze source refs, create an isolated SCIEDU-119 worktree/branch, and establish the baseline without touching product behavior or databases.

### Actions Taken
- Fetched backend and API remotes and froze backend main `ae29eae`, Course #60 `0b21642`, Experiment #61 `78adcbd`, Page #62 `a587eaf`, and API main `4a79c11`.
- Confirmed Page PR #62 now includes the previously local 409 fix, real-DB Page API test, and final DTO conversion commit; remote/local heads match.
- Created dedicated worktree `/Users/melodywu/mlocal/_Github/sdc/sciedu-backend-SCIEDU-119` and branch `feat/SCIEDU-119-integrate-experiment-course-page` from `origin/main`.
- Downloaded official prebuilt sqlc v1.30.0 to `/private/tmp/sciedu-phase0-bin`, ran `make gen`, and confirmed the worktree remained clean.
- Passed `go build ./...`, `go vet ./...`, `go test ./... -count=1`, and `gofmt -l .`; confirmed Docker daemon 29.4.0 is available without removing or migrating a DB.
- Updated `_mynotes/2608_merge_exp-page-course/phases.md`, `plan.md`, and `PR.md` with frozen inputs and Phase 0 evidence. No backend commit or product-code diff was created.

### Attempted Methods
- Initial sandboxed `git fetch` failed DNS resolution; reran with approved network access successfully.
- `go install sqlc@v1.28.0` failed compiling `pg_query_go` because Go 1.26.5/macOS SDK reports a conflicting `strchrnul` declaration. Switched to the official prebuilt binary.
- sqlc v1.28.0 completed schema/sqlc generation but produced 13 version-comment-only diffs because tracked files were generated with v1.30.0. Reran with v1.30.0 and restored a clean worktree without reverting files manually.
- First `make gen` hit sandbox denial on the global Go build cache; reran with `GOCACHE=/private/tmp/sciedu-phase0-gocache`.
- First sandboxed test run failed because existing auth/chat `httptest` servers could not bind loopback ports. The identical test command passed with approved local-network access.

### Issues & Blockers
- Local Go is 1.26.5 while CI declares 1.25.1.
- GitHub workflows install sqlc v1.28.0, but tracked generated files declare v1.30.0; Phase 1 must continue with v1.30.0 to avoid generated noise, and the workflow mismatch should be resolved deliberately rather than inside an unrelated merge commit.
- No Phase 1 blocker remains, but its three merge-commit boundaries and conflict ownership must be confirmed before entry.

### Next Steps
- Discuss Phase 1 merge commits: Course #60, Page #62, then Experiment #61.
- Decide whether toolchain workflow alignment is in SCIEDU-119 scope or a separate follow-up; do not mix it into source merge commits implicitly.

## [2026-08-25] Task Record — SCIEDU-119 Phase 1 complete

### Task Description
- Execute the approved Phase 1 source imports for Course PR #60, Page PR #62, and Experiment PR #61, then align CI's sqlc version in a separate SCIEDU-119 commit.

### Actions Taken
- Merged frozen Course head `0b21642` and committed `aded44c feat: integrate Courses API` with `Refs: #60` and `Task: SCIEDU-119`.
- Merged frozen Page head `a587eaf`, preserved Course #60's canonical migration 12/schema/query files, combined Course and Page wiring, regenerated all shared sqlc models, and committed `7b60878 feat: integrate Pages and PageBlocks APIs` with `Refs: #60, #62`.
- Merged frozen Experiment head `78adcbd`, combined all three route/wiring paths, regenerated shared models, and committed `4dabd71 feat: integrate Experiments API` with `Refs: #61`.
- Changed all nine sqlc pins in the main, pull-request, and stage workflows from v1.28.0 to v1.30.0; committed `f32d8b6 ci: align sqlc generation version` with `Task: SCIEDU-119` and no source PR trailer.
- Kept migration 14's missing Course FK unchanged because database reconciliation belongs to Phase 2.

### Integration Issues Resolved
- Page's schema causes sqlc to generate a `Page` database model in every package. This collided with Course and Experiment service pagination DTOs also named `Page`; renamed them to `CoursePage` and `ExperimentPage` in the relevant merge commits.
- Resolved shared generated-model conflicts exclusively through sqlc v1.30.0 regeneration from the accepted combined schema.
- Preserved imported source reports and commit history; no source commit message was rewritten.

### Verification
- Before each domain merge commit: `make gen`, `git diff --check`, `gofmt -l .`, `go build ./...`, and `go test ./... -count=1` passed.
- Phase exit: `go vet ./...` and `go test ./... -race -count=1` passed for all packages.
- A second final `make gen` produced no tracked diff; the integration worktree is clean at `f32d8b6`.

### Attempted Methods
- The first long-running race-test tool call completed without returning captured output. Repeated the same uncached race command and received an explicit exit code 0 plus passing output for every package.

### Issues & Blockers
- No Phase 1 blocker remains.
- Phase 2 still needs the migration 14 Course FK amendment and disposable PostgreSQL migration/FK verification; Phase 3 still owns replacing Course's development Student access mock and splitting Page read/write authorization.

### Next Steps
- Stop before Phase 2 and agree on its exact commit slicing, disposable database strategy, and migration evidence.

## [2026-08-25] Task Record — SCIEDU-119 Phase 2 complete

### Task Description
- Reconcile the Course–Experiment database relationship in one atomic commit and prove migration/FK correctness against an isolated disposable PostgreSQL database.

### Actions Taken
- Started `sciedu-119-phase2-postgres` from the existing PostgreSQL 18.1 image on localhost port 55432 with no Docker volume; verified the name and port were unused first.
- Added an integration-tagged migration test using the explicitly destructive `MIGRATION_INTEGRATION_DATABASE_URL` contract.
- Captured the intended pre-fix failure: inserting an `experiment_courses` row with a nonexistent Course returned no error.
- Added `REFERENCES courses (id) ON DELETE CASCADE` to migration 14 and the matching Experiment sqlc schema.
- Tested the complete migration lifecycle `empty → 15 → empty → 15`, migration version 15 with `dirty=false`, PostgreSQL `23503` orphan rejection, and Course-delete cascade.
- Updated the Course status-filter integration case to include its per-test search prefix after a fully migrated database exposed the two migration-15 PUBLISHED mock Courses.
- Committed `802187a fix: enforce experiment course relationships` with `Refs: #60, #61` and `Task: SCIEDU-119`.
- Stopped the dedicated `--rm` test container; it was automatically removed and no persistent volume was created or deleted.

### Verification
- Passed migration integration tests against PostgreSQL 18.1, including the race variant.
- Passed Course and Page real-PostgreSQL integration suites sequentially, including the race variants.
- Passed `make gen`, `git diff --check`, `gofmt -l .`, `go build ./...`, `go vet ./...`, integration-tag vet, `go test ./... -count=1`, and `go test ./... -race -count=1`.
- `golangci-lint run ./...` and integration-tag lint reported zero issues. The normal lint run could not write its default cache inside the sandbox but still exited zero; the integration lint used `/private/tmp` and was clean.
- Post-commit sqlc v1.30.0 generation produced no tracked diff. Only the pre-existing untracked `.codebase-tutor/`, `CLAUDE.md`, and this report remain outside the commit.

### Attempted Methods
- The first test command did not start because zsh expanded the unquoted `?sslmode=disable`; reran with the DSN quoted.
- The first compilation of the new integration test found an unused `context` import; removed it before capturing the behavioral pre-fix failure.
- Course integration initially failed because its status-only assertion also saw migration 15's two PUBLISHED seed Courses. Scoped the case with its unique search prefix and reran Course/Page successfully.
- One full race run transiently compiled `internal/experiment/queries.sql.go` without seeing its generated `db.go`/`models.go`, despite those files being present and earlier gates passing. The Experiment race package immediately passed alone, and the repeated full uncached race suite passed every package. Final generation remained deterministic.

### Issues & Blockers
- No Phase 2 blocker remains.
- Migration 15's mock Course seeds intentionally remain while Phase 3 still uses the Course development access checker.
- Phase 3 must decide the shared actor/access policy and the 403/404 behavior before replacing the mock and opening the three Student Page read routes.

### Next Steps
- Stop before Phase 3 and discuss its two behavior commits, shared access seam, authorization truth table, and database-backed verification ownership.

## [2026-08-25] Task Record — Commit trailer policy update

### Task Description
- Remove `Task: SCIEDU-119` from the current integration commit history and stop adding that trailer to future commits.

### Actions Taken
- Audited the first-parent integration history and found the four Phase 1 commits had already been rewritten without `Task:` trailers: `79b3eab`, `ee31ccf`, `14f15db`, and `6b21bf5`.
- Amended only the Phase 2 commit, changing `802187a` to `b6c4996` and retaining `Refs: #60, #61`; its tree is unchanged.
- Updated SCIEDU-119 notes, ADR, PR draft, phase evidence, and verification matrix with the current SHAs and the rule that future commits omit Jira trailers.

### Issues & Blockers
- This is a local history rewrite. If an older branch tip was pushed elsewhere, the human developer must account for the changed SHAs when publishing; Codex did not push.

### Next Steps
- Discuss Phase 3 without starting implementation.

## [2026-08-25] Task Record — SCIEDU-119 Phase 3 complete

### Task Description
- Replace development Course access with Experiment-backed Student authorization, authorize the three Student Page reads without opening writes, remove migration 15 seeds, and verify the combined behavior.

### Decisions Implemented
- Existing-but-unauthorized access returns 403; directly addressed missing Pages return 404. Both responses are declared by the pinned TypeSpec.
- Overlapping Experiments use any-match semantics: one ACTIVE, in-schedule participant/Course match is sufficient.
- Page passes actor IDs explicitly and delegates the owning-Course decision to `course.Service.ByIDForActor`; it does not duplicate authorization SQL or read hidden service context.
- Migration 15 was development-only and was deleted with the mock. Migration 14 is now latest.

### Actions Taken
- Added the Experiment Store's real Student Course-access query and production Course wiring; deleted the deterministic access mock and migration 15 Course seeds.
- Added database truth-table coverage for participant, status, schedule, Course assignment, Course publication, and overlapping Experiments.
- Split Page route middleware so three authenticated GET routes reach data-level authorization while all eight management writes remain EXPERIMENTER/ADMIN.
- Added explicit actor-aware Page service methods and wired Page to the shared Course service.
- Added unit/handler coverage for actor propagation, authorization ordering, missing actor 401, Student read routing, and write denial.
- Added real PostgreSQL Page API coverage proving assigned Student reads return 200, unassigned reads return 403, and Student writes return 403 without mutating Page data.
- Committed `458478f feat: enforce experiment-backed course access` with `Refs: #60, #61` and `feat: authorize Student page reads` with `Refs: #61, #62`. Neither commit contains a `Task:` trailer.

### Verification
- Passed migration race integration `empty → 14 → empty → 14` on isolated PostgreSQL 18.1, with clean version state, FK rejection, and cascade behavior.
- Passed Experiment, Course, and Page PostgreSQL integration suites sequentially with `-race`.
- Passed full build, full unit suite, full race suite, normal and integration-tag vet, and normal and integration-tag lint with zero issues.
- Ran sqlc v1.30.0 generation after the commits and confirmed no tracked diff.
- Stopped and auto-removed the no-volume `sciedu-119-phase3-postgres` container.

### Attempted Methods
- Sandboxed Page integration and full unit tests could not use localhost/httptest sockets; repeated the identical commands with approved local network access and they passed.
- The first normal lint run exposed one obsolete test helper and an unwritable default cache. Removed the unused helper, moved the lint cache to `/private/tmp`, and reran with zero issues.

### Issues & Blockers
- No Phase 3 blocker remains.
- Phase 4 contract gaps remain intentionally untouched: Course empty search, required Page/Block zero-value fields, malformed JSON consistency, and the display-order upper-bound contract decision.

### Next Steps
- Stop before Phase 4 and discuss its exact commit slicing and display-order strategy.

## [2026-08-25] Task Record — SCIEDU-119 Phase 4 entry decisions

### Task Description
- Agree on the Phase 4 contract-gap boundaries, commit slicing, and Page display-order strategy before implementation.

### Decisions
- Phase 4 will use three behavior commits. Each commit includes its own failing-before/fixed-after tests and uses only the materially related source PR references. No commit will contain a `Task: SCIEDU-119` trailer.
- Commit 1: `fix: reject empty Course search queries` with `Refs: #60`.
  - An omitted `search` remains valid.
  - A present empty value (`?search=`) returns 400, matching TypeSpec `@minLength(1)`.
  - Whitespace-only search remains valid because the pinned TypeSpec has no non-blank pattern; the backend must not add a stricter trim-based rule implicitly.
- Commit 2: `fix: validate Page request payloads` with `Refs: #62`.
  - Page/Block request DTOs use pointer fields where TypeSpec requires zero-capable values, so omission is distinguishable from explicit `displayOrder: 0` and `required: false`.
  - Missing required values and malformed/empty/wrong-type JSON return 400 Problem Details without reaching services.
  - Unknown JSON fields remain accepted; strict unknown-field rejection is outside this phase because the pinned contract does not require it.
- Commit 3: `fix: support full Page display order range` with `Refs: #62`.
  - Backend will support the full TypeSpec range of non-negative `int32` values rather than adding the implementation-derived `99999` maximum to TypeSpec.
  - Migration 13 and the Page sqlc schema may be amended because all databases remain disposable local/snapshot environments.
  - Page and PageBlock order uniqueness becomes `DEFERRABLE INITIALLY IMMEDIATE`: ordinary create/update conflicts are still checked immediately. Reorder transactions run `SET CONSTRAINTS ALL DEFERRED`; these are currently the schema's only two deferrable constraints, and both are order uniqueness. Reorder then updates directly to `0..N-1`.
  - Remove the `+100000` temporary offset and the handler/service `99999` guards, preserving atomic reorder and rollback without integer overflow.

### Verification Agreement
- Run focused Course/Page handler and service tests per commit.
- At phase exit, run migration `empty → 14 → empty → 14`, real PostgreSQL Page reorder/rollback/full-int32 cases, domain integration race tests, full unit/race/build/vet/lint, integration-tag vet/lint, and deterministic sqlc v1.30.0 generation.

### Status
- Decisions approved; Phase 4 implementation started with the Course search contract fix.

### Commit 1 implementation evidence
- Added handler cases proving the pre-fix behavior was wrong: `?search=` returned 200 and a whitespace-only value was discarded instead of being preserved.
- Changed Course query parsing to distinguish an omitted parameter from a present empty value and to validate the untrimmed Unicode length exactly as declared by TypeSpec.
- Focused Course tests, Course vet, and full repository build passed after the fix.

## [2026-08-25] Task Record — SCIEDU-119 Phase 4 complete

### Task Description
- Close the agreed Course and Page contract gaps in three independently reviewable behavior commits and verify the final schema and runtime behavior.

### Actions Taken
- Committed `3fa7a30 fix: reject empty Course search queries` with `Refs: #60`.
  - A present empty search now returns 400; omission remains valid; whitespace is preserved according to the pinned TypeSpec.
- Committed `6212613 fix: validate Page request payloads` with `Refs: #62`.
  - Required zero-capable Page/Block fields use pointers at the HTTP boundary, distinguishing omission from explicit `0` and `false`.
  - Page JSON syntax/type/empty-body errors now return 400 Problem Details across all six body routes without reaching services.
- Implemented `fix: support full Page display order range` with `Refs: #62`.
  - Removed the handler/service `99999` upper bound and accepted the full non-negative PostgreSQL/TypeSpec `int32` range.
  - Amended migration 13 and the Page schema with named `DEFERRABLE INITIALLY IMMEDIATE` order uniqueness constraints.
  - Replaced the overflow-prone `+100000` offset with transaction-scoped constraint deferral and direct final ordering.
  - Mapped raw PostgreSQL/context errors that can first surface at deferred-constraint commit, preserving reorder's declared 409 behavior for a concurrent `23505` while leaving validation errors unchanged.
  - Added real-DB API coverage that creates and reorders both Pages and PageBlocks from `2147483647`, plus retained rollback and immediate duplicate-conflict coverage.

### Failing-Before Evidence
- Course tests showed `?search=` incorrectly returned 200 and whitespace-only search was discarded.
- Page handler tests showed omitted required zero-value fields reached services, while malformed/empty/wrong-type JSON returned 500.
- Page handler/service tests rejected `2147483647`; the old database reorder strategy would overflow when adding 100000.

### Verification
- Focused Course and Page tests passed after their respective fixes; full unit suites passed before every behavior commit.
- Full migration race integration passed `empty → 14 → empty → 14` on PostgreSQL 18.1 with migration 13's deferrable constraints.
- Experiment, Course, and Page real-PostgreSQL integration suites passed sequentially with `-race`.
- Page integration verified max-int32 Page/PageBlock creation and reorder, transaction rollback after an injected write failure, and ordinary duplicate display-order 409 behavior.
- Unit coverage verifies a deferred commit-time `23505` maps to the shared unique-violation sentinel and validation errors pass through unchanged.
- Full repository build, normal and integration-tag vet, normal and integration-tag lint, full unit tests, and full race tests passed.
- Post-commit sqlc v1.30.0 generation was deterministic and left no tracked diff.
- The no-volume `sciedu-119-phase4-postgres` container was stopped and auto-removed after verification.

### Attempted Methods
- sqlc's catalog parser rejected named `SET CONSTRAINTS <constraint>` statements even though PostgreSQL accepts them. Used one generated `DeferOrderConstraints` query with `SET CONSTRAINTS ALL DEFERRED`; the full schema confirms the two Page order constraints are the only deferrable constraints.
- Sandboxed full tests and PostgreSQL suites could not use loopback sockets; repeated the same commands with approved local access.

### Issues & Blockers
- No Phase 4 blocker remains.
- Strict unknown-field rejection remains intentionally out of scope because the pinned TypeSpec does not require it.
- Phase 5 should reassess remaining automated coverage rather than duplicate the migration, access, reorder, and contract tests already added in Phases 2–4.

### Next Steps
- Stop before Phase 5 and discuss whether any meaningful automated coverage gaps remain.

## [2026-08-25] Task Record — SCIEDU-119 Phase 5 entry decisions

### Task Description
- Add only the remaining high-value automated verification for the 21 in-scope Experiment, Course, and Page endpoints.

### Decisions
- Phase 5 uses three domain-attributable test commits and does not repeat the migration, Student-access truth table, Page reorder, or request-contract coverage already established in Phases 2–4.
- Commit 1: `test: verify Experiment API against PostgreSQL` with `Refs: #61`.
  - Exercise all five in-scope Experiment endpoints through HTTP, the real service, and PostgreSQL.
  - Verify configuration JSON round-trip, detail counts, metadata/status updates, list filtering/pagination/counts, and missing-resource behavior.
- Commit 2: `test: verify Course API against PostgreSQL` with `Refs: #60, #61`.
  - Exercise all five Course endpoints through HTTP, the real service, and PostgreSQL.
  - Verify list filters/pagination, duplicate-code conflict, missing-resource behavior, and direct Student Course reads backed by Experiment assignments.
- Commit 3: `test: complete Page API lifecycle coverage` with `Refs: #62`.
  - Add real-PostgreSQL success coverage for Update Page, Update PageBlock, and Delete PageBlock, which are the remaining Page endpoints without successful full-stack database evidence.
- Do not optimize for an aggregate coverage percentage. The Phase 5 invariant is that each of the 21 endpoints has successful HTTP-to-database evidence where applicable, while focused negative and cross-domain cases remain covered.
- Keep real session-cookie/login, OpenAPI/Prism, and Yaak verification in Phase 6. Phase 5 integration handlers may inject an actor while retaining real domain wiring.
- If a new test exposes a product defect, preserve the failing evidence and separate the behavior fix from the `test:` commit. Stop for discussion first if the correction requires a new contract or authorization decision.

### Verification Agreement
- Run focused integration tests, including race variants, for each domain commit.
- At phase exit, run the migration lifecycle and all three domain PostgreSQL suites against one disposable no-volume PostgreSQL instance.
- Run deterministic sqlc v1.30.0 generation, full build, unit/race tests, normal and integration-tag vet/lint, formatting/diff checks, and a tracked-tree cleanliness check.

### Status
- Decisions approved; Experiment PostgreSQL API verification started.

### Commit 1 implementation evidence
- Added an integration-tagged Experiment API suite that wires the real handler, service, Store, and PostgreSQL while injecting an EXPERIMENTER actor at the authentication boundary.
- The suite exercises create, detail, metadata/configuration update, status update, and list endpoints; verifies JSONB configuration round-trip, participant/Course counts, status preservation, status/schedule/search filters, pagination totals, and GET/PUT/status 404 behavior.
- Focused normal tests, integration tests, integration race tests, and integration-tag vet passed. No product defect was exposed.
- A combined formatting command accidentally passed the Markdown report to `gofmt`, which rejected the `#` character without modifying the file. The corrected Go-only formatting and `git diff --check` checks passed.
