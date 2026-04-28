package question

import (
	"context"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Repository struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

func NewRepository(db *pgxpool.Pool, logger *zap.Logger) *Repository {
	return &Repository{db: db, logger: logger}
}

func (r *Repository) List(ctx context.Context) ([]Question, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, type, content, created_at, updated_at
		FROM questions
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, r.logger, "list questions")
	}
	defer rows.Close()

	questions, err := pgx.CollectRows(rows, pgx.RowToStructByName[Question])
	if err != nil {
		return nil, databaseutil.WrapDBError(err, r.logger, "collect questions")
	}
	return r.attachOptions(ctx, questions)
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Question, error) {
	var q Question
	err := r.db.QueryRow(ctx, `
		SELECT id, type, content, created_at, updated_at
		FROM questions
		WHERE id = $1`, id).Scan(&q.ID, &q.Type, &q.Content, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		return Question{}, databaseutil.WrapDBErrorWithKeyValue(err, "questions", "id", id.String(), r.logger, "get question")
	}
	questions, err := r.attachOptions(ctx, []Question{q})
	if err != nil {
		return Question{}, err
	}
	return questions[0], nil
}

func (r *Repository) Create(ctx context.Context, req UpsertRequest) (Question, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Question{}, databaseutil.WrapDBError(err, r.logger, "begin create question")
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var q Question
	err = tx.QueryRow(ctx, `
		INSERT INTO questions (type, content)
		VALUES ($1, $2)
		RETURNING id, type, content, created_at, updated_at`, req.Type, req.Content).
		Scan(&q.ID, &q.Type, &q.Content, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		return Question{}, databaseutil.WrapDBError(err, r.logger, "create question")
	}
	q.Options, err = r.insertOptions(ctx, tx, q.ID, req.Options)
	if err != nil {
		return Question{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Question{}, databaseutil.WrapDBError(err, r.logger, "commit create question")
	}
	return q, nil
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, req UpsertRequest) (Question, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Question{}, databaseutil.WrapDBError(err, r.logger, "begin update question")
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var q Question
	err = tx.QueryRow(ctx, `
		UPDATE questions
		SET type = $2, content = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING id, type, content, created_at, updated_at`, id, req.Type, req.Content).
		Scan(&q.ID, &q.Type, &q.Content, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		return Question{}, databaseutil.WrapDBErrorWithKeyValue(err, "questions", "id", id.String(), r.logger, "update question")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM options WHERE question_id = $1`, id); err != nil {
		return Question{}, databaseutil.WrapDBError(err, r.logger, "replace question options")
	}
	q.Options, err = r.insertOptions(ctx, tx, q.ID, req.Options)
	if err != nil {
		return Question{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Question{}, databaseutil.WrapDBError(err, r.logger, "commit update question")
	}
	return q, nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM questions WHERE id = $1`, id)
	if err != nil {
		return databaseutil.WrapDBError(err, r.logger, "delete question")
	}
	if tag.RowsAffected() == 0 {
		return databaseutil.WrapDBErrorWithKeyValue(pgx.ErrNoRows, "questions", "id", id.String(), r.logger, "delete question")
	}
	return nil
}

func (r *Repository) ListAnswers(ctx context.Context, questionID uuid.UUID) ([]Answer, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, question_id, selected_option_id, text_answer, created_at, updated_at
		FROM answers
		WHERE question_id = $1
		ORDER BY created_at ASC`, questionID)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, r.logger, "list answers")
	}
	defer rows.Close()

	var answers []Answer
	for rows.Next() {
		var a Answer
		if err := rows.Scan(&a.ID, &a.QuestionID, &a.SelectedOptionID, &a.TextAnswer, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, databaseutil.WrapDBError(err, r.logger, "scan answers")
		}
		answers = append(answers, a)
	}
	return answers, databaseutil.WrapDBError(rows.Err(), r.logger, "iterate answers")
}

func (r *Repository) CreateAnswer(ctx context.Context, questionID uuid.UUID, req SubmitAnswerRequest) (Answer, error) {
	var a Answer
	err := r.db.QueryRow(ctx, `
		INSERT INTO answers (question_id, selected_option_id, text_answer)
		VALUES ($1, $2, $3)
		RETURNING id, question_id, selected_option_id, text_answer, created_at, updated_at`,
		questionID, req.SelectedOptionID, req.TextAnswer).
		Scan(&a.ID, &a.QuestionID, &a.SelectedOptionID, &a.TextAnswer, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return Answer{}, databaseutil.WrapDBError(err, r.logger, "create answer")
	}
	return a, nil
}

func (r *Repository) attachOptions(ctx context.Context, questions []Question) ([]Question, error) {
	for i := range questions {
		rows, err := r.db.Query(ctx, `
			SELECT id, label, content, created_at, updated_at
			FROM options
			WHERE question_id = $1
			ORDER BY label ASC`, questions[i].ID)
		if err != nil {
			return nil, databaseutil.WrapDBError(err, r.logger, "list question options")
		}
		var options []Option
		for rows.Next() {
			var o Option
			if err := rows.Scan(&o.ID, &o.Label, &o.Content, &o.CreatedAt, &o.UpdatedAt); err != nil {
				rows.Close()
				return nil, databaseutil.WrapDBError(err, r.logger, "scan question options")
			}
			options = append(options, o)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, databaseutil.WrapDBError(err, r.logger, "iterate question options")
		}
		rows.Close()
		questions[i].Options = options
	}
	return questions, nil
}

func (r *Repository) insertOptions(ctx context.Context, tx pgx.Tx, questionID uuid.UUID, inputs []OptionInput) ([]Option, error) {
	options := make([]Option, 0, len(inputs))
	for _, input := range inputs {
		var o Option
		err := tx.QueryRow(ctx, `
			INSERT INTO options (question_id, label, content)
			VALUES ($1, $2, $3)
			RETURNING id, label, content, created_at, updated_at`,
			questionID, input.Label, input.Content).
			Scan(&o.ID, &o.Label, &o.Content, &o.CreatedAt, &o.UpdatedAt)
		if err != nil {
			return nil, databaseutil.WrapDBError(err, r.logger, "create option")
		}
		options = append(options, o)
	}
	return options, nil
}
