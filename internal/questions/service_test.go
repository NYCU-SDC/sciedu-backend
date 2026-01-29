package questions_test

import (
	"context"
	logutil "sciedu-backend/internal/error"
	"sciedu-backend/internal/questions"
	"sciedu-backend/internal/questions/mocks"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestService_CreateQuestion(t *testing.T) {
	type arg struct {
		Type    string
		Content string
		Options []questions.OptionRequest
	}

	tests := []struct {
		name       string
		arg        arg
		setupMock  func(m *mocks.Querier)
		wantResult questions.BoundQuestion
	}{
		{
			name: "Should create a CHOICE question",
			arg: arg{
				Type:    "CHOICE",
				Content: "This is a sample CHOICE question",
				Options: []questions.OptionRequest{
					{Label: "A", Content: "Option A"},
					{Label: "B", Content: "Option B"},
					{Label: "C", Content: "Option C"},
					{Label: "D", Content: "Option D"},
				},
			},
			setupMock: func(m *mocks.Querier) {
				m.On("CreateQuestion", mock.Anything, questions.CreateQuestionParams{
					Type:    "CHOICE",
					Content: "This is a sample CHOICE question",
				}).Return(questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "CHOICE",
					Content: "This is a sample CHOICE question",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "A",
					Content:    "Option A",
				}).Return(questions.Option{
					ID:         uuid.MustParse("9a6ba605-fa30-42c1-8ffd-401976ddbd43"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "A",
					Content:    "Option A",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "B",
					Content:    "Option B",
				}).Return(questions.Option{
					ID:         uuid.MustParse("26e846ad-e36c-43b7-91ef-647130da9880"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "B",
					Content:    "Option B",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "C",
					Content:    "Option C",
				}).Return(questions.Option{
					ID:         uuid.MustParse("4a66e144-1d9b-4837-b525-608ea89d1490"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "C",
					Content:    "Option C",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "D",
					Content:    "Option D",
				}).Return(questions.Option{
					ID:         uuid.MustParse("eb040b25-8b60-43cc-b82c-331332df0598"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "D",
					Content:    "Option D",
				}, nil)
			},
			wantResult: questions.BoundQuestion{
				Question: questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "CHOICE",
					Content: "This is a sample CHOICE question",
				},
				Options: []questions.Option{
					{
						ID:         uuid.MustParse("9a6ba605-fa30-42c1-8ffd-401976ddbd43"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "A",
						Content:    "Option A",
					},
					{
						ID:         uuid.MustParse("26e846ad-e36c-43b7-91ef-647130da9880"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "B",
						Content:    "Option B",
					},
					{
						ID:         uuid.MustParse("4a66e144-1d9b-4837-b525-608ea89d1490"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "C",
						Content:    "Option C",
					},
					{
						ID:         uuid.MustParse("eb040b25-8b60-43cc-b82c-331332df0598"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "D",
						Content:    "Option D",
					},
				},
			},
		},
		{
			name: "Should create a TEXT question",
			arg: arg{
				Type:    "TEXT",
				Content: "This is a sample TEXT question",
				Options: nil,
			},
			setupMock: func(m *mocks.Querier) {
				m.On("CreateQuestion", mock.Anything, questions.CreateQuestionParams{
					Type:    "TEXT",
					Content: "This is a sample TEXT question",
				}).Return(questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "TEXT",
					Content: "This is a sample TEXT question",
				}, nil).Once()
			},
			wantResult: questions.BoundQuestion{
				Question: questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "TEXT",
					Content: "This is a sample TEXT question",
				},
				Options: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := mocks.NewQuerier(t)
			tt.setupMock(querier)

			logger, err := logutil.InitLogger()
			assert.NoError(t, err, "failed to initialize logger")

			service := questions.NewServiceWithQuerier(logger, querier)

			ctx := context.Background()
			actualResp, err := service.CreateQuestion(ctx, questions.QuestionRequest{
				Type:    tt.arg.Type,
				Content: tt.arg.Content,
				Options: tt.arg.Options,
			})
			if err != nil {
				logger.Error("failed to create question", zap.Error(err))
			}

			assert.Equal(t, tt.wantResult, actualResp)
		})
	}
}

func TestHandler_UpdateQuestion(t *testing.T) {
	type arg struct {
		Type    string
		Content string
		Options []questions.OptionRequest
	}

	tests := []struct {
		name       string
		arg        arg
		setupMock  func(m *mocks.Querier)
		wantResult questions.BoundQuestion
	}{
		{
			name: "Should update the CHOICE question",
			arg: arg{
				Type:    "CHOICE",
				Content: "This is a sample updated CHOICE question",
				Options: []questions.OptionRequest{
					{Label: "A", Content: "Updated option A"},
					{Label: "B", Content: "Updated option B"},
					{Label: "C", Content: "Updated option C"},
					{Label: "D", Content: "Updated option D"},
				},
			},
			setupMock: func(m *mocks.Querier) {
				m.On("UpdateQuestion", mock.Anything, questions.UpdateQuestionParams{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "CHOICE",
					Content: "This is a sample updated CHOICE question",
				}).Return(questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "CHOICE",
					Content: "This is a sample updated CHOICE question",
				}, nil)

				m.On("DeleteCorrespondingOption", mock.Anything,
					uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003")).Return(nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "A",
					Content:    "Updated option A",
				}).Return(questions.Option{
					ID:         uuid.MustParse("9a6ba605-fa30-42c1-8ffd-401976ddbd43"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "A",
					Content:    "Updated option A",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "B",
					Content:    "Updated option B",
				}).Return(questions.Option{
					ID:         uuid.MustParse("26e846ad-e36c-43b7-91ef-647130da9880"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "B",
					Content:    "Updated option B",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "C",
					Content:    "Updated option C",
				}).Return(questions.Option{
					ID:         uuid.MustParse("4a66e144-1d9b-4837-b525-608ea89d1490"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "C",
					Content:    "Updated option C",
				}, nil)

				m.On("CreateCorrespondOption", mock.Anything, questions.CreateCorrespondOptionParams{
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "D",
					Content:    "Updated option D",
				}).Return(questions.Option{
					ID:         uuid.MustParse("eb040b25-8b60-43cc-b82c-331332df0598"),
					QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Label:      "D",
					Content:    "Updated option D",
				}, nil)
			},
			wantResult: questions.BoundQuestion{
				Question: questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "CHOICE",
					Content: "This is a sample updated CHOICE question",
				},
				Options: []questions.Option{
					{
						ID:         uuid.MustParse("9a6ba605-fa30-42c1-8ffd-401976ddbd43"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "A",
						Content:    "Updated option A",
					},
					{
						ID:         uuid.MustParse("26e846ad-e36c-43b7-91ef-647130da9880"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "B",
						Content:    "Updated option B",
					},
					{
						ID:         uuid.MustParse("4a66e144-1d9b-4837-b525-608ea89d1490"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "C",
						Content:    "Updated option C",
					},
					{
						ID:         uuid.MustParse("eb040b25-8b60-43cc-b82c-331332df0598"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "D",
						Content:    "Updated option D",
					},
				},
			},
		},
		{
			name: "Should update the TEXT question",
			arg: arg{
				Type:    "TEXT",
				Content: "This is a sample updated TEXT question",
				Options: nil,
			},
			setupMock: func(m *mocks.Querier) {
				m.On("UpdateQuestion", mock.Anything, questions.UpdateQuestionParams{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "TEXT",
					Content: "This is a sample updated TEXT question",
				}).Return(questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "TEXT",
					Content: "This is a sample updated TEXT question",
				}, nil)

				m.On("DeleteCorrespondingOption", mock.Anything,
					uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003")).Return(nil)
			},
			wantResult: questions.BoundQuestion{
				Question: questions.Question{
					ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					Type:    "TEXT",
					Content: "This is a sample updated TEXT question",
				},
				Options: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := mocks.NewQuerier(t)
			tt.setupMock(querier)

			logger, err := logutil.InitLogger()
			assert.NoError(t, err, "failed to initialize logger")

			service := questions.NewServiceWithQuerier(logger, querier)

			ctx := context.Background()
			actualResp, err := service.UpdateQuestion(ctx,
				uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				questions.QuestionRequest{
					Type:    tt.arg.Type,
					Content: tt.arg.Content,
					Options: tt.arg.Options,
				})

			assert.NoError(t, err)
			assert.Equal(t, tt.wantResult, actualResp)
		})
	}
}

func TestHandler_SubmitAnswer(t *testing.T) {
	type arg struct {
		QuestionID       uuid.UUID
		SelectedOptionID *uuid.UUID
		TextAnswer       string
	}

	selOptionp := uuid.MustParse("9a6ba605-fa30-42c1-8ffd-401976ddbd43")

	tests := []struct {
		name       string
		arg        arg
		setupMock  func(m *mocks.Querier)
		wantResult questions.Answer
	}{
		{
			name: "Should submit a CHOICE answer",
			arg: arg{
				QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				SelectedOptionID: &selOptionp,
				TextAnswer:       "",
			},
			setupMock: func(m *mocks.Querier) {
				m.On("SubmitAnswer", mock.Anything, questions.SubmitAnswerParams{
					QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					SelectedOptionID: &selOptionp,
					TextAnswer:       "",
				}).Return(questions.Answer{
					ID:               uuid.MustParse("c2e7626c-d6f1-4345-9938-06d425681ab4"),
					QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					SelectedOptionID: &selOptionp,
					TextAnswer:       "",
					CreatedAt: pgtype.Timestamptz{
						Time:             time.Time{},
						InfinityModifier: 0,
						Valid:            true,
					},
				}, nil)
			},
			wantResult: questions.Answer{
				ID:               uuid.MustParse("c2e7626c-d6f1-4345-9938-06d425681ab4"),
				QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				SelectedOptionID: &selOptionp,
				TextAnswer:       "",
				CreatedAt: pgtype.Timestamptz{
					Time:             time.Time{},
					InfinityModifier: 0,
					Valid:            true,
				},
			},
		},
		{
			name: "Should submit a TEXT answer",
			arg: arg{
				QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				SelectedOptionID: nil,
				TextAnswer:       "This is a sample TEXT answer",
			},
			setupMock: func(m *mocks.Querier) {
				m.On("SubmitAnswer", mock.Anything, questions.SubmitAnswerParams{
					QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					SelectedOptionID: nil,
					TextAnswer:       "This is a sample TEXT answer",
				}).Return(questions.Answer{
					ID:               uuid.MustParse("c2e7626c-d6f1-4345-9938-06d425681ab4"),
					QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					SelectedOptionID: nil,
					TextAnswer:       "This is a sample TEXT answer",
					CreatedAt: pgtype.Timestamptz{
						Time:             time.Time{},
						InfinityModifier: 0,
						Valid:            true,
					},
				}, nil)
			},
			wantResult: questions.Answer{
				ID:               uuid.MustParse("c2e7626c-d6f1-4345-9938-06d425681ab4"),
				QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				SelectedOptionID: nil,
				TextAnswer:       "This is a sample TEXT answer",
				CreatedAt: pgtype.Timestamptz{
					Time:             time.Time{},
					InfinityModifier: 0,
					Valid:            true,
				},
			},
		},
		{
			name: "Should submit a both TEXT and CHOICE answer",
			arg: arg{
				QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				SelectedOptionID: &selOptionp,
				TextAnswer:       "This is a sample TEXT answer",
			},
			setupMock: func(m *mocks.Querier) {
				m.On("SubmitAnswer", mock.Anything, questions.SubmitAnswerParams{
					QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					SelectedOptionID: &selOptionp,
					TextAnswer:       "This is a sample TEXT answer",
				}).Return(questions.Answer{
					ID:               uuid.MustParse("c2e7626c-d6f1-4345-9938-06d425681ab4"),
					QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
					SelectedOptionID: &selOptionp,
					TextAnswer:       "This is a sample TEXT answer",
					CreatedAt: pgtype.Timestamptz{
						Time:             time.Time{},
						InfinityModifier: 0,
						Valid:            true,
					},
				}, nil)
			},
			wantResult: questions.Answer{
				ID:               uuid.MustParse("c2e7626c-d6f1-4345-9938-06d425681ab4"),
				QuestionID:       uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
				SelectedOptionID: &selOptionp,
				TextAnswer:       "This is a sample TEXT answer",
				CreatedAt: pgtype.Timestamptz{
					Time:             time.Time{},
					InfinityModifier: 0,
					Valid:            true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := mocks.NewQuerier(t)
			tt.setupMock(querier)

			logger, err := logutil.InitLogger()
			assert.NoError(t, err, "failed to initialize logger")

			service := questions.NewServiceWithQuerier(logger, querier)

			ctx := context.Background()
			actualResp, err := service.SubmitAnswer(ctx, questions.SubmitAnswerParams{
				QuestionID:       tt.arg.QuestionID,
				SelectedOptionID: tt.arg.SelectedOptionID,
				TextAnswer:       tt.arg.TextAnswer,
			})

			assert.NoError(t, err)
			assert.Equal(t, tt.wantResult, actualResp)
		})
	}
}
