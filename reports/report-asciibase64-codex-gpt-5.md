## [2026-05-18 20:27] Task Record

### Task Description
- Review and correct the proposed auth/user/question/chat ERD according to the prior auth table design recommendations.

### Actions Taken
- Verified Git status, Git username, `.gitignore`, existing reports, and relevant auth/API documentation.
- Read `AuthFlow.md`, `AuthTableDesign.md`, `docs/API.md`, `docs/LLM_ERD.md`, and `docs/user_question.md` context.
- Produced a corrected Mermaid ERD in the chat response.

### Attempted Methods
- Compared the submitted ERD against the earlier auth design review.
- Reflected the main design corrections in the ERD: composite family/user consistency, one active refresh token per family, stronger refresh token format wording, verified OAuth email semantics, and PostgreSQL `inet` fields.

### Issues & Blockers
- No repository schema or migration files were modified because the user asked for an ERD correction, not implementation.
- Existing docs still have auth API path inconsistencies that should be resolved before implementation.

### Next Steps
- If the corrected ERD is accepted, apply it to the appropriate design document and then implement migrations, sqlc queries, service logic, and table-driven tests.

## [2026-05-18 20:35] Task Record

### Task Description
- Rebase the corrected ERD on the user's current user/question/chat ERD and fix the `answers` semantics so it represents user-submitted answers, not canonical correct answers.

### Actions Taken
- Rechecked Git status and Git username.
- Reviewed `docs/user_question.md` to compare the existing ERD shape.
- Produced an updated Mermaid ERD in the chat response with `answers.user_id`, `chats.user_id`, and answer relationships representing submissions.

### Attempted Methods
- Preserved the auth/session table design from the previous review while updating the question-answer semantics.
- Changed the `questions/options -> answers` relationship wording from correctness semantics to submission/selection semantics.

### Issues & Blockers
- No schema or documentation file was updated in this step beyond the required report record.
- Whether to enforce `UNIQUE(user_id, question_id)` depends on whether the product allows multiple attempts per question.

### Next Steps
- If this ERD is accepted, update the relevant design document and decide whether answers are append-only attempts or one current answer per user/question.

## [2026-05-18 20:35] Task Record

### Task Description
- Create database migration files for the auth/user/answers/chat ERD and add comments for meaningful columns.

### Actions Taken
- Added `internal/database/migrations/7_auth_users_answers.up.sql`.
- Added `internal/database/migrations/7_auth_users_answers.down.sql`.
- The up migration creates `users`, `oauth_accounts`, `refresh_token_families`, `refresh_tokens`, `oauth_login_states`, and `answers`.
- The up migration alters `chats` with `user_id`, `title`, and `updated_at`, and adds a composite unique constraint on `options(id, question_id)` so submitted choice answers can ensure the selected option belongs to the same question.
- Added table/column comments for meaningful business columns while omitting overly simple timestamp fields.

### Attempted Methods
- Preserved existing `questions`, `options`, and `chats` tables instead of recreating them.
- Kept `chats.user_id` nullable to avoid breaking migration on legacy chat rows that predate user ownership.
- Did not add `UNIQUE(user_id, question_id)` to `answers` because the product rule for repeated attempts is not decided.
- Checked the new migration files for whitespace issues with `git diff --no-index --check`.

### Issues & Blockers
- Docker is not running in this environment, so the migration was not applied against local PostgreSQL.
- `psql` is not installed locally.
- A full `git diff --check` still reports an existing trailing whitespace issue in `AuthFlow.md`, unrelated to the new migration files.

### Next Steps
- Start local PostgreSQL and run the migrations up/down before merging.
- Decide whether `answers` should allow multiple attempts or enforce one answer per user/question.
- Once chat rows are backfilled or known empty in all environments, consider a follow-up migration to make `chats.user_id` `NOT NULL`.

## [2026-05-18 20:54] Task Record

### Task Description
- Modify `docs/auth_ERD.md` and `internal/database/migrations/7_auth_users_answers.up.sql` according to the published SciEdu API Users tag, while respecting `AuthFlow.md` and `AuthTableDesign.md`.

### Actions Taken
- Fetched the published OpenAPI document from `https://nycu-sdc.github.io/sciedu-api/tsp-output/schema/openapi.1.0.0.yaml`.
- Updated `docs/auth_ERD.md` to include only users/auth tables required by Users/Auth flows.
- Rewrote `internal/database/migrations/7_auth_users_answers.up.sql` to remove unrelated `answers`, `questions/options`, and `chats` changes.
- Rewrote `internal/database/migrations/7_auth_users_answers.down.sql` to match the reduced up migration.
- Replaced `COMMENT ON TABLE` / `COMMENT ON COLUMN` statements with inline SQL comments.

### Attempted Methods
- Initial sandboxed curl failed because DNS/network access was restricted; reran with approved network access.
- Used the API reference to confirm `POST /api/users` and `GET /api/users/me` only need `id`, `email`, `name`, optional `avatarUrl`, and `roles` on the user table.
- Kept auth support tables from `AuthFlow.md` and `AuthTableDesign.md`: `oauth_accounts`, `refresh_token_families`, `refresh_tokens`, and `oauth_login_states`.

### Issues & Blockers
- Docker is still not running, so the migration was not applied against PostgreSQL.
- `psql` is not installed locally.
- The migration filename still contains `answers` because the user requested edits to that exact file; its contents no longer create answers-related objects.

### Next Steps
- Run migration up/down against local PostgreSQL once Docker is available.
- Consider renaming migration files in a follow-up if the team wants the filename to match the reduced auth/users scope.

## [2026-05-18 21:03] Task Record

### Task Description
- Rename the auth migration files to match their current auth/users scope and update `docs/auth_ERD.md` to fit the current design.

### Actions Taken
- Renamed `internal/database/migrations/7_auth_users_answers.up.sql` to `internal/database/migrations/7_auth_users.up.sql`.
- Renamed `internal/database/migrations/7_auth_users_answers.down.sql` to `internal/database/migrations/7_auth_users.down.sql`.
- Updated `docs/auth_ERD.md` so `users.roles` is represented as a PostgreSQL enum array aligned with the migration.
- Rechecked that the auth migration and ERD no longer reference answers, questions, options, chats, or `COMMENT ON`.

### Attempted Methods
- Used direct file renames because the migration files were untracked and the task was specifically to rename them.
- Ran targeted whitespace checks on the renamed migration files and `docs/auth_ERD.md`.

### Issues & Blockers
- Docker/PostgreSQL verification remains blocked because Docker is not running in this environment.

### Next Steps
- Run migration up/down against local PostgreSQL when Docker is available.
- Stage the renamed migration files with the deleted old names if they had been staged elsewhere.

## [2026-05-18 21:17] Task Record

### Task Description
- Delete `AuthFlow.md` and `AuthTableDesign.md`, and remove them from remote GitHub.

### Actions Taken
- Deleted `AuthFlow.md`.
- Deleted `AuthTableDesign.md`.
- Checked Git status after deletion.
- Searched for remaining references to the deleted filenames.

### Attempted Methods
- Used file deletion patches so Git records the files as deleted.
- Did not push to GitHub because workspace instructions strictly prohibit remote push operations by agents.

### Issues & Blockers
- Remote GitHub removal requires a commit and push, but agent remote push operations are prohibited in this project.
- Remaining references to the deleted filenames exist only in this report file as historical task records.

### Next Steps
- Human developer should review, commit, and push the deletions to remove the files from remote GitHub.
