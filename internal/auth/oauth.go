package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

const (
	googleProviderName = "google"
	googleAuthURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL     = "https://oauth2.googleapis.com/token"
)

type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
	Verifier     *GoogleIDTokenVerifier
}

type GoogleOAuthProvider struct {
	oauth2Config oauth2.Config
	verifier     *GoogleIDTokenVerifier
}

func NewGoogleOAuthProvider(config GoogleOAuthConfig) (*GoogleOAuthProvider, error) {
	if config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "" {
		return nil, errOAuthNotConfigured
	}
	verifier := config.Verifier
	if verifier == nil {
		verifier = NewGoogleIDTokenVerifier(GoogleIDTokenVerifierConfig{
			Audience:   config.ClientID,
			HTTPClient: config.HTTPClient,
		})
	}
	return &GoogleOAuthProvider{
		oauth2Config: oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  config.RedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  googleAuthURL,
				TokenURL: googleTokenURL,
			},
		},
		verifier: verifier,
	}, nil
}

func (p *GoogleOAuthProvider) Name() string {
	return googleProviderName
}

func (p *GoogleOAuthProvider) AuthCodeURL(state, codeVerifier string) string {
	return p.oauth2Config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(codeVerifier),
	)
}

func (p *GoogleOAuthProvider) ExchangeIDToken(ctx context.Context, code, codeVerifier string) (string, error) {
	token, err := p.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return "", fmt.Errorf("exchange oauth code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", errors.New("missing id_token")
	}
	return rawIDToken, nil
}

func (p *GoogleOAuthProvider) VerifyIDToken(ctx context.Context, rawToken string) (GoogleIDTokenClaims, error) {
	return p.verifier.Verify(ctx, rawToken)
}
