package healthz_test

import (
	"net/http"
	"net/http/httptest"
	logutil "sciedu-backend/internal/error"
	"sciedu-backend/internal/healthz"
	"sciedu-backend/internal/healthz/mocks"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandler_Healthz(t *testing.T) {
	tests := []struct {
		name       string
		setupMock  func(m *mocks.Store)
		wantStatus int
	}{
		{
			name: "Should return 200 OK",
			setupMock: func(m *mocks.Store) {
				m.On("Healthz").Return(true, nil)
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := mocks.NewStore(t)
			tt.setupMock(store)

			logger, err := logutil.InitLogger()
			assert.NoError(t, err, "failed to initialize logger")

			r := httptest.NewRequest(http.MethodGet, "/Healthz", nil)
			w := httptest.NewRecorder()

			handler := healthz.NewHandler(logger, store)
			handler.Healthz(w, r)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
