package auth

import (
	"encoding/json"
	"os"
)

type GoogleOAuthCredentials struct {
	ClientID     string
	ClientSecret string
	RedirectURIs []string
}

type googleOAuthCredentialsFile struct {
	Web       googleOAuthCredentialBlock `json:"web"`
	Installed googleOAuthCredentialBlock `json:"installed"`
}

type googleOAuthCredentialBlock struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURIs []string `json:"redirect_uris"`
}

func LoadGoogleOAuthCredentials(path string) (GoogleOAuthCredentials, error) {
	file, err := os.Open(path)
	if err != nil {
		return GoogleOAuthCredentials{}, err
	}
	defer func() {
		_ = file.Close()
	}()

	var parsed googleOAuthCredentialsFile
	if err := json.NewDecoder(file).Decode(&parsed); err != nil {
		return GoogleOAuthCredentials{}, err
	}

	block := parsed.Web
	if block.ClientID == "" {
		block = parsed.Installed
	}
	return GoogleOAuthCredentials(block), nil
}
