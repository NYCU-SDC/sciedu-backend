## [2026-07-21 00:00] Task Record

### Task Description
- Implement the `answer` (user submission) feature per `docs/answer_design.md` and `docs/answer_implementation_guide.md`.
- The user is new to Go and asked to follow the guide's steps.
- Step 0 (`make gen` / `make test` toolchain verification) was temporarily skipped because the local environment lacks Go (`go` command not found; only `sqlc` is installed). The user will complete it later in a GoLand IDE.
- The user asked to first complete the "commit 1" scope; they will create the branch and commit themselves, so no need for me to commit/push.

### Actions Taken
- Created `internal/database/migrations/11_answers.up.sql`: added the `answers` table (`id`, `question_id` FK→questions, `user_id` FK→users, `selected_option_id` nullable FK→options ON DELETE SET NULL, `text_answer` nullable, `created_at`, `updated_at`), matching the style of the existing `1_questions.up.sql` / `2_options.up.sql` (no extra alignment whitespace/comments).
- Created `internal/database/migrations/11_answers.down.sql`: `DROP TABLE IF EXISTS answers;`
- Modified `internal/question/schema.sql`: appended the same `answers` table definition after the existing `questions` and `options` definitions (for sqlc type inference only, not for creating the DB).
- Confirmed the `users` table is defined in `internal/auth/schema.sql` (not `internal/question/schema.sql`); `create_sqlc_full_schema.sh` merges every package's `schema.sql`, so `answers.user_id` referencing `users(id)` has no cross-file problem.

### Attempted Methods
- Tried to verify the Go/sqlc toolchain (Step 0): `which go`, `which sqlc`, `brew list` search → confirmed `sqlc` is installed via Homebrew, but `go` is not found at all (not a PATH issue, it is simply not installed). Confirmed with the user and decided to skip Step 0 by design, letting the user complete it later in the IDE environment.
- Confirmed the "commit 1" scope with the user: originally proposed Steps 1-3 (migration + schema.sql + queries.sql); the user chose to narrow it to Steps 1-2 (migration + schema.sql only), leaving queries.sql for the next commit.

### Issues & Blockers
- The local environment has no Go toolchain, so `make gen` / `make test` cannot be run to verify this change (it is pure SQL and in theory needs no Go to write, but the type output has not actually been verified via `sqlc generate`).
- No git operations performed yet (no add/commit); per the user's instruction the user will create the branch and commit themselves.

### Next Steps
- For the next AI/engineer taking over: first do Step 3 (`internal/question/answer_queries.sql`, two queries: `CreateAnswer`, `ListAnswersByQuestionForUser`), still pure SQL, no Go toolchain needed.
- Later, in an environment with Go/sqlc (the user mentioned they will use a GoLand IDE), run Step 0 verification, then continue with Step 4 (`make gen`), Step 5 (service), Step 6 (handler), Step 7 (main.go wiring), Step 8 (tests), Step 9 (lint/test/manual verification).
- Note that the nullable columns (`selected_option_id`, `text_answer`) will become `pgtype.UUID` / `pgtype.Text` after `make gen`; the service layer must convert them itself, do not edit the generated files.
- The working directory currently has other unrelated uncommitted files (changes to `CLAUDE.md`, `mermaid.md`, `mguide.md`, `docs/user_question.md`) that are not outputs of this task; when committing, be careful to add only the files relevant to this task.

## [2026-07-21 00:30] Task Record

### Task Description
- The user confirmed the branch name `feat/answer` (created by themselves, no git operations run by the AI), then asked to start the next commit's content.
- Per the earlier discussion, the next commit scope is Step 3: hand-written query SQL.

### Actions Taken
- Created `internal/question/answer_queries.sql`: two sqlc queries
  - `CreateAnswer :one` — insert into `answers`, returning all columns.
  - `ListAnswersByQuestionForUser :many` — filter by `question_id` + `user_id`, `ORDER BY created_at DESC`.
  The format follows the existing `option_queries.sql` (sqlc `-- name:` directives, column order consistent with the migration).

### Attempted Methods
- Reused the column order confirmed in the previous commit (`id, question_id, user_id, selected_option_id, text_answer, created_at, updated_at`), keeping it consistent with the `answers` definition in `internal/question/schema.sql` to avoid column mismatches when sqlc generates types.

### Issues & Blockers
- Same as before: the local machine still has no Go toolchain, so `sqlc generate` has not actually been run to verify these two queries' syntax/types; needs the user to verify after running Step 0 in the IDE environment.
- No git add/commit yet; the user will do it themselves.

### Next Steps
- After the user gets Step 0 (`make gen` / `make test`) working in their own IDE environment, they can proceed to Step 4 (`make gen` to generate `answer_queries.sql.go` and the `Answer` model), then continue with Step 5 (`answer_service.go`).
- Note for Step 5: the `AnswerService` constructor should inject `*QuestionService` to reuse `Get` / `ListOptionsByQuestion` for validation; do not write separate queries.

## [2026-07-21 01:00] Task Record

### Task Description
- The user has completed Step 4 (`make gen`, produced with sqlc v1.30.0, no version noise) and committed. Then asked to do the next commit scope: Step 5, the service layer.

### Actions Taken
- Modified `internal/question/errors.go`: added `errInvalidAnswerPayload` (the base error for the answer validation error family).
- Created `internal/question/answer_service.go`, containing:
  - `AnswerRequest` (service input DTO): `QuestionID`, `UserID uuid.UUID`, `SelectedOptionID *uuid.UUID`, `TextAnswer *string`.
  - `AnswerQuerier` interface (for test substitution): `CreateAnswer`, `ListAnswersByQuestionForUser`.
  - `AnswerService` + `NewAnswerService(querier, questionService *QuestionService, logger)`, holding `*QuestionService` to reuse queries for validation.
  - `Create`: first `questionService.Get` (a miss is turned into not found by WrapDBError → handler maps to 404), then `validatePayload`, finally `CreateAnswer` (wrapped by WrapDBError).
  - `ListByQuestionForUser`: calls the querier directly, wrapped by `WrapDBErrorWithKeyValue`.
  - `validatePayload`: splits into CHOICE / TEXT by `question.Type`; CHOICE requires `SelectedOptionID` and that the option belongs to this question (checked via `questionService.ListOptionsByQuestion`), and must not have text; TEXT requires a non-blank `TextAnswer` and must not have an option; an unknown type returns an error.
  - `optionBelongsToQuestion` helper: lists the question's options and compares IDs.
  - Local `nullableUUID(*uuid.UUID)` / `nullableText(*string)` helpers: convert pointers to `pgtype.UUID`/`pgtype.Text`, treating nil or blank as NULL. Modeled on the same-named helpers in `internal/auth/store.go` (but the auth versions are unexported, and its text version takes `string` rather than `*string`, so a separate one is written in this package).

### Attempted Methods
- Placed validation logic in the service layer (not the handler), reusing `QuestionService.Get` / `ListOptionsByQuestion`, without writing separate SQL, per section 5 of the design doc and the decision log.
- pgtype conversion follows the repo's existing conventions (`pgtype.UUID{Bytes: *id, Valid: true}`, `pgtype.Text{String, Valid}`); the Bytes field directly takes `uuid.UUID` (underlying `[16]byte`), the same as `auth/store.go:289`.
- **Validation decision (made on my own, open to discussion)**: treat an "empty / all-whitespace textAnswer" as "not provided". → A CHOICE question sending a blank textAnswer is not a violation (stored as NULL); a TEXT question sending a blank textAnswer counts as missing text (returns 400). Rationale: blank is not a valid answer, and it keeps the CHOICE/TEXT judgment symmetric.

### Verification
- Verified with the local `/Users/melodywu/sdk/go1.26.1/bin/go` (GoLand SDK):
  - `go build ./internal/question/` → success.
  - `go vet ./internal/question/` → no issues.
  - `go test ./internal/question/` → all 31 tests pass (existing tests not broken).

### Issues & Blockers
- No tests added for the answer service yet (Step 8); this commit only covers the service implementation; existing tests are still all green.
- No git add/commit yet; the user will do it themselves.

### Next Steps
- Next commit: Step 6, the handler layer (`answer_handler.go`), referencing `handler.go`; needs `import sciedu-backend/internal/auth` to get `auth.UserIDFromContext`.
- Then Step 7 wiring in `cmd/backend/main.go`, Step 8 tests (`answer_service_test.go` / `answer_handler_test.go`), Step 9 lint/manual API.
- The handler's problemWriter mapping must cover `errInvalidAnswerPayload` (→ 400).

## [2026-07-21 01:20] Task Record

### Task Description
- The user asked to align `answer_service.go`'s style with the existing conventions of `option_service.go` / `question_service.go`.

### Actions Taken
- Refactored `internal/question/answer_service.go`:
  - Split the original `validatePayload` (a method that mixed pure checks with DB checks) into a package-level pure function `validateAnswerPayload(questionType string, arg AnswerRequest) error` that only checks "presence / type correctness", mirroring the shape of `validateQuestionOptions` (no receiver, no DB, `%w:` messages, `default` handling for unknown types), placed at the bottom of the file with the other helpers.
  - Moved the "does the option belong to this question" DB check into `Create` (the `if question.Type == "CHOICE"` block), keeping DB orchestration in the service method body, consistent with `question_service.CreateWithOptions`'s layering. Dereferencing `*arg.SelectedOptionID` is safe because pure validation already guarantees CHOICE has an option.
  - Changed error messages from JSON field names (`selectedOptionId` / `textAnswer`) to conceptual wording (`selected option` / `text answer`), aligning with the wording in `validateQuestionOptions`.

### Verification
- `gofmt -l` no output; `go build` / `go vet` pass; `go test ./internal/question/` all pass.

### Next Steps
- Unchanged: the next commit is Step 6, the handler layer.

## [2026-07-21 01:40] Task Record

### Task Description
- The user asked to fix issue #1 raised in code review: `nullableText` hides a business rule (blank treated as unfilled) inside a type converter, and validation uses trim while storage does not, causing a discrepancy.

### Actions Taken
- Refactored `internal/question/answer_service.go` to consolidate the "blank = unfilled" business rule into a single entry point:
  - Added `normalizeText(value *string) *string`: trims leading/trailing whitespace, all-blank → nil (treated as not provided). This is the rule's only home.
  - `Create` adds one line `arg.TextAnswer = normalizeText(arg.TextAnswer)` after `Get` and before validation, so both validation and storage consume the "already normalized" value.
  - `validateAnswerPayload`'s `hasText` is simplified from `!= nil && TrimSpace != ""` to just `!= nil` (because the input is already normalized).
  - `nullableText` returns to a pure conversion: only `nil → NULL`, otherwise store the value, consistent with the pure helper in `auth/store.go`, no longer carrying trim/blank judgment.

### Attempted Methods
- Key trade-off: normalize must be added, otherwise if `nullableText` is only made pure without normalization, a CHOICE question sending an empty string would be stored as `text_answer=""` (not NULL), causing a regression. normalize converts blank to nil, so `nullableText(nil)` correctly stores NULL.
- Decided to "store the trimmed value" (`"  hi  "` → store `"hi"`): eliminates the original "validate trimmed, store untrimmed" inconsistency. Leading/trailing whitespace in free-text survey answers carries no meaning, so trimming is a reasonable normalization. If we don't want to alter stored content, we could revert to not trimming, but validation would need to be adjusted accordingly.

### Verification
- `gofmt -l` no output; `go build` / `go vet` pass; `go test ./internal/question/` all 44 tests pass.

### Next Steps
- Unchanged: the next commit is Step 6, the handler layer.

## [2026-07-21 02:00] Task Record

### Task Description
- The user questioned whether the "blank treated as unfilled" rule (and the `normalizeText` created for it) is necessary at this stage; after discussion, chose "Option A: simplest".

### Actions Taken
- Removed `normalizeText` from `internal/question/answer_service.go` and its call inside `Create`.
- Also removed the now-unused `strings` import.
- Result: `hasText := arg.TextAnswer != nil` (pure nil check); `nullableText` stays a pure conversion (`nil → NULL`, otherwise store the value). No more blank/trim business judgment anywhere.

### Attempted Methods / Decision rationale
- Section 5 of the design doc only requires "TEXT must have textAnswer, CHOICE must not"; it does not specify blank handling. "Blank = unfilled" was a rule added on my own earlier. The frontend field contract is also not yet aligned (design section 8), so defending against it early is guesswork → YAGNI, not doing it at this stage.
- Known trade-off (accepted): if the frontend sends `textAnswer: ""` on CHOICE it returns 400; sending `""` on TEXT stores an empty string. If the frontend really will send blanks in the future, just add normalize back then.

### Verification
- `gofmt -l` no output; `go build` / `go vet` pass; `go test ./internal/question/` all 44 tests pass.

### Next Steps
- Unchanged: the next commit is Step 6, the handler layer.

## [2026-07-21 02:40] Task Record

### Task Description
- Review Steps 6-8 completed by another session (handler / tests / wiring / auth helper), and fill in test coverage gaps.

### Review Findings
- **[Important] index and working tree inconsistent**: `answer_service.go` is `AM`. The staged version is pre-Option-A (validateAnswerPayload/nullableText still contain `strings.TrimSpace` blank handling); the working tree is Option A (pure `!= nil`). The public API is identical, so both handler/tests pass for both versions. → Remind the user to re-`git add answer_service.go` before committing, otherwise the discarded version would be shipped.
- **Test gap**: the two "mutually exclusive" branches of `validateAnswerPayload` (CHOICE must not have text, TEXT must not have option) originally had no tests, because existing cases would hit the "required" branch first and return.
- `auth/middleware.go` adds `ContextWithUserID`: uses the same `userIDContextKey` + `uuid.UUID`, consistent with the write on line 59 of the middleware, so the test faithfully reflects production. Reasonably let through.
- Comment-culture investigation: the `question` package has historically had 0 comments; chat (16) is mostly low-value "what" labels; auth (9) is high-value "why". The newly added `ContextWithUserID` doc and the test's `fakeAnswerQuerier` block comment are the auth-style "why", so they can stay; if we want to strictly align with this package's zero comments, that test block is the most removable (not yet touched).

### Actions Taken
- `internal/question/answer_handler_test.go`: added two cases to `TestAnswerHandlerSubmit_TableDriven`:
  - `choice with text returns 400` (CHOICE given both a valid option + text).
  - `text with option returns 400` (TEXT given both text + option).
  Both assert a 400 and `wantCreateCalls: 0`; because the option really belongs to the question, without the mutual-exclusion check it would return 201, so this test truly locks down those two branches.

### Verification
- `gofmt -l` no output; `go vet` passes; `go test ./internal/question/` all 46 tests pass (including the 2 new cases).

### Next Steps
- Pending the user's decision: whether to remove the test file's `fakeAnswerQuerier` block comment, and whether to align answer_handler's import grouping with `chat/handler.go` (local last).
- Reminder: `git add internal/question/answer_service.go` before committing to include the Option A version.

## [2026-07-21 02:55] Task Record

### Actions Taken
- Wrapped up per the user's decisions:
  - Removed the `fakeAnswerQuerier` block comment from `answer_handler_test.go` (aligning with the `question` package's zero-comment convention); kept the `ContextWithUserID` doc comment in `auth/middleware.go` (an exported function, Go convention).
  - Aligned the import grouping of `answer_handler.go` and `answer_handler_test.go` with `chat/handler.go`: the local import (`sciedu-backend/internal/auth`) moved after third-party and into its own final group.
- Confirmed index state: `answer_service.go` is now the Option A version (`git show :` shows no TrimSpace); the user has re-added it, so index/working are consistent.

### Verification
- `gofmt -l` no output; `go build ./...` succeeds; `go test ./internal/question/` all 46 tests pass.

### Git status (for handover reference)
- Committed: `c185a49 feat: add answers table migration and queries` (Steps 1-3); `7bff6b5 chore: generate sqlc code for answers` (Step 4).
- Pending commit (Steps 5-8): `cmd/backend/main.go`, `internal/auth/middleware.go`, `internal/question/{answer_handler.go, answer_handler_test.go, answer_service.go, errors.go}`. Among these, `answer_handler.go` / `answer_handler_test.go` have unstaged edits from this round; re-`git add` before committing.

## [2026-07-21 03:10] Task Record — Answer feature wrap-up

### Task Description
- Final polish and close-out of the `answer` feature.

### Actions Taken
- Trimmed the `ContextWithUserID` doc comment in `internal/auth/middleware.go` to a single line (`// ContextWithUserID returns a copy of ctx carrying userID, the inverse of UserIDFromContext.`), matching the terseness of its sibling exported functions.
- Fixed this report's chronological order: the `02:00` (Option A) entry had been appended at the end; moved it back to sit between `01:40` and `02:40`.

### Verification
- `gofmt -l` no output; `go build ./...` succeeds; `go test ./internal/question/ ./internal/auth/` → 127 tests pass.

### Overall status (Steps 1-8 complete)
- **Step 1-2** (migration + schema.sql), **Step 3** (queries), **Step 4** (sqlc codegen): committed (`c185a49`, `7bff6b5`).
- **Step 5** (service: `AnswerService`, type-aware validation, pgtype conversion), **Step 6** (handler: POST/GET `/api/questions/{id}/answers`), **Step 7** (main.go wiring), **Step 8** (table-driven handler tests incl. the two mutual-exclusion cases): implemented, not yet committed.
- Design decisions of note: validation lives in the service (reuses `QuestionService.Get`/`ListOptionsByQuestion`, no new SQL); "blank = unfilled" normalization was dropped (Option A / YAGNI); GET returns only the current user's answers, newest first.

### Remaining
- **Commit Steps 5-8.** Before committing, re-`git add internal/auth/middleware.go` (has the unstaged one-line comment trim); the other pending files are already staged and correct (`answer_service.go` confirmed as the Option A version in the index).
  - Suggested message: `feat: add answer submission and listing API`.
  - Optionally include `docs/user_question.md` (ERD `user_id` fix) if wanted; exclude unrelated `CLAUDE.md` / `mermaid.md` / `mguide.md`.
- **Step 9**: run `make lint` (golangci-lint) and a manual API smoke test (CHOICE → 201 + GET; TEXT → 201; repeat submit → array newest-first; unknown question → 404; type-mismatched body → 400). These need the full local stack (Docker Postgres) which this environment could not run; the user will do it in their IDE.
- Then open the PR per `AGENTS.md`, linking `docs/answer_design.md`.

## [2026-07-21 04:30] Task Record — Merge answer handler into question handler

### Task Description
- Merge `internal/question/answer_handler.go` into `internal/question/handler.go`, and `answer_handler_test.go` into `handler_test.go`.

### Design decisions (agreed with user before implementing)
- **Chose full fold, not colocation.** `AnswerHandler` was dissolved into `Handler` rather than kept as a second struct in the same file — otherwise the split survives under a new filename. `Handler` now holds both `questionService` and `answerService`; one `problemWriter` maps both `errInvalidQuestionPayload` and `errInvalidAnswerPayload`; one `RegisterRoutes` covers all 7 routes.
- **`buildAnswerResponse` stays a free function.** `buildQuestionResponse` is a method only because it needs `h.questionService`; `buildAnswerResponse` needs no receiver, so symmetry was not worth an unused one.
- **`fakeAnswerQuerier` deleted.** Its two methods (`CreateAnswer`, `ListAnswersByQuestionForUser`) and `createAnswerCalls` were folded directly into `fakeQuerier`, which now satisfies `AnswerQuerier`. One fake, one `newTestMux`; the embedded-fake + interface-param alternative was more indirection for no gain.

### Actions Taken
- `internal/question/handler.go`: added `answerService` field, `submitAnswerRequest`/`answerResponse` types, `SubmitAnswer`/`ListAnswers` methods, `buildAnswerResponse`; `NewHandler` gained an `answerService *AnswerService` second parameter; imports gained `time` and `sciedu-backend/internal/auth`.
- **Renamed methods on merge**: `AnswerHandler.Submit` → `Handler.SubmitAnswer`, `AnswerHandler.List` → `Handler.ListAnswers`. Forced — `Handler.List` (list questions) already existed. Route patterns are unchanged.
- Answer handlers now use `h.parseID` instead of calling `handlerutil.ParseUUID` directly, matching the question handlers.
- `internal/question/answer_handler.go` and `answer_handler_test.go`: deleted.
- `internal/question/handler_test.go`: absorbed the three answer tests (renamed `TestAnswerHandlerSubmit_TableDriven` → `TestHandlerSubmitAnswer_TableDriven`, `TestAnswerHandlerSubmit_PassesUserAndConversions` → `TestHandlerSubmitAnswer_PassesUserAndConversions`, `TestAnswerHandlerList_TableDriven` → `TestHandlerListAnswers_TableDriven`), plus `choiceQuestion`/`textQuestion` helpers; imports gained `pgtype` and `auth`.
- `cmd/backend/main.go`: `answerService` construction moved above `questionHandler`; `NewAnswerHandler` call and the `answerHandler.RegisterRoutes` line removed.

### Verification — ALL GREEN (levels 1 & 2)
Ran with `export PATH="$HOME/sdk/go1.26.1/bin:$HOME/go/bin:$PATH"` (go1.26.1 darwin/arm64):
- `gofmt -l internal/ cmd/` → no output (clean)
- `go build ./...` → exit 0
- `go vet ./...` → exit 0
- `go test ./internal/question/ -count=1 -v` → PASS, including all merged answer tests: `TestHandlerSubmitAnswer_TableDriven` (9 subtests), `TestHandlerSubmitAnswer_PassesUserAndConversions`, `TestHandlerListAnswers_TableDriven` (3 subtests)
- `go test ./... -count=1` → all packages ok
- `go test ./... -race -count=1` → all packages ok

### ⚠️ Correction to my own first attempt in this session — read this
I initially wrote in this report that **"the Go toolchain is not installed in this environment"** and asked the user to run the tests themselves. **That was wrong**, and it is the exact failure mode `docs/answer_implementation_guide.md` Step 0 and 常見卡點 already warn about — a previous agent made the identical mistake, which is why the warning exists.

Why I still got it wrong despite the doc existing:
- I checked `which go`, `/usr/local/go/bin`, `/opt/homebrew/bin/go*`, and `~/go/bin` — but **never `~/sdk`**, the GoLand-managed SDK directory the guide flags as ★最常被漏掉.
- My `find / -maxdepth 4 -name go` was **too shallow**: the real path `/Users/melodywu/sdk/go1.26.1/bin/go` is at depth 6, so the search returned empty and I read that as confirmation.
- Root cause: I never opened `docs/answer_implementation_guide.md` before declaring the environment broken. The user had to point me at it. **AGENTS.md says to check `docs/` first — do that before concluding any tool is missing.**

**For the next agent: `go` IS installed. Always prefix with `export PATH="$HOME/sdk/go1.26.1/bin:$HOME/go/bin:$PATH"`. Never claim "cannot verify" without first working through Step 0 of the implementation guide.**

### Next Steps
- Levels 1 & 2 are green, so this refactor is verified as far as unit tests reach. Level 4 (Docker/psql) is unaffected by this change — it is a pure handler-merge refactor with no SQL, schema, or route-pattern changes.
- `make lint` still not runnable (golangci-lint not installed; `brew install golangci-lint`). `go vet` + `gofmt` used as the substitute per the guide.
- Both handlers registered under `protectedMiddlewareSet` before the merge, so auth behavior is unchanged; no middleware divergence to reconcile.

## [2026-07-21 05:10] Task Record — Validate textAnswer length per API spec

### Task Description
- Code review comment on `submitAnswerRequest`: validate the length of `TextAnswer` per the API spec. Scoped to length only, per the user.

### Actions Taken
- `internal/question/handler.go:54`: added `validate:"omitempty,min=1,max=2000"` to `submitAnswerRequest.TextAnswer`, per `docs/question.tsp:107-115` (`textAnswer?` with `@minLength(1)` / `@maxLength(2000)`). No logic change — `ParseAndValidateRequestBody` already runs the validator.
- The review comment cites `internal/question/answer_handler.go`, which no longer exists; the type moved into `handler.go` in `965245c`.

### Notes
- `omitempty` on a non-nil `*string` does not skip validation, so an absent field passes but `"textAnswer": ""` is rejected — the intended reading of `@minLength(1)`. Confirmed with a throwaway test rather than assumed.
- Commit message used: `fix: validate text answer length on answer submission`.

### Investigation: mutual-exclusion logic (user asked where it lives)
- Already implemented in `answer_service.go:103-127` (`validateAnswerPayload`): CHOICE requires an option and forbids text, TEXT the reverse, unknown type errors. `answer_service.go:53-61` also verifies the option belongs to that question.
- Correctly in the service, not the handler — it needs `questionService.Get()` for the question type first.

### Verification
- `go build ./...` → exit 0; `go test ./internal/question/` → 46 passed.

### Next Steps
- **Spec gap (worth a separate ticket).** `SubmitAnswerRequest` in `docs/question.tsp` declares both fields as plain optionals; the mutual-exclusion rule lives only in Go, so frontend can only discover it via a 400. TypeSpec has no oneOf constraint, but it belongs in `@doc` on both fields. User declined for now; offer left open.
- Length is checked only at the handler, matching `createUpdateQuestionRequest`'s existing pattern — pushing it into the service would be a package-wide change, not a rider on this PR.
