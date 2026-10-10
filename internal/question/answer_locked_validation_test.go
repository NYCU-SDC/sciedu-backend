package question

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// Only the lock and authoritative read are allowed. Any attempted write or
// scope lookup before validation fails this test (other pgx methods are unset).
type lockedValidationTx struct {
	pgx.Tx
	t                     *testing.T
	questionID            uuid.UUID
	currentType           string
	queries               []string
	committed, rolledBack bool
}

type lockedValidationDB struct {
	DBTX
	tx *lockedValidationTx
}

func (d lockedValidationDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

type validationRow func(...any) error

func (r validationRow) Scan(dest ...any) error { return r(dest...) }

func (tx *lockedValidationTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, sql)
	return validationRow(func(dest ...any) error {
		switch sql {
		case lockAnswerQuestion:
			require.Len(tx.t, tx.queries, 1)
			*dest[0].(*uuid.UUID) = tx.questionID
		case getQuestion:
			require.Equal(tx.t, []string{lockAnswerQuestion, getQuestion}, tx.queries)
			*dest[0].(*uuid.UUID) = tx.questionID
			*dest[1].(*string) = "changed question"
			*dest[2].(*string) = tx.currentType
			*dest[3].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
			*dest[4].(*pgtype.Timestamptz) = pgtype.Timestamptz{}
		default:
			tx.t.Fatalf("unexpected query after incompatible payload: %s", sql)
		}
		return nil
	})
}
func (tx *lockedValidationTx) Commit(context.Context) error   { tx.committed = true; return nil }
func (tx *lockedValidationTx) Rollback(context.Context) error { tx.rolledBack = true; return nil }

func TestLockedQuestionValidation(t *testing.T) {
	for _, tt := range []struct {
		name, currentType string
		choice            bool
	}{
		{"text payload after question became choice", "CHOICE", false},
		{"choice payload after question became text", "TEXT", true},
	} {
		for _, correct := range []bool{false, true} {
			name := "submission/"
			if correct {
				name = "correct-answer/"
			}
			t.Run(name+tt.name, func(t *testing.T) {
				tx := &lockedValidationTx{t: t, questionID: uuid.New(), currentType: tt.currentType}
				store := NewStore(lockedValidationDB{tx: tx})
				text, option := "text", uuid.New()
				if correct {
					arg := UpsertCorrectAnswerParams{QuestionID: tx.questionID, Type: "TEXT", ReferenceAnswer: nullableText(&text)}
					if tt.choice {
						arg.Type = "CHOICE"
						arg.ReferenceAnswer = pgtype.Text{}
						arg.SelectedOptionID = nullableUUID(&option)
					}
					_, err := store.UpsertCorrectAnswer(t.Context(), arg)
					require.ErrorIs(t, err, errInvalidCorrectAnswerPayload)
				} else {
					request := AnswerRequest{QuestionID: tx.questionID, UserID: uuid.New(), TextAnswer: &text}
					if tt.choice {
						request.TextAnswer = nil
						request.SelectedOptionID = &option
					}
					_, err := store.SubmitAnswer(t.Context(), AnswerSubmissionCommand{Answer: request, Context: SubmissionContext{GradingMode: SubmissionGradingModeAutomatic}})
					require.ErrorIs(t, err, errInvalidAnswerPayload)
				}
				require.Equal(t, []string{lockAnswerQuestion, getQuestion}, tx.queries)
				require.False(t, tx.committed)
				require.True(t, tx.rolledBack)
			})
		}
	}
}
