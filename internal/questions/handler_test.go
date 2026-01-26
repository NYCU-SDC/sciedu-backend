package questions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	logutil "sciedu-backend/internal/error"
	questions "sciedu-backend/internal/questions"
	"sciedu-backend/internal/questions/mocks"
	"testing"

	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestHandler_CreateQuestion(t *testing.T) {
	type arg struct {
		Type    string
		Content string
		Options []questions.OptionRequest
	}

	tests := []struct {
		name       string
		arg        arg
		setupMock  func(m *mocks.Store)
		wantResult any
		wantStatus int
	}{
		{
			name: "Should create a CHOICE question",
			arg: arg{
				Type:    "CHOICE",
				Content: "This is a sample CHOICE question",
				Options: []questions.OptionRequest{
					{Label: "A", Content: "This is text for option A"},
					{Label: "B", Content: "This is text for option B"},
					{Label: "C", Content: "This is text for option C"},
					{Label: "D", Content: "This is text for option D"},
				},
			},
			setupMock: func(m *mocks.Store) {
				m.On("CreateQuestion", mock.Anything, questions.QuestionRequest{
					Type:    "CHOICE",
					Content: "This is a sample CHOICE question",
					Options: []questions.OptionRequest{
						{Label: "A", Content: "This is text for option A"},
						{Label: "B", Content: "This is text for option B"},
						{Label: "C", Content: "This is text for option C"},
						{Label: "D", Content: "This is text for option D"},
					},
				}).Return(questions.BoundQuestion{
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
							Content:    "This is text for option A",
						},
						{
							ID:         uuid.MustParse("26e846ad-e36c-43b7-91ef-647130da9880"),
							QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
							Label:      "B",
							Content:    "This is text for option B",
						},
						{
							ID:         uuid.MustParse("4a66e144-1d9b-4837-b525-608ea89d1490"),
							QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
							Label:      "C",
							Content:    "This is text for option C",
						},
						{
							ID:         uuid.MustParse("eb040b25-8b60-43cc-b82c-331332df0598"),
							QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
							Label:      "D",
							Content:    "This is text for option D",
						},
					},
				}, nil)
			},
			wantResult: questions.QuestionResponse{
				ID:      "82e1d250-cf47-4f97-9937-1550b9d57003",
				Type:    "CHOICE",
				Content: "This is a sample CHOICE question",
				Options: []questions.Option{
					{
						ID:         uuid.MustParse("9a6ba605-fa30-42c1-8ffd-401976ddbd43"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "A",
						Content:    "This is text for option A",
					},
					{
						ID:         uuid.MustParse("26e846ad-e36c-43b7-91ef-647130da9880"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "B",
						Content:    "This is text for option B",
					},
					{
						ID:         uuid.MustParse("4a66e144-1d9b-4837-b525-608ea89d1490"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "C",
						Content:    "This is text for option C",
					},
					{
						ID:         uuid.MustParse("eb040b25-8b60-43cc-b82c-331332df0598"),
						QuestionID: uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Label:      "D",
						Content:    "This is text for option D",
					},
				},
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "Should create a TEXT question",
			arg: arg{
				Type:    "TEXT",
				Content: "This is a sample TEXT question",
				Options: nil,
			},
			setupMock: func(m *mocks.Store) {
				m.On("CreateQuestion", mock.Anything, questions.QuestionRequest{
					Type:    "TEXT",
					Content: "This is a sample TEXT question",
					Options: nil,
				}).Return(questions.BoundQuestion{
					Question: questions.Question{
						ID:      uuid.MustParse("82e1d250-cf47-4f97-9937-1550b9d57003"),
						Type:    "TEXT",
						Content: "This is a sample TEXT question",
					},
				}, nil)
			},
			wantResult: questions.QuestionResponse{
				ID:      "82e1d250-cf47-4f97-9937-1550b9d57003",
				Type:    "TEXT",
				Content: "This is a sample TEXT question",
				Options: nil,
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "Should return error when type is CHOICE and missing options",
			arg: arg{
				Type:    "CHOICE",
				Content: "This is a sample CHOICE question",
				Options: nil,
			},
			setupMock: func(m *mocks.Store) {
				m.AssertNotCalled(t, "CreateQuestion")
			},
			wantResult: problemutil.NewValidateProblem("Key: 'QuestionRequest.Options' Error:Field validation for 'Options' failed on the 'required_if' tag"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "Should return error when the length of the content less than 1",
			arg: arg{
				Type:    "TEXT",
				Content: "",
				Options: nil,
			},
			setupMock: func(m *mocks.Store) {
				m.AssertNotCalled(t, "CreateQuestion")
			},
			wantResult: problemutil.NewValidateProblem("Key: 'QuestionRequest.Content' Error:Field validation for 'Content' failed on the 'required' tag"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "Should return error when the length of the content bigger than 2000",
			arg: arg{
				Type:    "TEXT",
				Content: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Options: nil,
			},
			setupMock: func(m *mocks.Store) {
				m.AssertNotCalled(t, "CreateQuestion")
			},
			wantResult: problemutil.NewValidateProblem("Key: 'QuestionRequest.Content' Error:Field validation for 'Content' failed on the 'max' tag"),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := mocks.NewStore(t)
			problemWriter := problemutil.New()
			tt.setupMock(store)

			logger, err := logutil.InitLogger()
			assert.NoError(t, err, "failed to initialize logger")

			requestBody, err := json.Marshal(tt.arg)
			if err != nil {
				assert.Failf(t, "Failed to marshal request body", "%+v", err)
			}

			r := httptest.NewRequest(http.MethodPost, "/api/questions", bytes.NewReader(requestBody))
			w := httptest.NewRecorder()

			handler := questions.NewHandler(logger, problemWriter, validator.New(), store)
			handler.CreateQuestion(w, r)

			assert.Equal(t, tt.wantStatus, w.Code)

			var data interface{} = tt.wantResult

			var questionResp questions.QuestionResponse
			var problemResp problemutil.Problem

			if _, ok := data.(questions.QuestionResponse); ok {
				err = json.Unmarshal(w.Body.Bytes(), &questionResp)
				assert.NoError(t, err, "Failed to unmarshal actual response body")
				assert.Equal(t, tt.wantResult, questionResp)

			} else {
				err = json.Unmarshal(w.Body.Bytes(), &problemResp)
				assert.NoError(t, err, "Failed to unmarshal expect response body")
				assert.Equal(t, tt.wantResult, problemResp)

			}
		})
	}
}
