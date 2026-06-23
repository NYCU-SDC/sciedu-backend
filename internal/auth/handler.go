package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"go.uber.org/zap"
)

type HandlerService interface {
	BeginOAuth(ctx context.Context, params BeginOAuthParams) (BeginOAuthResult, error)
	CompleteOAuth(ctx context.Context, params CompleteOAuthParams) (CompleteOAuthResult, error)
	Session(ctx context.Context, accessToken, refreshToken string) (Session, error)
	Refresh(ctx context.Context, refreshToken string) (Session, error)
	Logout(ctx context.Context, refreshToken string) error
}

type CookieConfig struct {
	Environment string
	Domain      string
}

type Handler struct {
	service       HandlerService
	cookies       CookieConfig
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
}

func NewHandler(service HandlerService, cookies CookieConfig, logger *zap.Logger) *Handler {
	if cookies.Environment == "" {
		cookies.Environment = EnvironmentProd
	}
	if cookies.Domain == "" && cookies.Environment != EnvironmentDev {
		cookies.Domain = defaultCookieDomain
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		service: service,
		cookies: cookies,
		logger:  logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			if errors.Is(err, ErrRefreshReuseDetected) {
				return problemutil.NewUnauthorizedProblem("You must be logged in to access this resource")
			}
			if errors.Is(err, errOAuthNotConfigured) {
				return problemutil.NewInternalServerProblem("oauth provider is not configured")
			}
			if errors.Is(err, errInvalidOAuthState) || errors.Is(err, errInvalidRedirectURL) {
				return problemutil.NewBadRequestProblem(err.Error())
			}
			if errors.Is(err, errOAuthCodeExchange) {
				return problemutil.NewBadRequestProblem("oauth code exchange failed")
			}
			if errors.Is(err, errInvalidIDToken) {
				return problemutil.NewUnauthorizedProblem("You must be logged in to access this resource")
			}
			return problemutil.Problem{}
		}),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set) {
	handle := func(pattern string, fn http.HandlerFunc) {
		if middlewares != nil {
			fn = middlewares.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	handle("GET /api/auth/session", h.Session)
	handle("POST /api/auth/refresh", h.Refresh)
	handle("POST /api/auth/logout", h.Logout)
	handle("GET /api/login/oauth/google", h.LoginGoogle)
	handle("GET /api/auth/callback", h.Callback)
}

func (h *Handler) LoginGoogle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	result, err := h.service.BeginOAuth(ctx, BeginOAuthParams{
		Provider:    "google",
		RedirectURL: r.URL.Query().Get("r"),
		IPAddress:   clientIP(r),
		UserAgent:   r.UserAgent(),
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	http.Redirect(w, r, result.AuthURL, http.StatusFound)
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	result, err := h.service.CompleteOAuth(ctx, CompleteOAuthParams{
		Provider:  "google",
		Code:      r.URL.Query().Get("code"),
		State:     r.URL.Query().Get("state"),
		IPAddress: clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		h.clearSessionCookies(w)
		if errors.Is(err, errInvalidOAuthState) {
			h.writeUnauthorizedProblem(w)
			return
		}
		logger.Error("failed to complete oauth callback", zap.Error(err))
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	h.setSessionCookies(w, result.Session)
	http.Redirect(w, r, result.RedirectURL, http.StatusFound)
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	accessToken, err := cookieValue(r, accessTokenCookieName)
	if err != nil {
		h.writeUnauthorizedProblem(w)
		return
	}
	refreshToken, err := cookieValue(r, refreshTokenCookieName)
	if err != nil {
		h.writeUnauthorizedProblem(w)
		return
	}

	session, err := h.service.Session(ctx, accessToken, refreshToken)
	if err != nil {
		if errors.Is(err, handlerutil.ErrUnauthorized) {
			h.writeUnauthorizedProblem(w)
			return
		}
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, session)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	refreshToken, err := cookieValue(r, refreshTokenCookieName)
	if err != nil {
		h.clearSessionCookies(w)
		h.writeUnauthorizedProblem(w)
		return
	}

	session, err := h.service.Refresh(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, ErrRefreshReuseDetected) || errors.Is(err, handlerutil.ErrUnauthorized) {
			h.clearSessionCookies(w)
			h.writeUnauthorizedProblem(w)
			return
		}
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	h.setSessionCookies(w, session)
	handlerutil.WriteJSONResponse(w, http.StatusOK, session)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	refreshToken, err := cookieValue(r, refreshTokenCookieName)
	if err != nil && !errors.Is(err, handlerutil.ErrUnauthorized) {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	if err := h.service.Logout(ctx, refreshToken); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	h.clearSessionCookies(w)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) setSessionCookies(w http.ResponseWriter, session Session) {
	http.SetCookie(w, h.accessCookie(session.AccessToken, int(accessTokenLifetime.Seconds())))
	http.SetCookie(w, h.refreshCookie(session.RefreshToken, int(timeUntil(session.RefreshTokenExpiresAt).Seconds())))
}

func (h *Handler) clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, h.accessCookie("", -1))
	http.SetCookie(w, h.refreshCookie("", -1))
}

func (h *Handler) writeUnauthorizedProblem(w http.ResponseWriter) {
	problem := problemutil.NewUnauthorizedProblem("You must be logged in to access this resource")
	data, err := json.Marshal(problem)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	_, _ = w.Write(data)
}

func (h *Handler) accessCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     accessTokenCookieName,
		Value:    value,
		Path:     "/",
		Domain:   h.cookies.Domain,
		HttpOnly: true,
		SameSite: h.cookieSameSite(http.SameSiteLaxMode),
		Secure:   h.cookies.Environment != EnvironmentDev,
		MaxAge:   maxAge,
	}
}

func (h *Handler) refreshCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    value,
		Path:     "/api/auth",
		Domain:   h.cookies.Domain,
		HttpOnly: true,
		SameSite: h.cookieSameSite(http.SameSiteStrictMode),
		Secure:   h.cookies.Environment != EnvironmentDev,
		MaxAge:   maxAge,
	}
}

func (h *Handler) cookieSameSite(prodMode http.SameSite) http.SameSite {
	if h.cookies.Environment == EnvironmentDev {
		return http.SameSiteNoneMode
	}
	return prodMode
}

func cookieValue(r *http.Request, name string) (string, error) {
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", handlerutil.ErrUnauthorized
	}
	return cookie.Value, nil
}

func timeUntil(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ip := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if net.ParseIP(ip) != nil {
			return ip
		}
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if net.ParseIP(r.RemoteAddr) != nil {
			return r.RemoteAddr
		}
		return ""
	}
	if net.ParseIP(host) == nil {
		return ""
	}
	return host
}
