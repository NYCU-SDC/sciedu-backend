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
	ErrRefreshReuseDetected = errors.New("refresh token reuse detected")
)

type Session struct {
	UserID                uuid.UUID `json:"-"`
	AccessToken           string    `json:"-"`
	RefreshToken          string    `json:"-"`
	AccessTokenExpiresAt  time.Time `json:"accessTokenExpiresAt"`
	RefreshTokenExpiresAt time.Time `json:"refreshTokenExpiresAt"`
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
