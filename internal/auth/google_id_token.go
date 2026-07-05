package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

type GoogleIDTokenVerifierConfig struct {
	Audience   string
	JWKSURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

type GoogleIDTokenVerifier struct {
	audience   string
	jwksURL    string
	httpClient *http.Client
	now        func() time.Time
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
}

type GoogleIDTokenClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	jwt.RegisteredClaims
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func NewGoogleIDTokenVerifier(config GoogleIDTokenVerifierConfig) *GoogleIDTokenVerifier {
	if config.JWKSURL == "" {
		config.JWKSURL = googleJWKSURL
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &GoogleIDTokenVerifier{
		audience:   config.Audience,
		jwksURL:    config.JWKSURL,
		httpClient: config.HTTPClient,
		now:        config.Now,
		keys:       make(map[string]*rsa.PublicKey),
	}
}

func (v *GoogleIDTokenVerifier) Verify(ctx context.Context, rawToken string) (GoogleIDTokenClaims, error) {
	if rawToken == "" || v.audience == "" {
		return GoogleIDTokenClaims{}, errInvalidIDToken
	}

	claims := &GoogleIDTokenClaims{}
	parser := jwt.NewParser(
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(clockSkew),
		jwt.WithTimeFunc(func() time.Time { return v.now().UTC() }),
	)
	token, err := parser.ParseWithClaims(rawToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Header["alg"])
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, errors.New("missing key id")
		}
		return v.key(ctx, kid)
	})
	if err != nil || !token.Valid {
		return GoogleIDTokenClaims{}, errInvalidIDToken
	}
	if !isAllowedGoogleIssuer(claims.Issuer) {
		return GoogleIDTokenClaims{}, errInvalidIDToken
	}
	if claims.ExpiresAt == nil {
		return GoogleIDTokenClaims{}, errInvalidIDToken
	}
	if claims.Subject == "" || claims.Email == "" || !claims.EmailVerified {
		return GoogleIDTokenClaims{}, errInvalidIDToken
	}
	return *claims, nil
}

func (v *GoogleIDTokenVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key := v.keys[kid]
	v.mu.RUnlock()
	if key != nil {
		return key, nil
	}

	// Google rotates signing keys; fetch JWKS lazily when a token references an unknown kid.
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	key = v.keys[kid]
	v.mu.RUnlock()
	if key == nil {
		return nil, errors.New("unknown key id")
	}
	return key, nil
}

func (v *GoogleIDTokenVerifier) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch google jwks: status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, item := range jwks.Keys {
		if item.Kty != "RSA" || item.Kid == "" {
			continue
		}
		key, err := rsaPublicKeyFromJWK(item)
		if err != nil {
			return err
		}
		keys[item.Kid] = key
	}
	v.mu.Lock()
	v.keys = keys
	v.mu.Unlock()
	return nil
}

func rsaPublicKeyFromJWK(key jwkKey) (*rsa.PublicKey, error) {
	modulus, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, err
	}
	exponent, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, err
	}

	e := 0
	for _, b := range exponent {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, errors.New("invalid rsa exponent")
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: e,
	}, nil
}

func isAllowedGoogleIssuer(issuer string) bool {
	return issuer == "https://accounts.google.com" || issuer == "accounts.google.com"
}
