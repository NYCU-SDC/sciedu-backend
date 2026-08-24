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
