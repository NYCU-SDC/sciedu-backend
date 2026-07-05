package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadGoogleOAuthCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client_secret.json")
	err := os.WriteFile(path, []byte(`{
		"web": {
			"client_id": "client-id",
			"client_secret": "client-secret",
			"redirect_uris": ["http://localhost:8080/api/auth/callback"]
		}
	}`), 0o600)
	require.NoError(t, err)

	credentials, err := LoadGoogleOAuthCredentials(path)
	require.NoError(t, err)
	require.Equal(t, "client-id", credentials.ClientID)
	require.Equal(t, "client-secret", credentials.ClientSecret)
	require.Equal(t, []string{"http://localhost:8080/api/auth/callback"}, credentials.RedirectURIs)
}
