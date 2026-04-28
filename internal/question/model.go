package question

import (
	"time"

	"github.com/google/uuid"
)

type Type string

const (
	TypeChoice Type = "CHOICE"
	TypeText   Type = "TEXT"
)

type Option struct {
	ID        uuid.UUID `json:"id"`
	Label     string    `json:"label"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type Question struct {
	ID        uuid.UUID `json:"id"`
	Type      Type      `json:"type"`
	Content   string    `json:"content"`
	Options   []Option  `json:"options,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type Answer struct {
	ID               uuid.UUID  `json:"id"`
	QuestionID       uuid.UUID  `json:"questionId"`
	SelectedOptionID *uuid.UUID `json:"selectedOptionId,omitempty"`
	TextAnswer       *string    `json:"textAnswer,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type OptionInput struct {
	Label   string `json:"label"`
	Content string `json:"content"`
}

type UpsertRequest struct {
	Type    Type          `json:"type"`
	Content string        `json:"content"`
	Options []OptionInput `json:"options"`
}

type SubmitAnswerRequest struct {
	SelectedOptionID *uuid.UUID `json:"selectedOptionId"`
	TextAnswer       *string    `json:"textAnswer"`
}
