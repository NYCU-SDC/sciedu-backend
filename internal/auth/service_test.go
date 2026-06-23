package auth

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestServiceRefresh(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	familyID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	tokenID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	rawToken := uuid.MustParse("44444444-4444-4444-4444-444444444444").String()

	tests := []struct {
		name       string
		record     RefreshTokenRecord
		findErr    error
		rotateErr  error
		wantErr    error
		wantRotate bool
		wantReuse  bool
	}{
		{
			name: "fresh current token rotates",
			record: RefreshTokenRecord{
				ID:              tokenID,
				FamilyID:        familyID,
				UserID:          userID,
				Hash:            refreshTokenHash(rawToken),
				IsCurrent:       true,
				IssuedAt:        now.Add(-time.Minute),
				FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			wantRotate: true,
		},
		{
			name:    "unknown token is unauthorized",
			findErr: errRefreshTokenNotFound,
			wantErr: handlerutil.ErrUnauthorized,
		},
		{
			name: "used token marks reuse detected",
			record: RefreshTokenRecord{
				ID:              tokenID,
				FamilyID:        familyID,
				UserID:          userID,
				Hash:            refreshTokenHash(rawToken),
				IsCurrent:       false,
				IssuedAt:        now.Add(-time.Hour),
				UsedAt:          ptrTime(now.Add(-time.Minute)),
				FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			wantErr:   handlerutil.ErrUnauthorized,
			wantReuse: true,
		},
		{
			name: "revoked family is unauthorized",
			record: RefreshTokenRecord{
				ID:              tokenID,
				FamilyID:        familyID,
				UserID:          userID,
				Hash:            refreshTokenHash(rawToken),
				IsCurrent:       true,
				IssuedAt:        now.Add(-time.Hour),
				FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
				FamilyRevokedAt: ptrTime(now.Add(-time.Minute)),
			},
			wantErr: handlerutil.ErrUnauthorized,
		},
		{
			name: "expired family is unauthorized",
			record: RefreshTokenRecord{
				ID:              tokenID,
				FamilyID:        familyID,
				UserID:          userID,
				Hash:            refreshTokenHash(rawToken),
				IsCurrent:       true,
				IssuedAt:        now.Add(-31 * 24 * time.Hour),
				FamilyExpiresAt: now.Add(-time.Minute),
			},
			wantErr: handlerutil.ErrUnauthorized,
		},
		{
			name: "concurrent rotation reuse is unauthorized",
			record: RefreshTokenRecord{
				ID:              tokenID,
				FamilyID:        familyID,
				UserID:          userID,
				Hash:            refreshTokenHash(rawToken),
				IsCurrent:       true,
				IssuedAt:        now.Add(-time.Minute),
				FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			rotateErr:  ErrRefreshReuseDetected,
			wantErr:    handlerutil.ErrUnauthorized,
			wantRotate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAuthRepository{record: tt.record, findErr: tt.findErr, rotateErr: tt.rotateErr, profile: testUserProfile(userID)}
			svc := NewService(repo, ServiceConfig{
				Secret:      "test-secret",
				Environment: EnvironmentDev,
				Now:         func() time.Time { return now },
			}, nil)

			session, err := svc.Refresh(context.Background(), rawToken)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, session.AccessToken)
				require.Equal(t, tt.wantReuse, repo.reuseDetected)
				require.Equal(t, tt.wantRotate, repo.rotated)
				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, session.AccessToken)
			require.NotEmpty(t, session.RefreshToken)
			require.Equal(t, userID, session.UserID)
			require.Equal(t, "Student", session.Username)
			require.Equal(t, "student@example.com", session.Email)
			require.Equal(t, now.Add(accessTokenLifetime), session.AccessTokenExpiresAt)
			require.Equal(t, tt.record.FamilyExpiresAt, session.RefreshTokenExpiresAt)
			require.Equal(t, tt.wantRotate, repo.rotated)
		})
	}
}

func TestServiceLogout(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	rawToken := uuid.MustParse("55555555-5555-5555-5555-555555555555").String()

	tests := []struct {
		name       string
		rawToken   string
		revokeErr  error
		wantErr    error
		wantRevoke bool
	}{
		{name: "revokes refresh family", rawToken: rawToken, wantRevoke: true},
		{name: "missing cookie is still idempotent success"},
		{name: "repository error is returned", rawToken: rawToken, revokeErr: errors.New("db down"), wantErr: errors.New("db down"), wantRevoke: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeAuthRepository{revokeErr: tt.revokeErr}
			svc := NewService(repo, ServiceConfig{
				Secret:      "test-secret",
				Environment: EnvironmentDev,
				Now:         func() time.Time { return now },
			}, nil)

			err := svc.Logout(context.Background(), tt.rawToken)
			if tt.wantErr != nil {
				require.ErrorContains(t, err, tt.wantErr.Error())
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantRevoke, repo.revoked)
		})
	}
}

func TestServiceSession(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	refreshToken := uuid.MustParse("77777777-7777-7777-7777-777777777777").String()

	repo := &fakeAuthRepository{
		profile: testUserProfile(userID),
		record: RefreshTokenRecord{
			ID:              uuid.New(),
			FamilyID:        uuid.New(),
			UserID:          userID,
			Hash:            refreshTokenHash(refreshToken),
			IsCurrent:       true,
			IssuedAt:        now,
			FamilyExpiresAt: now.Add(refreshTokenLifetime),
		},
	}
	svc := NewService(repo, ServiceConfig{
		Secret:      "test-secret",
		Environment: EnvironmentDev,
		Now:         func() time.Time { return now },
	}, nil)
	issued, err := svc.IssueSession(context.Background(), IssueSessionParams{UserID: userID})
	require.NoError(t, err)

	tests := []struct {
		name         string
		accessToken  string
		refreshToken string
		wantErr      error
	}{
		{name: "valid tokens return expiries", accessToken: issued.AccessToken, refreshToken: refreshToken},
		{name: "missing access token is unauthorized", refreshToken: refreshToken, wantErr: handlerutil.ErrUnauthorized},
		{name: "invalid access token is unauthorized", accessToken: "not-a-jwt", refreshToken: refreshToken, wantErr: handlerutil.ErrUnauthorized},
		{name: "missing refresh token is unauthorized", accessToken: issued.AccessToken, wantErr: handlerutil.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := svc.Session(context.Background(), tt.accessToken, tt.refreshToken)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, userID, session.UserID)
			require.Equal(t, "Student", session.Username)
			require.Equal(t, "student@example.com", session.Email)
			require.Equal(t, now.Add(accessTokenLifetime), session.AccessTokenExpiresAt)
			require.Equal(t, now.Add(refreshTokenLifetime), session.RefreshTokenExpiresAt)
		})
	}
}

func TestServiceOAuthFlow(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	accountID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	provider := &fakeOAuthProvider{
		claims: GoogleIDTokenClaims{
			Email:            "student@example.com",
			EmailVerified:    true,
			Name:             "Student",
			RegisteredClaims: jwt.RegisteredClaims{Subject: "google-subject"},
		},
	}
	repo := &fakeAuthRepository{
		oauthUser: OAuthUserRecord{UserID: userID, OAuthAccountID: accountID},
		profile:   testUserProfile(userID),
	}
	svc := NewService(repo, ServiceConfig{
		Secret:               "test-secret",
		Environment:          EnvironmentDev,
		Now:                  func() time.Time { return now },
		OAuthProvider:        provider,
		RedirectURLAllowlist: []string{"http://localhost:5173"},
	}, nil)

	begin, err := svc.BeginOAuth(t.Context(), BeginOAuthParams{
		Provider:    "google",
		RedirectURL: "http://localhost:5173/courses",
	})
	require.NoError(t, err)
	require.True(t, repo.createdState)
	require.Contains(t, begin.AuthURL, "state=")
	require.Contains(t, begin.AuthURL, "code_challenge=")

	complete, err := svc.CompleteOAuth(t.Context(), CompleteOAuthParams{
		Provider: "google",
		Code:     "auth-code",
		State:    stateFromURL(t, begin.AuthURL),
	})
	require.NoError(t, err)
	require.Equal(t, "http://localhost:5173/courses", complete.RedirectURL)
	require.Equal(t, userID, complete.Session.UserID)
	require.Equal(t, "Student", complete.Session.Username)
	require.Equal(t, "student@example.com", complete.Session.Email)
	require.Equal(t, "auth-code", provider.exchangedCode)
	require.Equal(t, repo.oauthState.CodeVerifier, provider.exchangedVerifier)
	require.NotEmpty(t, complete.Session.AccessToken)
	require.NotEmpty(t, complete.Session.RefreshToken)
}

func TestServiceBeginOAuthRejectsUnallowedRedirect(t *testing.T) {
	svc := NewService(&fakeAuthRepository{}, ServiceConfig{
		Secret:               "test-secret",
		Environment:          EnvironmentDev,
		OAuthProvider:        &fakeOAuthProvider{},
		RedirectURLAllowlist: []string{"http://localhost:5173"},
	}, nil)

	_, err := svc.BeginOAuth(t.Context(), BeginOAuthParams{
		Provider:    "google",
		RedirectURL: "https://evil.example.com",
	})
	require.ErrorIs(t, err, errInvalidRedirectURL)
}

type fakeOAuthProvider struct {
	claims            GoogleIDTokenClaims
	exchangedCode     string
	exchangedVerifier string
}

func (p *fakeOAuthProvider) Name() string { return "google" }

func (p *fakeOAuthProvider) AuthCodeURL(state, codeVerifier string) string {
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state + "&code_challenge=" + codeVerifier
}

func (p *fakeOAuthProvider) ExchangeIDToken(ctx context.Context, code, codeVerifier string) (string, error) {
	p.exchangedCode = code
	p.exchangedVerifier = codeVerifier
	return "id-token", nil
}

func (p *fakeOAuthProvider) VerifyIDToken(ctx context.Context, rawToken string) (GoogleIDTokenClaims, error) {
	return p.claims, nil
}

func stateFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Query().Get("state")
}

type fakeAuthRepository struct {
	record        RefreshTokenRecord
	oauthState    OAuthLoginStateRecord
	oauthUser     OAuthUserRecord
	profile       UserProfile
	findErr       error
	consumeErr    error
	oauthUserErr  error
	revokeErr     error
	rotateErr     error
	rotated       bool
	revoked       bool
	reuseDetected bool
	createdState  bool
}

func (r *fakeAuthRepository) CreateRefreshSession(ctx context.Context, params CreateRefreshSessionParams) (RefreshTokenRecord, error) {
	return RefreshTokenRecord{
		ID:              uuid.New(),
		FamilyID:        uuid.New(),
		UserID:          params.UserID,
		Hash:            params.TokenHash,
		IsCurrent:       true,
		IssuedAt:        params.Now,
		FamilyExpiresAt: params.FamilyExpiresAt,
	}, nil
}

func (r *fakeAuthRepository) FindRefreshToken(ctx context.Context, tokenHash []byte) (RefreshTokenRecord, error) {
	if r.findErr != nil {
		return RefreshTokenRecord{}, r.findErr
	}
	if string(tokenHash) != string(r.record.Hash) {
		return RefreshTokenRecord{}, errRefreshTokenNotFound
	}
	return r.record, nil
}

func (r *fakeAuthRepository) RotateRefreshToken(ctx context.Context, params RotateRefreshTokenParams) (RefreshTokenRecord, error) {
	r.rotated = true
	if r.rotateErr != nil {
		return RefreshTokenRecord{}, r.rotateErr
	}
	return RefreshTokenRecord{
		ID:              uuid.New(),
		FamilyID:        params.OldToken.FamilyID,
		UserID:          params.OldToken.UserID,
		Hash:            params.NewTokenHash,
		IsCurrent:       true,
		IssuedAt:        params.Now,
		FamilyExpiresAt: params.OldToken.FamilyExpiresAt,
	}, nil
}

func (r *fakeAuthRepository) RevokeRefreshFamily(ctx context.Context, tokenHash []byte, now time.Time, reason string) error {
	r.revoked = true
	return r.revokeErr
}

func (r *fakeAuthRepository) MarkReuseDetected(ctx context.Context, familyID uuid.UUID, now time.Time) error {
	r.reuseDetected = true
	return nil
}

func (r *fakeAuthRepository) CreateOAuthLoginState(ctx context.Context, params CreateOAuthStateParams) error {
	r.createdState = true
	r.oauthState = OAuthLoginStateRecord{
		StateHash:    params.StateHash,
		Provider:     params.Provider,
		CodeVerifier: params.CodeVerifier,
		RedirectURL:  params.RedirectURL,
		ExpiresAt:    params.ExpiresAt,
	}
	return nil
}

func (r *fakeAuthRepository) ConsumeOAuthLoginState(ctx context.Context, stateHash []byte, now time.Time) (OAuthLoginStateRecord, error) {
	if r.consumeErr != nil {
		return OAuthLoginStateRecord{}, r.consumeErr
	}
	if string(stateHash) != string(r.oauthState.StateHash) {
		return OAuthLoginStateRecord{}, errOAuthStateNotFound
	}
	return r.oauthState, nil
}

func (r *fakeAuthRepository) FindOrCreateOAuthUser(ctx context.Context, identity OAuthIdentity) (OAuthUserRecord, error) {
	if r.oauthUserErr != nil {
		return OAuthUserRecord{}, r.oauthUserErr
	}
	if r.oauthUser.UserID == uuid.Nil {
		r.oauthUser = OAuthUserRecord{UserID: uuid.New(), OAuthAccountID: uuid.New()}
	}
	return r.oauthUser, nil
}

func (r *fakeAuthRepository) GetUserProfile(ctx context.Context, userID uuid.UUID) (UserProfile, error) {
	if r.profile.ID == uuid.Nil {
		r.profile = testUserProfile(userID)
	}
	return r.profile, nil
}

func testUserProfile(userID uuid.UUID) UserProfile {
	return UserProfile{
		ID:       userID,
		Username: "Student",
		Email:    "student@example.com",
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
