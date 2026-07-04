package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChatServiceDeleteChat(t *testing.T) {
	chatID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	dbErr := errors.New("database unavailable")

	tests := []struct {
		name         string
		rowsAffected int64
		deleteErr    error
		wantError    bool
	}{
		{name: "deletes owned chat", rowsAffected: 1},
		{name: "missing or unowned chat is not found", wantError: true},
		{name: "database error is returned", deleteErr: dbErr, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeChatQuerier{
				deleteRowsAffected: tt.rowsAffected,
				deleteErr:          tt.deleteErr,
			}
			service := NewService(nil, querier, NewStreamHub(), zap.NewNop())

			err := service.DeleteChat(t.Context(), chatID, userID)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, DeleteChatParams{ID: chatID, UserID: userID}, querier.deleteParams)
		})
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "preserves short title", title: "short title", want: "short title"},
		{name: "preserves 255 runes", title: strings.Repeat("好", 255), want: strings.Repeat("好", 255)},
		{name: "truncates ASCII title", title: strings.Repeat("a", 256), want: strings.Repeat("a", 255)},
		{name: "truncates multibyte title by rune", title: strings.Repeat("好", 256), want: strings.Repeat("好", 255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, truncateTitle(tt.title))
		})
	}
}

type fakeChatQuerier struct {
	*Queries
	deleteParams       DeleteChatParams
	deleteRowsAffected int64
	deleteErr          error
}

func (q *fakeChatQuerier) DeleteChat(_ context.Context, params DeleteChatParams) (int64, error) {
	q.deleteParams = params
	return q.deleteRowsAffected, q.deleteErr
}
