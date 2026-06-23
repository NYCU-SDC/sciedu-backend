package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHandlerSession(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	validSession := Session{
		UserID:                uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Username:              "Student",
		Email:                 "student@example.com",
		AccessTokenExpiresAt:  now.Add(accessTokenLifetime),
		RefreshTokenExpiresAt: now.Add(refreshTokenLifetime),
	}

	tests := []struct {
		name       string
		cookies    []*http.Cookie
		sessionErr error
		wantStatus int
	}{
		{
			name: "valid cookies return expiry metadata",
			cookies: []*http.Cookie{
				{Name: accessTokenCookieName, Value: "access"},
				{Name: refreshTokenCookieName, Value: "refresh"},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing cookies returns unauthorized",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "service unauthorized maps to problem response",
			cookies: []*http.Cookie{
				{Name: accessTokenCookieName, Value: "access"},
				{Name: refreshTokenCookieName, Value: "refresh"},
			},
			sessionErr: handlerutil.ErrUnauthorized,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeHandlerService{session: validSession, sessionErr: tt.sessionErr}
			handler := NewHandler(svc, CookieConfig{Environment: EnvironmentDev}, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
			for _, cookie := range tt.cookies {
				req.AddCookie(cookie)
			}
			rec := httptest.NewRecorder()

			handler.Session(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusOK {
				require.Contains(t, rec.Body.String(), `"username":"Student"`)
				require.Contains(t, rec.Body.String(), `"email":"student@example.com"`)
				require.Contains(t, rec.Body.String(), "accessTokenExpiresAt")
				require.Contains(t, rec.Body.String(), "refreshTokenExpiresAt")
				require.Equal(t, "access", svc.accessToken)
				require.Equal(t, "refresh", svc.refreshToken)
			} else {
				require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestHandlerRefresh(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	session := Session{
		UserID:                uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		AccessToken:           "new-access",
		RefreshToken:          "new-refresh",
		Username:              "Student",
		Email:                 "student@example.com",
		AccessTokenExpiresAt:  now.Add(accessTokenLifetime),
		RefreshTokenExpiresAt: now.Add(refreshTokenLifetime),
	}

	tests := []struct {
		name       string
		cookie     *http.Cookie
		refreshErr error
		wantStatus int
		wantSet    bool
		wantClear  bool
	}{
		{
			name:       "fresh refresh token sets rotated cookies",
			cookie:     &http.Cookie{Name: refreshTokenCookieName, Value: "refresh"},
			wantStatus: http.StatusOK,
			wantSet:    true,
		},
		{
			name:       "missing refresh cookie is unauthorized",
			wantStatus: http.StatusUnauthorized,
			wantClear:  true,
		},
		{
			name:       "reuse detection clears cookies",
			cookie:     &http.Cookie{Name: refreshTokenCookieName, Value: "refresh"},
			refreshErr: ErrRefreshReuseDetected,
			wantStatus: http.StatusUnauthorized,
			wantClear:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeHandlerService{session: session, refreshErr: tt.refreshErr}
			handler := NewHandler(svc, CookieConfig{Environment: EnvironmentDev}, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()

			handler.Refresh(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			cookies := rec.Result().Cookies()
			if tt.wantSet {
				require.Contains(t, rec.Body.String(), `"username":"Student"`)
				require.Contains(t, rec.Body.String(), `"email":"student@example.com"`)
				requireCookie(t, cookies, accessTokenCookieName, "new-access", true)
				requireCookie(t, cookies, refreshTokenCookieName, "new-refresh", true)
			}
			if tt.wantClear {
				requireClearedCookie(t, cookies, accessTokenCookieName)
				requireClearedCookie(t, cookies, refreshTokenCookieName)
			}
		})
	}
}

func TestHandlerLogout(t *testing.T) {
	tests := []struct {
		name       string
		cookie     *http.Cookie
		logoutErr  error
		wantStatus int
	}{
		{
			name:       "logout revokes and clears cookies",
			cookie:     &http.Cookie{Name: refreshTokenCookieName, Value: "refresh"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing cookie still clears cookies",
			wantStatus: http.StatusOK,
		},
		{
			name:       "service error maps to problem response",
			cookie:     &http.Cookie{Name: refreshTokenCookieName, Value: "refresh"},
			logoutErr:  errors.New("db down"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeHandlerService{logoutErr: tt.logoutErr}
			handler := NewHandler(svc, CookieConfig{Environment: EnvironmentDev}, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()

			handler.Logout(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusOK {
				require.Equal(t, tt.cookie != nil, svc.logoutToken == "refresh")
				requireClearedCookie(t, rec.Result().Cookies(), accessTokenCookieName)
				requireClearedCookie(t, rec.Result().Cookies(), refreshTokenCookieName)
			}
		})
	}
}

func TestHandlerCallbackOAuthExchangeError(t *testing.T) {
	svc := &fakeHandlerService{completeErr: errOAuthCodeExchange}
	handler := NewHandler(svc, CookieConfig{Environment: EnvironmentDev}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=bad-code&state=state", nil)
	rec := httptest.NewRecorder()

	handler.Callback(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "oauth code exchange failed")
	requireClearedCookie(t, rec.Result().Cookies(), accessTokenCookieName)
	requireClearedCookie(t, rec.Result().Cookies(), refreshTokenCookieName)
}

func TestHandlerCallbackInvalidOAuthState(t *testing.T) {
	svc := &fakeHandlerService{completeErr: errInvalidOAuthState}
	handler := NewHandler(svc, CookieConfig{Environment: EnvironmentDev}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=bad-code&state=state", nil)
	rec := httptest.NewRecorder()

	handler.Callback(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), "You must be logged in to access this resource")
	requireClearedCookie(t, rec.Result().Cookies(), accessTokenCookieName)
	requireClearedCookie(t, rec.Result().Cookies(), refreshTokenCookieName)
}

func TestHandlerSessionCookieAttrs(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	session := Session{
		AccessToken:           "access",
		RefreshToken:          "refresh",
		RefreshTokenExpiresAt: now.Add(refreshTokenLifetime),
	}

	tests := []struct {
		name            string
		config          CookieConfig
		request         *http.Request
		redirectURL     string
		wantDomain      string
		wantSecure      bool
		wantAccessSite  http.SameSite
		wantRefreshSite http.SameSite
	}{
		{
			name:            "dev https backend with localhost redirect uses cross-site secure cookies",
			config:          CookieConfig{Environment: EnvironmentDev},
			request:         requestWithForwardedProto("https"),
			redirectURL:     "http://localhost:5173/auth/callback",
			wantSecure:      true,
			wantAccessSite:  http.SameSiteNoneMode,
			wantRefreshSite: http.SameSiteNoneMode,
		},
		{
			name:            "dev https backend with localhost origin uses cross-site secure cookies",
			config:          CookieConfig{Environment: EnvironmentDev},
			request:         requestWithOriginAndForwardedProto("http://localhost:3000", "https"),
			wantSecure:      true,
			wantAccessSite:  http.SameSiteNoneMode,
			wantRefreshSite: http.SameSiteNoneMode,
		},
		{
			name:            "dev http localhost backend uses same-site cookies without secure",
			config:          CookieConfig{Environment: EnvironmentDev},
			request:         requestWithOriginAndForwardedProto("http://localhost:5173", "http"),
			wantAccessSite:  http.SameSiteLaxMode,
			wantRefreshSite: http.SameSiteStrictMode,
		},
		{
			name:            "prod uses shared domain and stricter SameSite cookies",
			config:          CookieConfig{Environment: EnvironmentProd},
			request:         httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil),
			wantDomain:      defaultCookieDomain,
			wantSecure:      true,
			wantAccessSite:  http.SameSiteLaxMode,
			wantRefreshSite: http.SameSiteStrictMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(&fakeHandlerService{}, tt.config, nil)
			attrs := handler.cookieAttrs(tt.request, tt.redirectURL)
			accessCookie := handler.accessCookie(session.AccessToken, int(accessTokenLifetime.Seconds()), attrs)
			refreshCookie := handler.refreshCookie(session.RefreshToken, int(timeUntil(session.RefreshTokenExpiresAt).Seconds()), attrs)

			require.Equal(t, tt.wantDomain, accessCookie.Domain)
			require.Equal(t, tt.wantDomain, refreshCookie.Domain)
			require.Equal(t, tt.wantSecure, accessCookie.Secure)
			require.Equal(t, tt.wantSecure, refreshCookie.Secure)
			require.Equal(t, tt.wantAccessSite, accessCookie.SameSite)
			require.Equal(t, tt.wantRefreshSite, refreshCookie.SameSite)
		})
	}
}

func TestHandlerClearSessionCookieAttrs(t *testing.T) {
	handler := NewHandler(&fakeHandlerService{}, CookieConfig{Environment: EnvironmentDev}, nil)
	req := requestWithOriginAndForwardedProto("http://localhost:5173", "https")
	rec := httptest.NewRecorder()

	handler.clearSessionCookies(rec, req)

	cookies := rec.Result().Cookies()
	accessCookie := findCookie(t, cookies, accessTokenCookieName)
	refreshCookie := findCookie(t, cookies, refreshTokenCookieName)
	require.Empty(t, accessCookie.Domain)
	require.Empty(t, refreshCookie.Domain)
	require.Equal(t, "/", accessCookie.Path)
	require.Equal(t, "/api/auth", refreshCookie.Path)
	require.Equal(t, http.SameSiteNoneMode, accessCookie.SameSite)
	require.Equal(t, http.SameSiteNoneMode, refreshCookie.SameSite)
	require.True(t, accessCookie.Secure)
	require.True(t, refreshCookie.Secure)
	require.LessOrEqual(t, accessCookie.MaxAge, -1)
	require.LessOrEqual(t, refreshCookie.MaxAge, -1)
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		want       string
	}{
		{name: "remote ipv4 host port", remoteAddr: "127.0.0.1:54321", want: "127.0.0.1"},
		{name: "remote ipv6 host port", remoteAddr: "[::1]:54321", want: "::1"},
		{name: "forwarded first ip", remoteAddr: "127.0.0.1:54321", forwarded: "203.0.113.10, 198.51.100.1", want: "203.0.113.10"},
		{name: "invalid forwarded is dropped", remoteAddr: "127.0.0.1:54321", forwarded: "unknown", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}

			require.Equal(t, tt.want, clientIP(req))
		})
	}
}

type fakeHandlerService struct {
	session      Session
	begin        BeginOAuthResult
	complete     CompleteOAuthResult
	beginErr     error
	completeErr  error
	sessionErr   error
	refreshErr   error
	logoutErr    error
	accessToken  string
	refreshToken string
	logoutToken  string
}

func (s *fakeHandlerService) BeginOAuth(ctx context.Context, params BeginOAuthParams) (BeginOAuthResult, error) {
	if s.beginErr != nil {
		return BeginOAuthResult{}, s.beginErr
	}
	return s.begin, nil
}

func (s *fakeHandlerService) CompleteOAuth(ctx context.Context, params CompleteOAuthParams) (CompleteOAuthResult, error) {
	if s.completeErr != nil {
		return CompleteOAuthResult{}, s.completeErr
	}
	return s.complete, nil
}

func (s *fakeHandlerService) Session(ctx context.Context, accessToken, refreshToken string) (Session, error) {
	s.accessToken = accessToken
	s.refreshToken = refreshToken
	if s.sessionErr != nil {
		return Session{}, s.sessionErr
	}
	return s.session, nil
}

func (s *fakeHandlerService) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	if s.refreshErr != nil {
		return Session{}, s.refreshErr
	}
	return s.session, nil
}

func (s *fakeHandlerService) Logout(ctx context.Context, refreshToken string) error {
	s.logoutToken = refreshToken
	return s.logoutErr
}

func requireCookie(t *testing.T, cookies []*http.Cookie, name, value string, httpOnly bool) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			require.Equal(t, value, cookie.Value)
			require.Equal(t, httpOnly, cookie.HttpOnly)
			return
		}
	}
	t.Fatalf("missing cookie %s", name)
}

func requireClearedCookie(t *testing.T, cookies []*http.Cookie, name string) {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			require.Empty(t, cookie.Value)
			require.LessOrEqual(t, cookie.MaxAge, -1)
			return
		}
	}
	t.Fatalf("missing cleared cookie %s", name)
}

func requestWithForwardedProto(proto string) *http.Request {
	return requestWithOriginAndForwardedProto("", proto)
}

func requestWithOriginAndForwardedProto(origin, proto string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if proto != "" {
		req.Header.Set("X-Forwarded-Proto", proto)
	}
	return req
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing cookie %s", name)
	return nil
}
