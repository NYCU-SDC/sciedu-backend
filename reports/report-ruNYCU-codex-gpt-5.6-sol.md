## [2026-08-10 11:11] Task Record

### Task Description
- Implement the Course database/DDL phase using migration version 12.
- Add the Course sqlc schema and queries, generate code, add the Store layer and directly relevant integration tests.
- Do not implement or modify Page or Experiment database objects, and do not commit or push.

### Actions Taken
- Added `internal/database/migrations/12_courses.up.sql` and `12_courses.down.sql` for the `course_status` enum, `courses` table, constraints, defaults, and case-insensitive unique course-code index.
- Added `internal/course/schema.sql` and `internal/course/queries.sql` for the repository's merged-schema sqlc workflow.
- Generated `internal/course/db.go`, `internal/course/models.go`, and `internal/course/queries.sql.go` with sqlc v1.30.0.
- Added `internal/course/store.go` with domain-facing records and create/list/count/get/update/status-update methods.
- Added `internal/course/store_integration_test.go` covering lifecycle operations, list/count filters, literal substring search, pagination, defaults, limits, non-empty checks, case-insensitive uniqueness, and not-found behavior.
- Regeneration added the generated `CourseStatus`, `NullCourseStatus`, and `Course` model declarations to the existing `internal/auth/models.go`, `internal/chat/models.go`, `internal/content/models.go`, `internal/question/models.go`, and `internal/user/models.go`, because every package consumes the shared merged schema.
- The generator rewrote existing generated `db.go` and query output files with LF line endings; Git reports some of them as modified on Windows even though `git diff` shows no semantic changes.
- Ran formatting, generation, tests, vet, and whitespace validation. No Page or Experiment files or tables were changed.

### Attempted Methods
- `make gen` was attempted first but failed because `make` is not installed in the Windows environment.
- Running the schema merge script through Git Bash initially selected Windows `find.exe`; setting Git Bash's PATH to `/usr/bin:/bin` made the repository script complete successfully.
- `sqlc` was not installed on PATH, so the exact repository-generated version was run with `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate` after approval. The combined command timed out while `go generate ./...` was still running, but sqlc generation had completed successfully. Generated output was inspected before continuing.
- The first integration-tag test attempt was blocked by sandbox access to the Windows Go build cache. Re-running outside the sandbox completed successfully.

### Issues & Blockers
- `COURSE_INTEGRATION_DATABASE_URL` is not set, so the integration tests compiled and reported PASS but their database cases were skipped. No Docker volume was destroyed or local database state changed.
- Course owns migration version 12, Page owns version 13, and Experiment owns version 14. This change does not create or depend on Page or Experiment tables.
- Student access to published courses assigned through an Experiment remains outside this database-only phase and cannot be implemented until the Experiment data model exists.

### Next Steps
- Run `COURSE_INTEGRATION_DATABASE_URL=<migrated-test-dsn> go test -tags integration -v ./internal/course` against a disposable PostgreSQL database with migrations through version 12 applied.
- In the next phase, add the Course service and handler layers, Summer error mapping, validation, RBAC, and table-driven unit tests without introducing Page or Experiment tables.

## [2026-08-10 12:09] Task Record

### Task Description
- Implement the Course service and HTTP API phase for the five `courses.tsp` operations.
- Add EXPERIMENTER/ADMIN RBAC, Summer error handling, application wiring, and table-driven service/handler tests.
- Do not create or modify Page or Experiment domain objects, and do not commit or push.

### Actions Taken
- Added `internal/course/errors.go` with the Course validation sentinel.
- Added `internal/course/service.go` with repository abstraction, pagination, metadata/status validation, checked offset arithmetic, CRUD orchestration, and Summer database error wrapping.
- Added `internal/course/handler.go` with request/response DTOs and handlers for list, create, get, full metadata update, and status update.
- Registered `GET /api/courses`, `POST /api/courses`, `GET /api/courses/{id}`, `PUT /api/courses/{id}`, and `PUT /api/courses/{id}/status` behind DB-backed EXPERIMENTER/ADMIN authorization.
- Added explicit Summer mappings for malformed JSON/invalid Course input (400) and unique course-code conflicts (409); standard Summer mappings handle authentication, authorization, UUID, not-found, and internal database errors.
- Updated `cmd/backend/main.go` to construct and register the Course Store, Service, and Handler.
- Added `internal/course/service_test.go` and `internal/course/handler_test.go` with table-driven validation, pagination, operation, error, and RBAC coverage.
- Verified that STUDENT receives 403 for Course list/create/get/update/status routes. Conditional Student course access is intentionally not implemented without Experiment assignment tables.
- No Page or Experiment files or database objects were changed.

### Attempted Methods
- The first Course handler test run exposed that Summer v1.0.0-test does not map raw JSON decode errors or `databaseutil.ErrUniqueViolation` by default. Inspected the pinned Summer implementation and added Course-specific mappings instead of accepting 500 responses.
- A PowerShell `gofmt internal/course/*.go` attempt did not expand the wildcard. Re-ran formatting with an explicit PowerShell file list and confirmed `gofmt -l` was clean.
- An additional `go test -race ./internal/course` attempt could not start because the local Go environment has CGO disabled. Required non-race tests passed.
- Sandbox access to the Windows Go build cache was denied on one test attempt; the same test succeeded outside the sandbox.

### Issues & Blockers
- `courses.tsp` permits a STUDENT to get a PUBLISHED course only when it is assigned to the student's current Experiment. Experiment participant/course assignment tables and repository methods do not exist yet, so implementing this check would require guessing or creating out-of-scope Experiment schema. The endpoint currently fails closed for STUDENT with 403.
- No additional Course status-transition rules were defined by `courses.tsp`; the service accepts any of the three declared enum statuses without inventing a transition state machine.
- Race testing remains unavailable until CGO is enabled in the Windows Go environment.

### Next Steps
- After the Experiment domain exists, add a repository authorization query for the caller's current Experiment and its assigned published Courses, then allow only that verified STUDENT read path on `GET /api/courses/{id}`.
- Run Course database integration tests against a disposable migrated PostgreSQL database when `COURSE_INTEGRATION_DATABASE_URL` is available.

## [2026-08-10 19:32] Task Record

### Task Description
- Add a Course-owned, mockable Student authorization path for `GET /api/courses/{id}` without modifying auth infrastructure or inventing Experiment schema or SQL.
- Keep Course management routes restricted to EXPERIMENTER/ADMIN and verify the change without generation, commits, or pushes.

### Actions Taken
- Added `StudentCourseAccessChecker` and actor-aware `ByIDForActor` authorization to `internal/course/service.go`, reusing the existing `auth.RoleQuerier` interface.
- Split Course route registration so only `GET /api/courses/{id}` uses authentication-only middleware; the other four Course routes remain management-only.
- Updated `Handler.Get` to read the authenticated actor through `auth.UserIDFromContext` and delegate role/access decisions to the Course service.
- Added service and handler fakes/tests for Student allow, deny, dependency error, management bypass, mixed roles, role lookup errors, missing actors, unsupported roles, and not-found behavior.
- Updated `cmd/backend/main.go` to inject the existing `authStore` as the role querier and a temporary deterministic development mock Student checker.
- Modified `internal/course/service.go`, `internal/course/handler.go`, `internal/course/errors.go`, `internal/course/service_test.go`, `internal/course/handler_test.go`, `cmd/backend/main.go`, and this report. No auth, migration, schema, query, or generated files were modified by this task.

### Attempted Methods
- The first repeated Course test run was denied access to the default Windows Go build cache. Re-running with `GOCACHE` under the system temporary directory succeeded.
- A combined long-running verification tool connection timed out before returning results, so the required test, vet, and diff checks were rerun separately and completed successfully.
- A service test initially expected Summer's database error wrapper to preserve a general role dependency error in the `errors.Is` chain. The test was corrected to verify an internal error and absence of downstream calls, matching the existing wrapper behavior.

### Issues & Blockers
- The production Student checker is intentionally temporary development mock behavior backed by explicit in-memory Student-to-Course assignments; this is not final spec compliance.
- The real checker must enforce the current ACTIVE/scheduled Experiment, participant assignment, course assignment, and PUBLISHED Course requirements after the Experiment implementation is available.

### Next Steps
- Replace `NewDevelopmentMockStudentCourseAccessChecker` in application wiring with an Experiment-backed implementation.
- Add cross-domain integration tests after the Experiment migration/schema and repository APIs are merged.

## [2026-08-10 21:01] Task Record

### Task Description
- Correct the Course migration assignment from version 13 to version 12 because Page version 13 depends on `courses(id)`.
- Replace the unavailable Student access checker with temporary deterministic development assignments that support real 200 and 403 API testing without Experiment tables or SQL.

### Actions Taken
- Renamed the only uncommitted Course migrations to `internal/database/migrations/12_courses.up.sql` and `12_courses.down.sql`; no duplicate version 13 Course migration remains.
- Updated earlier Course report references to the confirmed Course 12 / Page 13 / Experiment 14 ordering.
- Initially added deterministic PUBLISHED development Course records to migration 12; they were subsequently removed so migration 12 remains schema-only pending a separately numbered seed migration.
- Added `internal/course/student_access_mock.go` with a temporary explicit assignment from the existing seeded Student `00000000-0000-0000-0000-000000000001` to Course `00000000-0000-0000-0000-000000000012` only.
- Replaced application wiring with `NewDevelopmentMockStudentCourseAccessChecker`; management-role behavior and the `StudentCourseAccessChecker` boundary remain unchanged.
- Added checker and Handler tests proving assigned Student/Course access returns 200 and the explicit unassigned Course returns 403. Existing Service tests continue to cover EXPERIMENTER/ADMIN bypass.

### Attempted Methods
- Inspected migrations 9 and 10, auth/session implementation, and development documentation before selecting IDs. The existing deterministic Student ID was reused; no deterministic Course IDs previously existed.
- Confirmed that an in-memory assignment without a matching Course row produces 404 after authorization. The matching rows must be supplied by a separately numbered seed migration after the team confirms its version.

### Issues & Blockers
- The mock assignment is development-only and is not the final Experiment authorization implementation.
- The repository has no development login endpoint for the deterministic mock Student. Yaak must use an authenticated session/token whose subject is `00000000-0000-0000-0000-000000000001` to exercise the documented 200 assignment directly.

### Next Steps
- Replace `NewDevelopmentMockStudentCourseAccessChecker` with an Experiment-backed `StudentCourseAccessChecker` after Experiment merges.
- Add the deterministic development Course records in a separate `*_seed_mock_courses` migration after its version is confirmed, then remove that seed and the mock assignment when real Experiment assignment data is available.
- Add integration tests for current ACTIVE/scheduled Experiment, participant assignment, course assignment, and PUBLISHED status.

## [2026-08-10 21:33] Task Record

### Task Description
- Keep Course migration 12 schema-only and determine the appropriate repository placement for deterministic development Course seed data without choosing a migration version.

### Actions Taken
- Removed both temporary `MOCK-COURSE-*` inserts from `internal/database/migrations/12_courses.up.sql`.
- Preserved `internal/course/student_access_mock.go` and its deterministic Student-to-Course assignment logic.
- Inspected every existing migration and confirmed that `10_seed_mock_user.up/down.sql` is the repository's explicit seed-migration naming pattern.
- Corrected the preceding report entry so it no longer claims migration 12 contains development Course records.

### Attempted Methods
- Compared migration 10 with migration 9: migration 10 is explicitly named and scoped as mock seed data, while migration 9 creates the same user only to support a chat schema backfill and foreign key.

### Issues & Blockers
- No deterministic Course rows currently exist in the database. The development checker still has deterministic assignment IDs, but its allowed API path will return 404 until a separate Course seed migration is added.
- A migration version for the proposed `*_seed_mock_courses.up/down.sql` pair has not been selected and requires team confirmation.

### Next Steps
- After the team assigns a version later than Course migration 12, add an independently reversible `*_seed_mock_courses` migration containing the assigned and unassigned deterministic PUBLISHED Course rows.

## [2026-08-10 21:56] Task Record

### Task Description
- Add a strictly development-only login for the deterministic mock Student so Yaak can obtain valid session cookies through the existing auth session issuance path.
- Preserve all production OAuth, token validation, middleware, authorization, and database behavior.

### Actions Taken
- Added the shared `DevelopmentMockStudentID` constant for the existing migration-10 Student `00000000-0000-0000-0000-000000000001`.
- Added `POST /api/auth/dev-login`, registered only when `CookieConfig.Environment == EnvironmentDev`, with a second environment guard inside the Handler.
- Reused `Service.IssueSession` and the existing `setSessionCookies` helper; the endpoint accepts no body, user ID, or role input.
- Added Handler tests for development route registration and cookie issuance, production route absence, and direct non-development invocation rejection.
- Updated the Course development checker to reference the shared auth Student constant without changing its authorization behavior.

### Attempted Methods
- Inspected and reused the existing `EnvironmentDev` / `EnvironmentProd` constants and the Handler's local `handle` route-registration helper rather than introducing another environment mechanism.

### Issues & Blockers
- A separately numbered mock Course seed migration is still required before the assigned Course path can return 200 instead of 404. Its number remains intentionally unselected pending team confirmation.

### Next Steps
- In Yaak, call `POST /api/auth/dev-login` against a local `ENVIRONMENT=dev` backend and retain the returned `access_token` cookie for Course requests.

## [2026-08-10 22:55] Task Record

### Task Description
- Allow the local development backend to start without Google OAuth credentials while preserving existing production OAuth and authentication behavior.

### Actions Taken
- Extracted Google OAuth bootstrap logic into `initGoogleOAuthProvider` in `cmd/backend/main.go`.
- Made the provider optional only when `Environment == dev` and both the Google client ID and client secret are absent.
- Kept partial development credentials invalid and kept the existing production initialization behavior unchanged.
- Added `cmd/backend/main_test.go` coverage for development without credentials, development with complete credentials, partial development credentials, and production without credentials when a redirect URL is configured.

### Attempted Methods
- Initial tests encountered a Windows shared Go build-cache permission lock. Re-running with a workspace-local temporary `GOCACHE` completed successfully.

### Verification
- `go test ./cmd/backend ./internal/auth`
- `go test ./...`
- `go vet ./...`
- `gofmt -l cmd/backend/main.go cmd/backend/main_test.go`
- `git diff --check`

### Issues & Blockers
- None.

## [2026-08-10 23:24] Task Record

### Task Description
- Add migration version 15 containing only the deterministic temporary development Course seed records used by Yaak authorization testing.

### Actions Taken
- Added `15_seed_mock_courses.up.sql` with the assigned Course `00000000-0000-0000-0000-000000000012` and unassigned Course `00000000-0000-0000-0000-000000000013`.
- Gave both records unique mock-oriented codes, non-empty titles and descriptions, and `PUBLISHED` status.
- Added `15_seed_mock_courses.down.sql`, scoped to deleting only those two exact UUIDs.
- Left Course schema migration 12 and all other migrations unchanged.

### Verification
- `go test ./internal/course`
- `go test ./...`
- `go vet ./...`
- `git diff --check`

### Issues & Blockers
- None.

## [2026-09-12 15:45] Task Record

### Task Description
- Implement the first SCIEDU-121 phase: PageVisit persistence, database invariants, sqlc queries, transaction store scaffolding, and PostgreSQL integration coverage without HTTP/service orchestration.

### Actions Taken
- Added migration 15 and matching `internal/pagevisit/schema.sql` for PageVisit persistence.
- Added student-scoped idempotency uniqueness, one-open-visit-per-student/session uniqueness, timestamp ordering validation, cascade foreign keys, and list/filter indexes.
- Added sqlc queries for student locking, idempotency lookup, ownership lookup, current-open lookup, close/create/leave operations, and filtered list/count operations.
- Added `pagevisit.Store.WithinTx` for later atomic enter orchestration.
- Generated sqlc code with sqlc v1.30.0; generated shared model files now include `PageVisit`.
- Added integration tests for uniqueness/check constraints, reopening after close, Page cascade deletion, transaction rollback, list/count filtering, and newest-first ordering.

### Attempted Methods
- The first schema-generation invocation used `bash`/`sqlc` directly from PowerShell, but neither command was on PATH.
- Calling Git Bash normally selected Windows `find.exe`, causing the merge script to report no schemas. Running Git Bash with its `/usr/bin:/bin` PATH fixed this.
- Used `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate` because no standalone sqlc executable was installed.
- The first Go verification used the default Windows Go cache and failed with sandbox access-denied errors. Re-running with a workspace-local temporary `GOCACHE` succeeded.

### Verification
- `go test ./...` passed.
- `go test -tags integration ./internal/pagevisit -count=1 -v` compiled and passed, but all database tests were skipped because `PAGE_VISIT_INTEGRATION_DATABASE_URL` was not set.
- `go vet ./...` passed.
- `go build ./...` passed.
- `git diff --check` passed.

### Issues & Blockers
- Real PostgreSQL assertions still require migration 15 to be applied to a disposable database and `PAGE_VISIT_INTEGRATION_DATABASE_URL` to be set.
- `internal/pagevisit/queries.sql.go` matches the repository's ignored generated-file pattern and must be force-included when the human stages the new package.
- No ACTIVE Experiment/Page access behavior was selected or implemented in this phase.

### Next Steps
- Implement service orchestration for server timestamps, replay/conflict handling, automatic close plus create in one transaction, and idempotent leave.
- Add handlers/RBAC/list pagination only after persistence integration verification and without duplicating Course/Page authorization SQL.

## [2026-09-12 16:14] Task Record

### Task Description
- Implement SCIEDU-121 Phase 2 core PageVisit enter/leave orchestration without HTTP handlers, RBAC, list parsing, or ACTIVE Experiment access rules.

### Actions Taken
- Added a consumer-owned transaction repository interface and changed `Store.WithinTx` to expose that interface instead of generated `*Queries`.
- Added `Service.Enter` with student-row serialization, pre-mutation idempotency lookup, one server timestamp, automatic close, atomic create, replay, and conflict behavior.
- Added `Service.Leave` with ownership-scoped lookup, server timestamp generation only for OPEN visits, and idempotent preservation of an existing `left_at`.
- Added `ErrNotFound` and `ErrIdempotencyConflict` domain errors for later HTTP mapping.
- Added table-driven fake-transaction tests for enter, replay, conflict, rollback, leave, ownership hiding, and timestamp behavior.
- Added PostgreSQL integration tests for concurrent same-key requests, conflicting replay, same-session enters, enter/leave, and double leave.

### Attempted Methods
- Initial focused compilation found a duplicated helper introduced while patching `service.go`; removed the duplicate and reran the suite successfully.
- Used a workspace-local Go build cache because the sandbox cannot reliably write the default Windows Go cache.

### Verification
- `go test ./internal/pagevisit` passed.
- `go test ./...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `git diff --check` passed.
- `go test -tags integration ./internal/pagevisit -count=1 -v`: all non-DB service tests passed; PostgreSQL tests compiled but skipped because `PAGE_VISIT_INTEGRATION_DATABASE_URL` was not set.
- `staticcheck` and `golangci-lint` were not installed, so they were not run.

### Issues & Blockers
- Real PostgreSQL concurrency verification remains pending a disposable database with migration 15 and `PAGE_VISIT_INTEGRATION_DATABASE_URL`.
- No HTTP routes, RBAC, list request parsing, or Page/Experiment access decision was added; those remain Phase 3 scope.

### Next Steps
- Map domain errors to API responses and implement the merged contract's enter, leave, and management-list handlers in Phase 3.

## [2026-09-12 17:28] Task Record

### Task Description
- Implement SCIEDU-121 Phase 3 HTTP handlers, authentication/RBAC, strict request/query parsing, pagination, API DTOs, error mapping, and production wiring.

### Actions Taken
- Added `POST /api/page-visits`, `POST /api/page-visits/{id}/leave`, and `GET /api/page-visits` routes.
- Added STUDENT role gates for enter/leave and EXPERIMENTER/ADMIN role gates for management listing.
- Added strict create-body decoding, required UUID Idempotency-Key parsing, authenticated Student identity propagation, and read-only response DTO projection.
- Added strict optional filter parsing with `query.Has`, RFC3339 timestamps, OPEN/CLOSED validation, and page/pageSize validation.
- Added PageVisit list service pagination over existing generated list/count queries.
- Added Summer ProblemDetail mapping for validation, not-found, idempotency conflict, and unexpected errors.
- Wired PageVisit Store, Service, and Handler in `cmd/backend/main.go`.
- Added table-driven handler, route authorization, parsing, response-shape, and service-list tests.

### Attempted Methods
- Initial compilation exposed a naming collision between the PageVisit pagination result and sqlc's generated `Page` model. Renamed the pagination result to `VisitPage` without changing behavior.

### Verification
- `go test ./internal/pagevisit` passed.
- `go test ./...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `git diff --check` passed.
- `staticcheck` and `golangci-lint` were not installed.

### Issues & Blockers
- No confirmed Phase 3 implementation blocker.
- ACTIVE Experiment/Page access validation remains intentionally outside scope.
- Handler-level tests use fakes; a final production-route PostgreSQL smoke/integration pass remains appropriate before PR.

### Next Steps
- Perform final diff/spec review, run real HTTP/database route tests if required, and ensure ignored generated `internal/pagevisit/queries.sql.go` is force-included when staging.

## [2026-09-12 21:12] Task Record

### Task Description
- Resolve the final SCIEDU-121 pre-PR issues: map missing or mismatched Course/Page targets to 404 through the production service/repository path, remove an unnecessary large-offset restriction, and record final PostgreSQL verification.

### Actions Taken
- Added `LockPageVisitTarget`, which validates and locks the persisted Page-to-Course relationship without adding Experiment or access-policy behavior.
- Added the target check to the enter transaction after idempotency replay lookup and before the server clock, automatic close, or create operations; missing/mismatched targets return `ErrNotFound`, while query failures remain unexpected errors.
- Removed the `math.MaxInt32` offset restriction while retaining validated `int32` page inputs and overflow-safe `int64` offset arithmetic.
- Regenerated sqlc with v1.30.0.
- Added service tests for missing targets, unexpected target-query errors, and the largest valid page value.
- Added real PostgreSQL service/repository tests for missing Course, missing Page, and a Page belonging to another Course.
- Previously corrected the integration fixture role array to `ARRAY['STUDENT']::user_role[]`.

### Attempted Methods
- The first sqlc invocation could not access `proxy.golang.org` from the sandbox; rerunning the same pinned generator with approved network access succeeded.
- The first focused Go test could not write the default Windows Go build cache; subsequent checks used the workspace-local Go cache.

### Verification
- Migrations through 15 were already applied successfully to the disposable `sciedu_pagevisit_test` database.
- `go test -tags integration ./internal/pagevisit -count=1 -v` passed against that database with 0 failures and 0 skipped PostgreSQL tests.
- PostgreSQL-backed groups: 9 passed, 0 failed, 0 skipped; all 5 concurrency groups executed and passed.
- No deadlock, serialization failure, or unexpected constraint failure was observed.
- `go test ./internal/pagevisit`, `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check` passed.

### Issues & Blockers
- `internal/pagevisit/queries.sql.go` remains ignored by the repository-wide generated-file pattern and must be force-added when staging.
- No ACTIVE Experiment/Page access rule was added because it is not required by the merged PageVisit contract.

### Next Steps
- Stage the ignored generated PageVisit query file explicitly before commit.
