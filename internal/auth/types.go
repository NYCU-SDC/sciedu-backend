package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	EnvironmentDev  = "dev"
	EnvironmentProd = "prod"

	accessTokenCookieName  = "access_token"
	refreshTokenCookieName = "refresh_token"

	accessTokenLifetime  = 15 * time.Minute
	refreshTokenLifetime = 30 * 24 * time.Hour
	clockSkew            = time.Minute

	defaultCookieDomain = "sciedu.sdc.nycu.club"
	defaultSecret       = "default-secret"
)

var (
	errRefreshTokenNotFound = errors.New("refresh token not found")
	errOAuthStateNotFound   = errors.New("oauth state not found")
	errOAuthNotConfigured   = errors.New("oauth provider not configured")
	errInvalidOAuthState    = errors.New("invalid oauth state")
	errInvalidRedirectURL   = errors.New("invalid redirect url")
	errInvalidIDToken       = errors.New("invalid id token")
	ErrRefreshReuseDetected = errors.New("refresh token reuse detected")
)

type Session struct {
	UserID                uuid.UUID `json:"-"`
	AccessToken           string    `json:"-"`
	RefreshToken          string    `json:"-"`
	Username              string    `json:"username"`
	Email                 string    `json:"email"`
	AccessTokenExpiresAt  time.Time `json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt time.Time `json:"refreshTokenExpiresAt"`
}

type UserProfile struct {
	ID       uuid.UUID
	Username string
	Email    string
}

type IssueSessionParams struct {
	UserID         uuid.UUID
	OAuthAccountID *uuid.UUID
	IPAddress      string
	UserAgent      string
}

type RefreshTokenRecord struct {
	ID              uuid.UUID
	FamilyID        uuid.UUID
	UserID          uuid.UUID
	Hash            []byte
	IsCurrent       bool
	IssuedAt        time.Time
	UsedAt          *time.Time
	RevokedAt       *time.Time
	FamilyExpiresAt time.Time
	FamilyRevokedAt *time.Time
}

type CreateRefreshSessionParams struct {
	UserID          uuid.UUID
	OAuthAccountID  *uuid.UUID
	TokenHash       []byte
	FamilyExpiresAt time.Time
	IPAddress       string
	UserAgent       string
	Now             time.Time
}

type RotateRefreshTokenParams struct {
	OldToken     RefreshTokenRecord
	NewTokenHash []byte
	Now          time.Time
}

type BeginOAuthParams struct {
	Provider    string
	RedirectURL string
	IPAddress   string
	UserAgent   string
}

type BeginOAuthResult struct {
	AuthURL string
}

type CompleteOAuthParams struct {
	Provider  string
	Code      string
	State     string
	IPAddress string
	UserAgent string
}

type CompleteOAuthResult struct {
	Session     Session
	RedirectURL string
}

type OAuthLoginStateRecord struct {
	StateHash    []byte
	Provider     string
	CodeVerifier string
	RedirectURL  string
	ExpiresAt    time.Time
	UsedAt       *time.Time
}

type CreateOAuthStateParams struct {
	StateHash    []byte
	Provider     string
	CodeVerifier string
	RedirectURL  string
	ExpiresAt    time.Time
	IPAddress    string
	UserAgent    string
	Now          time.Time
}

type OAuthIdentity struct {
	Provider       string
	ProviderUserID string
	Email          string
	EmailVerified  bool
	Name           string
	AvatarURL      string
	Now            time.Time
}

type OAuthUserRecord struct {
	UserID         uuid.UUID
	OAuthAccountID uuid.UUID
}
