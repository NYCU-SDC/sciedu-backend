package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCreateMessagePassesModelUnchanged(t *testing.T) {
	chatID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name      string
		body      string
		wantModel string
	}{
		{name: "passes an arbitrary model string", body: `{"content":"hello","model":"provider/custom model@preview"}`, wantModel: "provider/custom model@preview"},
		{name: "uses an empty model when omitted", body: `{"content":"hello"}`, wantModel: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeHandlerStore{}
			handler := NewHandler(store, zap.NewNop())
			mux := http.NewServeMux()
			handler.RegisterRoutes(mux, nil)
			protected := auth.NewMiddleware(fakeAccessTokenVerifier{userID: userID}, zap.NewNop()).HandlerFunc(mux.ServeHTTP)

			req := httptest.NewRequest(http.MethodPost, "/api/chat/"+chatID.String(), strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "access_token", Value: "test-token"})
			rec := httptest.NewRecorder()

			protected(rec, req)

			require.Equal(t, http.StatusCreated, rec.Code)
			require.Equal(t, tt.wantModel, store.model)
		})
	}
}

type fakeHandlerStore struct {
	model string
}

func (f *fakeHandlerStore) CreateChat(context.Context, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func (f *fakeHandlerStore) GetChat(context.Context, uuid.UUID, uuid.UUID) (Chat, []MessageReturn, error) {
	return Chat{}, nil, nil
}

func (f *fakeHandlerStore) CreateMessage(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ uuid.UUID, model string) (CreateMessageReturn, error) {
	f.model = model
	return CreateMessageReturn{Message: MessageReturn{ID: uuid.New(), CreatedAt: time.Now()}}, nil
}

func (f *fakeHandlerStore) Stream(context.Context, uuid.UUID, uuid.UUID) (bool, <-chan StreamDelta, <-chan error, func()) {
	return false, nil, nil, func() {}
}

func (f *fakeHandlerStore) ValidatePreviousID(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeHandlerStore) ListChats(context.Context, uuid.UUID, int32, int32) (ChatPage, error) {
	return ChatPage{}, nil
}

func (f *fakeHandlerStore) DeleteChat(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type fakeAccessTokenVerifier struct {
	userID uuid.UUID
}

func (f fakeAccessTokenVerifier) VerifyAccessToken(string) (auth.AccessTokenClaims, error) {
	return auth.AccessTokenClaims{UserID: f.userID, ExpiresAt: time.Now().Add(time.Hour)}, nil
}
