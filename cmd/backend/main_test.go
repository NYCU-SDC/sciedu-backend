package main

import (
	"testing"

	"sciedu-backend/internal/auth"
	"sciedu-backend/internal/config"

	"github.com/stretchr/testify/require"
)

func TestParseAllowOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins string
		want    []string
	}{
		{
			name:    "empty",
			origins: "",
		},
		{
			name:    "trims whitespace and trailing slash",
			origins: " http://localhost:5173/ , *.sciedu.sdc.nycu.club ",
			want:    []string{"http://localhost:5173", "*.sciedu.sdc.nycu.club"},
		},
		{
			name:    "ignores empty parts",
			origins: ", http://localhost:5173/,,",
			want:    []string{"http://localhost:5173"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parseAllowOrigins(tt.origins))
		})
	}
}

func TestInitGoogleOAuthProvider(t *testing.T) {
	tests := []struct {
		name         string
		config       config.Config
		wantProvider bool
		wantError    bool
	}{
		{
			name: "development without credentials",
			config: config.Config{
				Environment:            auth.EnvironmentDev,
				GoogleOAuthRedirectURL: "http://localhost:8080/api/auth/callback",
			},
		},
		{
			name: "development with credentials",
			config: config.Config{
				Environment:             auth.EnvironmentDev,
				GoogleOAuthClientID:     "client-id",
				GoogleOAuthClientSecret: "client-secret",
				GoogleOAuthRedirectURL:  "http://localhost:8080/api/auth/callback",
			},
			wantProvider: true,
		},
		{
			name: "development with partial credentials",
			config: config.Config{
				Environment:            auth.EnvironmentDev,
				GoogleOAuthClientID:    "client-id",
				GoogleOAuthRedirectURL: "http://localhost:8080/api/auth/callback",
			},
			wantError: true,
		},
		{
			name: "production configuration remains required",
			config: config.Config{
				Environment:            auth.EnvironmentProd,
				GoogleOAuthRedirectURL: "https://api.example.com/api/auth/callback",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := initGoogleOAuthProvider(tt.config)

			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, provider)
				return
			}
			require.NoError(t, err)
			if tt.wantProvider {
				require.NotNil(t, provider)
			} else {
				require.Nil(t, provider)
			}
		})
	}
}
