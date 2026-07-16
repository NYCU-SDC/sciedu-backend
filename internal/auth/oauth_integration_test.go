//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestOAuthCallbackCreatesUsableLocalSession(t *testing.T) {
	databaseURL := os.Getenv("AUTH_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AUTH_INTEGRATION_DATABASE_URL is not set")
	}

	ctx := t.Context()
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(ctx))

	testID := uuid.NewString()
	email := "oauth-integration-" + testID + "@example.com"
	userAgent := "sciedu-auth-integration-" + testID
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM oauth_login_states WHERE user_agent = $1", userAgent)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", email)
	})

	provider := integrationOAuthProvider{
		claims: GoogleIDTokenClaims{
			Email:         email,
			EmailVerified: true,
			Name:          "OAuth Integration User",
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "oauth-integration-" + testID,
			},
		},
	}
	service := NewService(NewStore(pool), ServiceConfig{
		Secret:               "integration-test-secret",
		Environment:          EnvironmentDev,
		OAuthProvider:        provider,
		RedirectURLAllowlist: []string{"http://localhost:5173"},
	}, nil)
	handler := NewHandler(service, CookieConfig{Environment: EnvironmentDev}, nil)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	loginRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		server.URL+"/api/login/oauth/google?r="+url.QueryEscape("http://localhost:5173/"),
		nil,
	)
	require.NoError(t, err)
	loginRequest.Header.Set("User-Agent", userAgent)
	loginResponse, err := client.Do(loginRequest)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = loginResponse.Body.Close()
	})
	require.Equal(t, http.StatusFound, loginResponse.StatusCode)

	authURL, err := url.Parse(loginResponse.Header.Get("Location"))
	require.NoError(t, err)
	state := authURL.Query().Get("state")
	require.NotEmpty(t, state)

	callbackRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		server.URL+"/api/auth/callback?code=integration-code&state="+url.QueryEscape(state),
		nil,
	)
	require.NoError(t, err)
	callbackRequest.Header.Set("User-Agent", userAgent)
	callbackResponse, err := client.Do(callbackRequest)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = callbackResponse.Body.Close()
	})
	require.Equal(t, http.StatusFound, callbackResponse.StatusCode)
	require.Equal(t, "http://localhost:5173/", callbackResponse.Header.Get("Location"))

	callbackCookies := callbackResponse.Cookies()
	accessCookie := findCookie(t, callbackCookies, accessTokenCookieName)
	refreshCookie := findCookie(t, callbackCookies, refreshTokenCookieName)
	require.False(t, accessCookie.Secure)
	require.False(t, refreshCookie.Secure)
	require.Equal(t, http.SameSiteLaxMode, accessCookie.SameSite)
	require.Equal(t, http.SameSiteStrictMode, refreshCookie.SameSite)

	sessionRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		server.URL+"/api/auth/session",
		nil,
	)
	require.NoError(t, err)
	sessionRequest.AddCookie(accessCookie)
	sessionRequest.AddCookie(refreshCookie)
	sessionResponse, err := client.Do(sessionRequest)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sessionResponse.Body.Close()
	})
	require.Equal(t, http.StatusOK, sessionResponse.StatusCode)

	var session Session
	require.NoError(t, json.NewDecoder(sessionResponse.Body).Decode(&session))
	require.Equal(t, "OAuth Integration User", session.Username)
	require.Equal(t, email, session.Email)
	require.False(t, session.AccessTokenExpiresAt.IsZero())
	require.False(t, session.RefreshTokenExpiresAt.IsZero())
}

type integrationOAuthProvider struct {
	claims GoogleIDTokenClaims
}

func (p integrationOAuthProvider) Name() string {
	return googleProviderName
}

func (p integrationOAuthProvider) AuthCodeURL(state, _ string) string {
	return "https://provider.test/oauth?state=" + url.QueryEscape(state)
}

func (p integrationOAuthProvider) ExchangeIDToken(context.Context, string, string) (string, error) {
	return "integration-id-token", nil
}

func (p integrationOAuthProvider) VerifyIDToken(context.Context, string) (GoogleIDTokenClaims, error) {
	return p.claims, nil
}
