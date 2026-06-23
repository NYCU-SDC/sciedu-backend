package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestGoogleIDTokenVerifierVerify(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	kid := "test-key"
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(jwksResponse{
			Keys: []jwkKey{jwkFromPublicKey(kid, &key.PublicKey)},
		}))
	}))
	defer jwksServer.Close()

	verifier := NewGoogleIDTokenVerifier(GoogleIDTokenVerifierConfig{
		Audience: "client-id.apps.googleusercontent.com",
		JWKSURL:  jwksServer.URL,
		Now:      func() time.Time { return now },
	})

	tests := []struct {
		name    string
		claims  GoogleIDTokenClaims
		wantErr bool
	}{
		{
			name: "valid google id token",
			claims: GoogleIDTokenClaims{
				Email:         "student@example.com",
				EmailVerified: true,
				Name:          "Student",
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "google-subject",
					Issuer:    "https://accounts.google.com",
					Audience:  []string{"client-id.apps.googleusercontent.com"},
					ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					IssuedAt:  jwt.NewNumericDate(now),
				},
			},
		},
		{
			name: "wrong audience rejected",
			claims: GoogleIDTokenClaims{
				Email:         "student@example.com",
				EmailVerified: true,
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "google-subject",
					Issuer:    "https://accounts.google.com",
					Audience:  []string{"other-client"},
					ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					IssuedAt:  jwt.NewNumericDate(now),
				},
			},
			wantErr: true,
		},
		{
			name: "unverified email rejected",
			claims: GoogleIDTokenClaims{
				Email: "student@example.com",
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "google-subject",
					Issuer:    "https://accounts.google.com",
					Audience:  []string{"client-id.apps.googleusercontent.com"},
					ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					IssuedAt:  jwt.NewNumericDate(now),
				},
			},
			wantErr: true,
		},
		{
			name: "wrong issuer rejected",
			claims: GoogleIDTokenClaims{
				Email:         "student@example.com",
				EmailVerified: true,
				RegisteredClaims: jwt.RegisteredClaims{
					Subject:   "google-subject",
					Issuer:    "https://evil.example.com",
					Audience:  []string{"client-id.apps.googleusercontent.com"},
					ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
					IssuedAt:  jwt.NewNumericDate(now),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawToken := jwt.NewWithClaims(jwt.SigningMethodRS256, tt.claims)
			rawToken.Header["kid"] = kid
			signed, err := rawToken.SignedString(key)
			require.NoError(t, err)

			got, err := verifier.Verify(context.Background(), signed)
			if tt.wantErr {
				require.ErrorIs(t, err, errInvalidIDToken)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.claims.Subject, got.Subject)
			require.Equal(t, tt.claims.Email, got.Email)
			require.True(t, got.EmailVerified)
		})
	}
}

func jwkFromPublicKey(kid string, key *rsa.PublicKey) jwkKey {
	return jwkKey{
		Kty: "RSA",
		Alg: "RS256",
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}
