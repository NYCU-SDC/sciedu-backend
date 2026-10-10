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

## [2026-08-24 22:50] Task Record

### Task Description
- Start Phase 1 of the correct-answer API: persistence, CHOICE/TEXT representation, versioning, idempotent management GET/PUT endpoints, authorization, validation, sqlc generation, and tests, while excluding all Experiment-dependent behavior.

### Actions Taken
- Performed the required pre-migration schema review and checked the current worktree, migration numbering, existing option/answer foreign keys, and report identity.
- Modified no migration, schema, query, generated Go, service, handler, or test file.
- Preserved the pre-existing unrelated modification to `scripts/create_sqlc_full_schema.sh`.

### Attempted Methods
- Evaluated the required `correct_answers.selected_option_id` relationship against the existing option replacement implementation, which deletes and recreates every option during question PUT.
- Considered `ON DELETE CASCADE`, `ON DELETE SET NULL`, and `RESTRICT`/`NO ACTION`; each choice imposes a different unresolved option-replacement policy, so none can be selected without maintainer direction.

### Issues & Blockers
- Phase 1 is blocked before migration creation because the correct-answer-to-option foreign-key deletion behavior depends directly on the explicitly unresolved question option replacement policy.
- `CASCADE` silently deletes the stored correct answer, `SET NULL` breaks the required CHOICE representation, and `RESTRICT`/`NO ACTION` makes current question option replacement fail once a correct answer exists.

### Next Steps
- Obtain maintainer confirmation for the option replacement policy and corresponding foreign-key behavior, then implement the Phase 1 migration, sqlc queries/generated code, service/handler layers, table-driven tests, and verification without adding Experiment-dependent behavior.

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

## [2026-08-24 23:55] Task Record

### Task Description
- Complete Phase 1 of the correct-answer API after the maintainer confirmed that deleting a referenced CHOICE option must cascade-delete its stored correct-answer record.

### Actions Taken
- Added migration 16 and the Question package schema for `correct_answers`, with CHOICE/TEXT payload constraints, version 1 default, regrade status, timestamps, question cascade, and selected-option cascade.
- Added sqlc GET and atomic UPSERT queries. Identical payloads preserve version, status, and `updated_at`; changed payloads increment version and set regrade status to `PENDING`.
- Generated sqlc query code and regenerated models with sqlc v1.30.0.
- Added `CorrectAnswerService` with question-type validation, CHOICE option ownership validation, and Unicode-aware TEXT reference-answer length validation.
- Added management GET/PUT handlers and wired both routes through the existing EXPERIMENTER/ADMIN authorizer.
- Added table-driven service and handler tests plus integration tests for versioning, idempotence, TEXT representation, and option-delete cascade behavior.
- Updated backend dependency wiring for the new service.
- Did not add `answers.experiment_id` or any Experiment, Page, submission grading, reachability, current-Experiment, or showScore behavior.

### Attempted Methods
- The initial schema merge attempt used `bash` from PATH, which was unavailable. Direct Git Bash initially resolved Windows `find`; rerunning Git Bash with `/usr/bin:/bin` first in PATH generated the merged schema and sqlc config correctly.
- The initial sqlc invocation was blocked by sandbox network restrictions. After approval, `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate` completed.
- The initial Go test run hit a Windows shared Go build-cache access error. A workspace-local temporary `GOCACHE` allowed all verification to complete; the temporary cache was removed afterward.

### Verification
- `go test ./...` passed.
- `go test -tags integration ./internal/question` compiled and passed; database-backed cases skipped because `QUESTION_INTEGRATION_DATABASE_URL` was not set.
- `go vet ./...` passed.
- `git diff --check` passed.
- sqlc v1.30.0 generation completed successfully.

### Issues & Blockers
- Database-backed integration assertions were not executed because no `QUESTION_INTEGRATION_DATABASE_URL` was provided; the integration test suite compiled and skipped cleanly.
- The pre-existing unrelated worktree modification to `scripts/create_sqlc_full_schema.sh` was preserved.

## [2026-08-25 10:04] Task Record

### Task Description
- Complete the next Experiment-independent Answers phase: persisted grading results, deterministic CHOICE grading primitives, stale-version projection, and paginated management answer listing with grading fields.

### Actions Taken
- Added migration 17 and Question schema for `answer_results`, including status, method, correctness, grading timestamp, correct-answer version, lifecycle checks, and answer cascade deletion.
- Replaced the unpaginated management answer query with count and paginated list queries joining persisted grading results and the current correct-answer version; added an answer-result UPSERT query for future transactional use.
- Regenerated sqlc models and query code with v1.30.0.
- Added typed Go grading status/method enums, a pure deterministic CHOICE comparison component, and a persisted-result projection that treats missing or stale-version grading as PENDING without exposing `isCorrect`.
- Updated `AnswerService.ListByQuestion` to validate pagination, calculate offsets/count metadata, and project grading fields without recalculation.
- Updated `GET /api/questions/{id}/answers` to return the standard pagination envelope and direct grading fields.
- Added table-driven grading, projection, service pagination, and handler response tests.
- Added integration tests for answer-result persistence, management joins/counting, and database constraints.
- Did not add Experiment/Page schema or SQL, `answers.experiment_id`, current-Experiment lookup, reachability, gradingMode/showScore lookup, or the final Student submission transaction.

### Attempted Methods
- The existing SQL files had mixed CRLF/LF endings after patching, causing `git diff --check` to report carriage returns as trailing whitespace. Normalized the modified query file to LF and reran the check successfully.
- Used a workspace-local Go build cache because the shared Windows Go cache previously produced access errors.

### Verification
- `go test ./...` passed.
- `go test -tags integration ./internal/question` passed compilation; database-backed cases skipped because `QUESTION_INTEGRATION_DATABASE_URL` was not set.
- `go vet ./...` passed.
- `git diff --check` passed after line-ending normalization.
- sqlc v1.30.0 generation completed successfully.

### Issues & Blockers
- The database-backed grading-result integration assertions remain unexecuted until a migrated PostgreSQL DSN is provided through `QUESTION_INTEGRATION_DATABASE_URL`.
- Production grading persistence still depends on the future Student submission transaction and Experiment-derived grading mode. Student result visibility still depends on Experiment `showScore`.
- The pre-existing unrelated worktree modification to `scripts/create_sqlc_full_schema.sh` was preserved.

## [2026-08-25 10:27] Task Record

### Task Description
- Fix three pre-Experiment compatibility issues: restore multi-user/newest-first management-list regression coverage, prevent stale regrade writes from replacing newer results, and require a correct-answer version for deterministic graded results.

### Actions Taken
- Added a database check to migration 17 and the Question sqlc schema requiring `correct_answer_version` only when an answer result is both `GRADED` and `DETERMINISTIC`; MANUAL semantics were not further constrained.
- Replaced the generic unconditional answer-result UPSERT with a create-only INSERT and a dedicated guarded regrade UPDATE. Regrade writes now require the target version to equal the question's current correct-answer version and refuse to replace a result with a newer stored version.
- Regenerated sqlc query code and interfaces with sqlc v1.30.0.
- Added handler regression coverage proving a management answer list contains distinct users and preserves the query's newest-first ordering.
- Expanded integration coverage for multi-user counting/pagination/newest-first order, the deterministic-version database invariant, allowed MANUAL grading without a version, and rejection of an older target-version regrade after a newer result exists.
- Kept the existing PR #57 Submit Answer handler/service flow unchanged. Added no Experiment/Page behavior and no `answers.experiment_id`.

### Attempted Methods
- Used the existing Git Bash schema-merging script with `/usr/bin:/bin` first in PATH, then ran sqlc v1.30.0 generation successfully.
- Used a workspace-local temporary Go build cache to avoid the known Windows shared-cache permission issue.
- A repository-wide `gofmt -l cmd internal` check reported pre-existing unrelated Go files; none of the Go files modified for this task were listed. The modified Go test files were formatted directly with `gofmt`.

### Verification
- `go test ./internal/question ./cmd/backend` passed.
- `go test ./...` passed.
- `go test -tags integration ./internal/question` passed compilation; database-backed cases skipped because `QUESTION_INTEGRATION_DATABASE_URL` was not set.
- The focused verbose integration run confirmed the three relevant database tests skipped for the same missing DSN.
- `go vet ./...` passed.
- `git diff --check` passed.
- sqlc v1.30.0 generation completed successfully.

### Issues & Blockers
- The new database constraint and guarded-regrade integration assertions have not executed against PostgreSQL because `QUESTION_INTEGRATION_DATABASE_URL` is unavailable; they compile and skip cleanly.
- Experiment-derived submission context, production grading transaction, reachability, and score visibility remain intentionally deferred.
- The pre-existing unrelated worktree modification to `scripts/create_sqlc_full_schema.sh` was preserved.

### Next Steps
- Before Experiment-dependent submission work, run the integration suite against a database migrated through version 17.
- Future regrade workers must use `RegradeAnswerResultForVersion`; they must not recreate an unconditional conflict-update path.

## [2026-08-25 16:51] Task Record

### Task Description
- Complete the pre-integration Answer orchestration phase without adding Experiment/Page SQL or schema: consumer-owned context/visibility boundaries, atomic submission contract, grading decisions with explicit assumption boundaries, and ownership/visibility projection.

### Actions Taken
- Added an Answer-owned `SubmissionContextResolver` and minimal `SubmissionContext` containing only Experiment ID and Answer-local grading mode; no Experiment models are imported.
- Added a separate narrow `ResultVisibilityResolver` for future `showScore` lookup.
- Added an unwired `AnswerSubmissionOrchestrator` and `AnswerSubmissionTransaction` boundary. The command carries validated Answer input, resolved context, and a grading decision so the future repository can perform duplicate detection, correct-answer read/comparison, Answer insert, and result insert atomically.
- Refactored the existing PR #57 `AnswerService.Create` to reuse a private validation method; its current validation and direct persistence behavior remain unchanged.
- Added confirmed grading behavior: AUTOMATIC CHOICE selects deterministic grading, and TEXT remains PENDING without invoking LLM grading.
- Encoded reasonable but unconfirmed pre-integration assumptions that pending MANUAL CHOICE/TEXT records use method MANUAL and pending AUTOMATIC TEXT omits the method.
- Added a pure ownership/result-visibility projection: management may read any result, students may read only their own, and student correctness is hidden when `showScore` is false.
- Added table-driven orchestration, decision, ownership, and visibility tests with fake resolvers/transaction boundaries.
- Added no migration, `answers.experiment_id`, duplicate constraint, Experiment/Page/Course code, production resolver, or production transaction wiring.

### Attempted Methods
- Used a workspace-local temporary `GOCACHE` to avoid the known Windows shared Go-cache permission issue.
- Checked for `golangci-lint`; it was not installed or available in PATH, so repository lint could not be executed locally.

### Verification
- `go test ./internal/question ./cmd/backend` passed.
- `go test ./...` passed.
- `go vet ./...` passed.
- Modified Go files were formatted with `gofmt`.
- `git diff --check` passed.

### Issues & Blockers
- No new blocker was introduced by this phase.
- Production context resolution, question reachability, `showScore` lookup, duplicate enforcement, and atomic persistence remain dependent on merged PR #61/#63 integration and the later `answers.experiment_id` policy.
- Missing-correct-answer behavior is deliberately not encoded in the pre-integration transaction contract; it remains a production integration decision unless confirmed separately.
- Whether `resultVisible` is true for PENDING, FAILED, or stale results when `showScore=true` remains an unconfirmed assumption; only omission of `isCorrect` in those states is confirmed.

### Next Steps
- After PR #61/#63 are integrated, implement an adapter that resolves exactly one current reachable Experiment and implements the consumer-owned interfaces without exposing Experiment/Page SQL to `internal/question`.
- Implement the transaction repository only after `answers.experiment_id`, legacy-row handling, uniqueness, and FK deletion behavior are confirmed.

## [2026-08-25 16:59] Task Record

### Task Description
- Correct documentation that overstated inferred pre-integration grading and result-visibility behavior as confirmed requirements.

### Actions Taken
- Updated the submission grading comment and the earlier task record to separate confirmed AUTOMATIC CHOICE/TEXT behavior from inferred pending-method representation.
- Documented that `resultVisible=true` for PENDING, FAILED, or stale results with `showScore=true` is a reasonable but unconfirmed assumption.
- Changed no executable behavior, tests, schema, migrations, SQL, handler wiring, or Experiment/Page/Course code.

### Verification
- Ran `gofmt` on the two Go files whose comments changed.
- `go test ./internal/question ./cmd/backend` passed.
- `go vet ./...` passed.
- `git diff --check` passed.

### Issues & Blockers
- Maintainer confirmation is still needed for pending grading-method representation and `resultVisible` semantics when `showScore=true` but no current correctness result exists.

### Next Steps
- Preserve the current provisional implementation until the maintainer confirms or changes those semantics.

## [2026-08-25 19:36] Task Record

### Task Description
- Continue only the Experiment-independent Answer result preparation while leaving multiple-current-Experiment resolution and Answer-to-Experiment FK lifecycle policy undecided.

### Actions Taken
- Added `FindAnswerResultByQuestionAndID`, a SELECT-only sqlc query that locates an Answer by both IDs and left-joins its persisted grading result and current correct-answer version.
- Regenerated sqlc v1.30.0 query code.
- Added `AnswerResultService`, which performs the double-ID lookup, uses `ProjectGrading`, enforces Student ownership, permits management access, and applies the existing provisional visibility projection using caller-supplied `showScore`.
- Added response-model/build preparation for the future handler without registering the route or adding production visibility wiring.
- Added table-driven service tests for ownership, management access, missing/PENDING/FAILED/current/stale results, score hiding, exact query parameters, and query errors.
- Added table-driven response serialization tests proving omitted grading fields remain omitted, and expanded the fake transaction-boundary test to cover persistence error propagation.
- Added no migration, `answers.experiment_id`, Experiment/Page/Course SQL, production showScore resolver, submit wiring, duplicate behavior, or route registration.

### Attempted Methods
- Initial sqlc generation was blocked by sandbox network restrictions; the approved sqlc v1.30.0 command then completed.
- The first targeted test compile found that the existing handler test file did not import testify; added the imports used by the new table-driven response test.
- Initial error assertions expected Summer database wrappers to preserve `errors.Is`; adjusted tests to assert the stable wrapped error message because those wrappers intentionally replace the original error chain.
- Used a workspace-local temporary `GOCACHE` to avoid the known Windows shared-cache permission issue.

### Verification
- `go test ./internal/question ./cmd/backend` passed.
- `go test ./...` passed.
- `go vet ./...` passed.
- Modified Go files were formatted with `gofmt`.
- `git diff --check` passed.
- `golangci-lint` was not available in PATH.

### Issues & Blockers
- No new policy decision was needed for this preparation phase.
- The production route remains intentionally unregistered because an Answer still has no Experiment ID to supply to `ResultVisibilityResolver`.
- The existing provisional `resultVisible=true` behavior for PENDING, FAILED, or stale results with `showScore=true` remains explicitly unconfirmed.
- Multiple simultaneous current Experiments and Answer-to-Experiment FK deletion/lifecycle behavior remain untouched and unresolved.

### Next Steps
- After the Experiment identity is persistable, call `ResultVisibilityResolver`, classify the authenticated actor, register the route, and add end-to-end handler tests.
- Do not add `answers.experiment_id` until legacy-row handling and FK lifecycle policy are confirmed.

## [2026-08-25 21:59] Task Record

### Task Description
- Begin the many-to-many Answer–Experiment persistence redesign with per-association grading, while stopping before migration changes if the Experiment-side association deletion policy remains ambiguous.

### Actions Taken
- Read the maintainer's attached requirements and rechecked the current working tree, migration inventory, repository instructions, and the previously reviewed PR #61/#63 migration assignments.
- Confirmed that the current Answer work owns migrations 16 and 17, while Page and Experiment were assigned migrations 13 and 14 respectively; migration 18 is not currently occupied in this working tree.
- Made no schema, migration, SQL, generated-code, service, handler, interface, or test changes.
- Preserved all unrelated and existing Phase 1/2 working-tree changes.

### Attempted Methods
- Tried to inspect the previously recorded PR #61/#63 commit objects locally, but those objects are not present in this repository clone.
- Tried the GitHub CLI as a read-only fallback, but `gh` is not installed in the environment. The migration assignments remain supported by the earlier completed PR review and repository report.
- Evaluated the confirmed requirement that Answer records survive independently of Experiment lifecycle. This rules out cascading deletion of Answer records, but does not uniquely determine whether deleting an Experiment should cascade-delete only its `answer_experiments` rows or be restricted so association/provenance remains available.

### Issues & Blockers
- The exact `answer_experiments.experiment_id` foreign-key behavior remains ambiguous. `ON DELETE CASCADE` preserves the Answer but removes its association and association-level grading result; `ON DELETE RESTRICT` preserves provenance but prevents hard deletion of a referenced Experiment. The maintainer has not selected between those materially different lifecycle semantics.
- The task explicitly requires stopping before choosing this FK behavior, so migration 18 and all dependent refactors were intentionally not created.
- Duplicate enforcement across Answer fields and the association remains a separate design item, but it was not reached because the earlier FK blocker prevents safely establishing the association schema.

### Next Steps
- Obtain an explicit maintainer decision for what happens to `answer_experiments` and its per-association grading row when a linked Experiment is hard-deleted: cascade-delete the association/result while preserving Answer, or restrict Experiment deletion/preserve provenance.
- After confirmation, create migration 18, refactor `answer_results` to an association-level composite key/FK, generate sqlc, update consumer-owned interfaces and repository operations, and add the requested unit/integration coverage.

## [2026-08-25 22:08] Task Record

### Task Description
- Continue the many-to-many persistence phase after the maintainer confirmed that hard-deleting an Experiment may cascade-delete its Answer association and per-association grading result while preserving the Answer row.

### Actions Taken
- Confirmed the intended foreign-key direction: `answer_experiments.experiment_id -> experiments.id ON DELETE CASCADE`, with association-level grading results referencing the composite association identity and cascading with it.
- Inspected the current migration inventory, migration 17, Question merged schema, Answer queries, submission abstractions, grading/result services, and PR #61's migration 14 definition.
- Made no migration, schema, SQL, generated-code, service, interface, handler, or test changes after finding a prerequisite schema blocker.

### Attempted Methods
- Retrieved PR #61's patch read-only and verified that it introduces `experiments` in migration 14, including an `id UUID PRIMARY KEY`.
- Evaluated whether migration 18 and sqlc generation could safely proceed against the current branch. The branch does not contain migration 14 or an Experiment schema input, so both the migration chain and merged sqlc schema lack the referenced table.
- Rejected adding a placeholder/stub Experiment declaration or omitting the FK because either approach would copy/recreate out-of-scope PR #61 schema or make the generated schema differ from the confirmed migration design.

### Issues & Blockers
- PR #61's Experiment schema is not merged into the current working branch. A migration 18 containing `REFERENCES experiments(id)` would fail when applied to the repository's current migration chain.
- The repository-standard merged-schema/sqlc generation cannot model the confirmed composite association FK until the Experiment schema is present. The task explicitly prohibits copying or recreating PR #61 schema locally.
- This is distinct from the now-resolved lifecycle decision; the remaining blocker is the absence of the concrete prerequisite table in the implementation target.

### Next Steps
- Merge or otherwise make PR #61 migration 14 and its schema input available in the target branch, without manually recreating it as part of the Answer change.
- Then create migration 18 with `answer_experiments`, refactor `answer_results` to key by `(answer_id, experiment_id)`, regenerate sqlc, update the consumer-owned multi-context interfaces, and add the requested unit/integration tests.

## [2026-09-03 22:05] Task Record

### Task Description
- Resume Answer/CorrectAnswer work from the existing uncommitted implementation, safely synchronize merged Course/Page/Experiment dependencies, re-read the merged API contract, and implement the singular Answer-to-Experiment design without retaining the obsolete many-to-many plan.

### Actions Taken
- Fetched the latest backend refs and verified that the current branch HEAD `0b21642` was an ancestor of `origin/main` with no branch-only commits.
- Preserved all tracked and untracked work in `stash@{0}` with the label `preserve answer-correct-answer work before syncing origin-main`, verified the stash's 34-file inventory, and kept the stash as a recovery snapshot.
- Fast-forwarded the current branch to backend `origin/main` commit `2a4a2df`, which contains merged PR #63 and its PR #61 dependency.
- Reapplied the stash without popping it. The only conflict was `internal/question/errors.go`; resolved it by retaining both upstream `errQuestionReferenced` and the Answer work's `errInvalidCorrectAnswerPayload`.
- Verified that migrations 13 (Page) and 14 (Experiment), Page-to-Question reachability, Experiment participants/courses, ACTIVE schedule checks, `gradingMode`, and `showScore` configuration are now present.
- Fetched and re-read API `origin/main` commit `ee0ee32`, including the updated `service/questions.tsp` contract.
- Made no Answer schema, migration, SQL, handler, service, grading, or production resolver implementation changes after identifying contract decisions required before migration work.

### Attempted Methods
- Compared dirty paths with upstream changes before synchronization; nine files overlapped, so a direct merge was rejected in favor of a verified stash plus fast-forward plus stash-apply workflow.
- Checked whether the merged Experiment schema guarantees a unique current Experiment per Student. It does not: `(experiment_id, user_id)` only prevents duplicate membership in one Experiment, and the existing access query uses `EXISTS` across all valid ACTIVE Experiments.
- Evaluated adding a required singular `answers.experiment_id`. Existing migration 11 permits legacy Answer rows with no Experiment provenance, while the updated contract requires every Answer to belong to exactly one Experiment. No safe backfill rule is defined.

### Verification
- `go test ./internal/question ./cmd/backend` passed after dependency synchronization and conflict resolution.
- `git diff --check` passed.
- No conflict markers remain, every path captured by the stash exists in the restored working tree, and the recovery stash remains available.

### Issues & Blockers
- The contract says submission uses "the student's active experiment", but the merged schema permits multiple simultaneously valid ACTIVE Experiments for the same Student and Question. No selection, rejection, or tie-breaking behavior is defined.
- Existing Answer rows have no `experiment_id`. Adding the contract-required NOT NULL foreign key needs a maintainer-approved legacy/backfill policy; assigning an arbitrary Experiment or deleting legacy Answers would be speculative.
- The singular Answer-to-Experiment foreign-key deletion policy is not defined by the merged API contract. The earlier cascade decision applied to the now-obsolete association table and cannot safely be transferred to deleting singular Answer rows.
- AUTOMATIC CHOICE submission when no CorrectAnswer exists remains unspecified, as do the provisional pending grading-method and `resultVisible` details documented in earlier records.
- The merged API contract still does not add `experimentId` to the individual result endpoint; it is required only for the management list. With singular Answer ownership, the result's Experiment can be derived from the Answer once persistence exists.

### Next Steps
- Obtain maintainer decisions for multiple simultaneous valid current Experiments, legacy Answer backfill, and singular Experiment FK deletion behavior before creating the next migration.
- Confirm missing-CorrectAnswer submission behavior and the remaining provisional grading/result-visibility semantics before final transaction and result-handler wiring.
- After confirmation, replace the old question-global list/result joins with Experiment-scoped queries, implement singular `answers.experiment_id` and the duplicate constraint, update sync status semantics to `PENDING/SYNCED/FAILED`, and wire the production resolver through the merged ECP schema.

## [2026-09-04] Repair interrupted Answer refactor

### Actions Taken
- Continued the dirty tree at 2a4a2df without reset, revert, stash operations, commits, or pushes. Existing recovery stash remains untouched.
- Kept migrations 16–18 and migration 17's grading model unchanged. Migration 18 remains unapplied by this task and contains the previously approved destructive legacy-Answer clearing step.
- Updated internal/question/answer_queries.sql and correct_answer_queries.sql, then generated sqlc rather than editing generated code. Fixed the generated AnswerExperimentConfiguration name collision and nullable scope UUID mismatch.
- Updated answer_submission.go, answer_service.go, correct_answer_service.go, handler.go and cmd/backend/main.go. Production POST now uses the consumer-owned scoped orchestrator. The body rejects experimentId; authenticated user identity and server-resolved ACTIVE/PUBLISHED reachability determine scope. The transaction rechecks scope/configuration and locks the Question to serialize with CorrectAnswer writes.
- Added answer_sync_store.go and answer_sync_service.go. Pending CorrectAnswer synchronization is resumed by a background loop, with bounded 100-result transactions and three attempts per failed batch before version-guarded FAILED marking. Missing and stale result eligibility includes question type, automatic Experiment mode, available current correct answer, and persisted result state; MANUAL results are excluded. Guarded INSERT/UPDATE can fill missing results, validates deterministic correctness, and cannot replace newer/current graded versions or MANUAL results. Correct-answer row locking prevents an old target racing a newer update. GET remains unchanged/read-only.
- Fixed renamed/scoped references in handler_test.go, answer_list_service_test.go, correct_answer_service_test.go and answer_result_store_integration_test.go. Added answer_scope_test.go, answer_sync_service_test.go, answer_sync_store_integration_test.go, and MANUAL version-exemption cases in grading_test.go.
- Integration fixtures now create explicit Experiment provenance; existing multi-user/newest-first list regression coverage is preserved.

### Verification and Environment
- Schema merge script and sqlc v1.30.0 generation succeeded; go generate ./... succeeded.
- gofmt applied to modified Go sources; question/backend tests, full go test ./..., go vet ./..., go build ./..., staticcheck 2025.1.1 and git diff --check passed before final report.
- Integration test package compiles. Database cases were skipped because QUESTION_INTEGRATION_DATABASE_URL is absent; Docker was not found on PATH or its standard installation path. No DB migrations were executed and no local DB data was removed.
- Initial generation failed because Windows FIND shadowed Unix find and sandbox denied module/cache access. Retried with Git Bash /usr/bin PATH and approved Go access successfully.
- Obsolete Question RegradeStatus and unscoped list identifiers are gone. ExperimentStatusCOMPLETED remains intentionally unchanged because it is an Experiment lifecycle status.
- Final golangci-lint v2.4.0 (matching CI) completed with 0 issues. Final submission JSON explicitly rejects experimentId, including case variants, rather than letting Summer's default unknown-field handling silently ignore it. Focused tests cover this rejection.

### Remaining Questions / Release Gate
- No-CorrectAnswer submission still uses the pre-existing explicitly provisional Answer-only persistence branch. This is not a finalized public result contract; no new status or pendingReason was added. Missing-result projection and pending method/resultVisible assumptions remain provisional.
- Individual result handler/production visibility integration is not completed by this submission-focused repair; persisted-result service/projection remains read-only.
- Real PostgreSQL validation (including concurrency, reachability fixtures, migrations, and duplicate enforcement) remains required before release. This task must not be described as fully integration-tested.
- The old many-to-many plan is obsolete. Production uses singular Answer.experiment_id and does not import Experiment domain models into Question.

## [2026-09-04] Persist PENDING result when CorrectAnswer is absent

### Confirmed Requirement and Changes
- The maintainer now confirms Answer plus PENDING AnswerResult persistence when CorrectAnswer is absent. This supersedes the previous entry's Answer-only provisional branch and removes that specific product blocker.
- Modified internal/question/answer_submission.go: removed the early Answer-only commit, extracted initialSubmissionGrading, and always creates the result before the shared transaction commit. A real read error still aborts; only pgx.ErrNoRows means missing CorrectAnswer.
- PENDING retains the existing submission decision method (DETERMINISTIC for AUTOMATIC CHOICE, MANUAL for MANUAL); is_correct, graded_at, and correct_answer_version are null. No synthetic version, enum, reason, SQL, migration, or GET changes were introduced. Existing TEXT/MANUAL method conventions were preserved, not redefined as newly confirmed requirements.
- Added internal/question/answer_initial_grading_test.go for missing/present CorrectAnswer, read failures, MANUAL CHOICE/TEXT, AUTOMATIC TEXT, and pending field invariants.
- Added internal/question/answer_pending_submission_integration_test.go for the production resolver/orchestrator/transaction using Experiment/Course/Page fixtures, real persisted PENDING rows, later correct-answer synchronization, and MANUAL exclusion.
- Updated internal/question/answer_sync_store_integration_test.go to cover MANUAL PENDING even inside an AUTOMATIC Experiment. Existing stale-version tests remain unchanged.

### Verification
- Ran schema merge, sqlc v1.30.0 generation, go generate ./..., gofmt, relevant and full unit tests, focused integration tests, vet, build, golangci-lint v2.4.0, and diff check.
- Relevant and full unit tests passed. Integration package compiled; database cases skipped because QUESTION_INTEGRATION_DATABASE_URL is unset. No migrations were applied.
- Generation, gofmt, go vet ./..., go build ./..., and git diff --check passed. golangci-lint v2.4.0 completed with 0 issues.
- Recovery stash preserved; no reset, revert, stash, commit, or push operations performed.

### Remaining Work
- Actual PostgreSQL integration/concurrency verification remains a release gate.
- Individual result endpoint production visibility/handler integration and previously documented provisional method/resultVisible details remain separate work; the no-CorrectAnswer persistence decision itself is no longer unresolved.

## [2026-09-04] Production result endpoint and locked payload validation

### Changes
- Updated cmd/backend/main.go and internal/question/handler.go; added internal/question/answer_result_handler.go. Registered GET /api/questions/{questionId}/answers/{answerId}/result and wired AnswerResultService plus the existing auth RoleQuerier.
- Auth context contains only user ID/access expiry, not roles. The handler uses authenticated user ID and ActiveUserRoles, matching the existing Authorizer convention; no client-supplied role or visibility is accepted.
- Updated internal/question/answer_result_service.go: removed ShowScore from AnswerResultAccess, injected the consumer-owned ResultVisibilityResolver, and resolves visibility using the persisted Answer.experiment_id only after ownership validation. Management access bypasses score hiding; other students receive 404. Projection and serialization remain unchanged and GET performs only reads.
- Updated internal/question/answer_submission.go and answer_sync_store.go: after locking Question, read authoritative state and reuse payload/type validators, verify selected-option ownership with transaction-bound queries, then write. Submission recomputes grading decision from the locked Question type. CorrectAnswer PUT preserves validation errors through correct_answer_service.go so a detected mismatch remains a validation response.
- Updated answer_result_service_test.go; added answer_result_route_test.go and answer_locked_validation_test.go. Tests exercise production route registration, student/management/unauthenticated roles, stored Experiment visibility versus query parameters, missing/mismatched resources, pending/failed/stale projection, visibility lookup failure, and lock-before-validation/no-write/rollback for both directions of Question type changes.
- No migrations, schema, SQL, correct-answer version semantics, Experiment models, or provisional projection rules were changed. Recovery stash remains untouched; no commit or push.

### Verification
- Ran schema merge, sqlc v1.30.0, go generate, gofmt, relevant/full unit tests, integration-tag package tests, vet, build, staticcheck 2025.1.1, golangci-lint v2.4.0, and git diff --check.
- Relevant and full unit tests pass, including new result-route and locked-state tests. Integration package compiles; actual DB cases skip because QUESTION_INTEGRATION_DATABASE_URL is unset. No migrations applied.
- Final generation, formatting, vet, build, staticcheck, and diff checks passed; golangci-lint v2.4.0 reported 0 issues.

### Remaining Work
- The required result endpoint is now exposed in production; it is no longer an implementation gap.
- CorrectAnswer delete/recreate version reuse remains an unresolved maintainer decision and was intentionally not fixed. Existing provisional resultVisible semantics remain unchanged.
- Real PostgreSQL concurrency/integration and deployed API contract verification remain verification work, not proof that the newly wired route is absent.

## [2026-09-04] Scoped duplicate Answer conflict fix

### Changes
- internal/question/answer_submission.go recognizes only PostgreSQL 23505 for answers_experiment_user_question_unique during CreateAnswer and returns the domain duplicate error. Existing deferred rollback remains unchanged; unrelated database errors retain Summer wrapping.
- internal/question/errors.go adds errDuplicateAnswer; internal/question/handler.go maps it through the existing Summer ProblemWriter to HTTP 409 without changing the existing question-reference conflict response.
- Added internal/question/answer_duplicate_route_test.go: table-driven production POST route, real orchestrator and Store with a fake PostgreSQL transaction. Scoped duplicate returns 409; unrelated uniqueness and foreign-key errors remain 500. All cases assert insertion was attempted, rollback occurred and commit did not occur.
- No migration, SQL, generated code, grading, visibility, Experiment scope or version semantics changed. Recovery stash untouched; no commit, push or migrations applied.

### Verification
- gofmt, go test ./internal/question ./cmd/backend, go test ./..., go vet ./..., go build ./..., staticcheck 2025.1.1 and git diff --check passed.
- Integration-tag package compiled and the new route regression passed with that tag. Real PostgreSQL tests were not executed because QUESTION_INTEGRATION_DATABASE_URL is unset.
- Initial full checks encountered access denied in the Go build cache; rerunning with approved cache access passed. Referenced docs/API.md and instructions/summer.instruction.md are absent; inspected local API origin/main and installed Summer error handling instead.
- The confirmed duplicate HTTP-status blocker is fixed. CorrectAnswer delete/recreate version reuse remains a maintainer decision; PostgreSQL execution remains verification work.

## [2026-09-04] Confirmed CorrectAnswer deletion invalidation

### Decision and deletion paths
- Maintainer confirmed deletion invalidates deterministic results to PENDING with NULL correctness, graded_at and correct_answer_version, preserving MANUAL and existing method semantics. This supersedes the earlier unresolved delete/recreate decision.
- No CorrectAnswer DELETE route/query exists. QuestionService.UpdateWithOptions -> SyncQuestionOptions -> OptionService.Delete -> DeleteOption removes options; correct_answers.selected_option_id cascades. Question deletion cascades both CorrectAnswer and Answer/results. Direct SQL CorrectAnswer deletion is also covered by the new trigger. Schema teardown migrations are not a retained-result application lifecycle.
- Service-only invalidation cannot cover FK cascades/direct deletion. Added migration 19 rather than changing migrations 16–18; it installs an AFTER DELETE row trigger on correct_answers that invalidates only DETERMINISTIC results joined by question ID in the deleting transaction. The down migration removes the trigger/function. No migrations were applied.

### Exact task changes
- internal/database/migrations/19_correct_answer_delete_invalidation.up.sql
- internal/database/migrations/19_correct_answer_delete_invalidation.down.sql
- internal/question/schema.sql: matching trigger/function for schema generation.
- internal/question/correct_answer_delete_integration_test.go: direct deletion, option cascade, question cascade, rollback, cleared fields, MANUAL preservation, reused version remaining pending and later eligible synchronization.
- internal/question/grading_test.go: invalidated result projection remains PENDING without correctness both before and after version 1 recreation.
- reports/report-ruNYCU-codex-gpt-5.6-sol.md: this record.
- No production Go service/handler changes, query changes, global counter/history/status additions or grading eligibility changes. Answers whose selected option was deleted remain ineligible under the existing selected-option requirement; the synchronization test uses an option that survives deletion.

### Verification and remaining work
- Ran repository schema merge, sqlc v1.30.0 generation, go generate ./..., gofmt on modified Go files, go test ./internal/question ./cmd/backend, go test ./..., go vet ./..., go build ./..., staticcheck 2025.1.1 and git diff --check: passed.
- Integration-tag package and duplicate-route regression passed. All three new database test cases skipped because QUESTION_INTEGRATION_DATABASE_URL is unset; trigger execution/rollback/cascade behavior is not claimed verified against PostgreSQL.
- No confirmed implementation blocker remains identified in this scoped fix. Migration 19 deployment and real PostgreSQL integration verification remain necessary. No reset/revert/stash/commit/push or migration application; recovery stash remains intact.

## [2026-09-05] Final pre-PR verification

### Manual and database verification
- Created the one-time smoke fixture only in the disposable sciedu_answer_test database. No fixture SQL/helper/UUID, Yaak file, bootstrap data, or environment file was added to repository changes.
- Manual Yaak verification passed: client-supplied experimentId returned 400; valid TEXT submission returned 201; the server resolved the expected ACTIVE Experiment; the creation response omitted grading fields; duplicate (experimentId, userId, questionId) submission returned 409.
- Re-ran all Question integration tests against sciedu_answer_test at migration 19 with QUESTION_INTEGRATION_DATABASE_URL set. The database-backed submission, reachability, persisted PENDING result, synchronization eligibility, MANUAL exclusion, stale-version protection, constraints, CorrectAnswer representation/versioning, deletion invalidation/cascades/rollback, version reuse, and management-list tests passed.

### Final checks
- Repository schema merge, sqlc v1.30.0 generation, and go generate ./... succeeded. Pre/post hashes confirmed generated Go output was already up to date.
- go test ./..., go test -tags integration ./internal/question -count=1, go vet ./..., go build ./..., staticcheck 2025.1.1, and git diff --check passed.
- golangci-lint is unavailable in the current PATH and expected Go bin location, so it was not rerun in this final pass. An earlier v2.4.0 run before the migration-19 integration test was added completed with zero issues; current staticcheck, tests, vet, and build cover the final tree.
- Current HEAD equals origin/main (2a4a2df); local main is stale at ae29eae. The intended PR diff is the uncommitted working tree against HEAD/origin/main, not the misleading aggregate against local main.
- `.env` remains ignored and untracked. Recovery stash remains untouched. No reset, revert, new stash, commit, push, or PR operation was performed.

### Readiness
- No confirmed implementation blocker remains. Production PostgreSQL deployment must apply migrations 16 through 19 in order; migration 18 intentionally clears legacy Answers under the maintainer-approved provenance strategy.
- Existing explicitly documented provisional TEXT/MANUAL method and resultVisible projection semantics remain known contract limitations, not newly introduced blockers.

## [2026-09-05] Answer pagination review fix

### Changes and verification
- Updated internal/question/handler.go so Answer management-list pagination uses URL query presence, matching Course and Experiment: absent page/pageSize use 1/20; present empty values are parsed and rejected.
- Updated the production handler table test to verify omitted defaults, empty page/pageSize returning 400, and non-integer page/pageSize returning 400.
- No service, SQL, schema, migration, grading, visibility, submission, or result behavior changed.
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

## [2026-09-17 00:24] Task Record

### Task Description
- Address the SCIEDU-121 PR review request to allow the `Idempotency-Key` request header in CORS preflight responses.

### Actions Taken
- Updated `internal/cors/middleware.go` to append `Idempotency-Key` to the existing `Access-Control-Allow-Headers` value.
- Updated `internal/cors/middleware_test.go` so the preflight requests `Idempotency-Key` and asserts the exact allowed-header list retains `Content-Type` and `Authorization`.
- Left the pre-existing untracked `q` file and recovery stash untouched.

### Attempted Methods
- The initial patch retained Windows CRLF line endings, causing `git diff --check` to flag trailing whitespace. Ran `gofmt` on only the two changed CORS Go files and verified the final diff is limited to the intended lines.

### Verification
- `go test ./internal/cors`, `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check` passed.

### Issues & Blockers
- None for this CORS review request.

### Next Steps
- Review and stage the focused CORS change with the existing SCIEDU-121 PR workflow; no commit or push was performed here.

## [2026-09-26 17:38] Task Record

### Task Description
- Complete the in-progress `origin/main` merge into SCIEDU-110 after the maintainer selected the migration collision strategy, and restore generated sqlc consistency without committing the merge.

### Actions Taken
- Preserved both append-only report histories while removing only merge conflict markers.
- Renamed the incoming PageVisit migration pair from version 15 to version 20; migrations 16 through 19 and all migration SQL contents remain unchanged.
- Rebuilt the merged schema/sqlc configuration, regenerated sqlc output, and ran `go generate ./...`.
- Staged the regenerated `internal/pagevisit/models.go`, which now reflects Answer/CorrectAnswer/AnswerResult and Chat `agentic_data` additions from the combined schema.
- Left the unrelated untracked `q` and recovery stash untouched.

### Attempted Methods
- The host has no `make`, installed `sqlc`, or Docker CLI, so the Makefile's three generation steps were invoked directly.
- CI-pinned sqlc v1.28.0 panicked in its Windows WASM PostgreSQL parser. The previously used Windows-compatible sqlc v1.30.0 completed generation; CI remains configured to run the normal `make gen` workflow with Linux sqlc v1.28.0.
- The first `go generate ./...` attempt could not write the default Windows Go cache; rerunning with the workspace-local cache passed.

### Verification
- Migration up/down versions are unique and ordered through 20: Agentic Chat 15, SCIEDU-110 16–19, PageVisit 20.
- No unmerged paths or conflict markers remain; working-tree and staged `git diff --check` passed.
- `go test ./...`, `go vet ./...`, and `go build ./...` passed.
- Integration-tag database tests compile and pass their non-database setup, but real migration execution was not run because `MIGRATION_INTEGRATION_DATABASE_URL` is unset.

### Issues & Blockers
- No confirmed merge blocker remains.

### Next Steps
- Review the staged merge and commit it when ready; no commit or push was performed here.

## [2026-09-26] Task Record

### Task Description
- Implement SCIEDU-113 `GET /api/experiments/current` for authenticated Students, returning the current ACTIVE Experiment and its assigned PUBLISHED Courses.

### Actions Taken
- Added current-Experiment and PUBLISHED-course sqlc queries using the established half-open schedule window (`scheduled_start_at <= CURRENT_TIMESTAMP` and `CURRENT_TIMESTAMP < scheduled_end_at`).
- Added Store and Service flow for resolving the authenticated Student's current Experiment and assigned PUBLISHED Courses.
- Added defensive handling for invalid overlapping current Experiments without inventing a tie-breaker; valid domain writes already reject overlapping participant schedules.
- Registered the Student-only route and reused the existing Experiment and Course response projections.
- Added table-driven service, handler, route-authorization, and PostgreSQL integration tests, including schedule-boundary coverage.

### Attempted Methods
- The repository-wide sqlc command could not complete because the current `sqlc.yaml` references Answer query files absent from this branch. Generated only the Experiment package with sqlc v1.30.0 and removed unrelated generated model drift.
- Attempted to verify the known disposable PostgreSQL database, but Docker CLI was unavailable in this environment; integration-tagged tests compiled and reported their database-backed cases as skipped because `EXPERIMENT_INTEGRATION_DATABASE_URL` was unset.

### Verification
- `go test ./internal/experiment` passed.
- `go test ./...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `go test -tags integration ./internal/experiment -count=1 -v` passed for non-database tests; all PostgreSQL-backed tests were explicitly skipped because `EXPERIMENT_INTEGRATION_DATABASE_URL` was not set.
- `git diff --check` passed.
- `staticcheck` and `golangci-lint` were unavailable.

### Issues & Blockers
- No SCIEDU-113 implementation blocker remains.
- Real PostgreSQL execution of the new current-Experiment integration cases remains a verification-only follow-up.
- `internal/experiment/queries.sql.go` was regenerated locally and remains ignored, matching the existing Experiment package convention; no generated file is added to the tracked diff.

### Next Steps
- Run the Experiment integration suite against a dedicated migrated PostgreSQL database before commit when available.

## [2026-09-27] Task Record

### Task Description
- Prepare and execute a minimal local Yaak-compatible smoke test for SCIEDU-113 `GET /api/experiments/current` against the disposable `sciedu_pagevisit_test` database.

### Actions Taken
- Verified the target database name, migration state (`15`, `dirty=false`), deterministic development Student, and absence of existing Experiment fixtures using a temporary parameterized pgx helper that was removed afterward.
- Inserted one disposable PUBLISHED Course, one current ACTIVE Experiment, its Student participant assignment, and its Course assignment in one transaction.
- Avoided the repository's duplicate version-15 migration source by creating a temporary migration directory containing versions 1-14 plus `15_page_visits`; no repository migration was renamed or edited.
- Started the current working-tree backend with CLI flags that override `.env`, authenticated through `/api/auth/dev-login`, and called `/api/experiments/current` with a cookie session.
- Confirmed a 200 response containing Experiment `11300000-0000-4000-8000-000000000002` and only PUBLISHED Course `11300000-0000-4000-8000-000000000001`.
- Stopped the temporary backend process and removed the temporary migration directory. The unrelated `q` file and recovery stash were untouched.

### Attempted Methods
- An absolute Windows `file:///C:/...` migration source failed before database migration because golang-migrate rejected the path format. The repository-style relative source `file://.tmp_sciedu113_migrations` succeeded and reported no migration required.
- Sending Ctrl-C to the `go run` parent left the compiled child listening on port 8080. Verified the exact PID/path belonged to this run, stopped that child explicitly, and confirmed the port was no longer serving.

### Verification
- Database precheck: `current_database() = sciedu_pagevisit_test`, schema migration version 15, `dirty=false`.
- Fixture transaction committed successfully; the target DB then contained one Experiment and one participant assignment.
- Backend migration startup reported version 15 and `Database schema is up to date, no migration required`.
- `POST /api/auth/dev-login` returned 200.
- `GET /api/experiments/current` returned 200 with the expected ACTIVE Experiment and PUBLISHED Course.

### Issues & Blockers
- No smoke-test blocker remains.
- The fixture intentionally remains only in the disposable `sciedu_pagevisit_test` database for manual Yaak testing; it was not added to repository code.
- A 404 case was not executed because the single deterministic dev Student now has the happy-path fixture, and mutating it solely for an optional test was unnecessary.

### Next Steps
- Start the backend with the documented temporary migration-source procedure, use Yaak's cookie jar for dev-login, and call `/api/experiments/current`.

## [2026-10-09] Task Record

### Task Description
- Address SCIEDU-113 review feedback by untracking the generated Experiment sqlc file and preventing overlapping current Experiments when an Experiment is activated again.

### Actions Taken
- Confirmed `internal/experiment/queries.sql.go` was tracked only because the SCIEDU-113 commit force-added it despite the existing `.gitignore` rule; removed it from the Git index with `git rm --cached` while preserving the local generated file.
- Moved status updates into the existing transaction boundary and added `UpdateStatus` to `MutationRepository`.
- For every non-ACTIVE to ACTIVE transition, the service now locks the Experiment, locks its participant users in the existing stable UUID order, checks the existing half-open schedule-overlap rule, and updates status only when no conflict exists.
- Kept ACTIVE-to-ACTIVE idempotent behavior and the defensive multiple-current check unchanged.
- Expanded table-driven service tests for conflicting/non-conflicting reactivation, no-participant activation, idempotent ACTIVE updates, invalid status, and the ARCHIVED-to-SCHEDULED-to-ACTIVE bypass.
- Added PostgreSQL API integration coverage for the exact reviewer scenario, boundary/disjoint/no-participant success cases, and concurrent overlapping reactivations.

### Attempted Methods
- Attempted to run the PostgreSQL integration suite against the known disposable `sciedu_pagevisit_test` database. PostgreSQL was not listening on localhost:5432, so DB-backed tests failed during connection setup before executing assertions.

### Verification
- `go test ./internal/experiment` passed.
- Integration-tagged Experiment tests compile successfully.
- `go test ./...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `git diff --check` passed.
- Real PostgreSQL integration execution remains unavailable because localhost:5432 refused the connection.

### Issues & Blockers
- No implementation blocker remains.
- PostgreSQL concurrency/integration cases require rerunning when the disposable database is available.
- The ignored generated Experiment query file remains available locally; CI, Docker builds, and repository workflows all run generation before building.

### Next Steps
- Start the disposable PostgreSQL service and rerun `go test -tags integration ./internal/experiment -count=1 -v` before finalizing the review response.
