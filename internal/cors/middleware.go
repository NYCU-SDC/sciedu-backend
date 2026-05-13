package cors

import (
	"net/http"
	"strings"

	"go.uber.org/zap"
)

type Middleware struct {
	logger       *zap.Logger
	allowOrigins []string
}

func NewMiddleware(logger *zap.Logger, allowOrigins []string) Middleware {
	logger.Info("CORS middleware initialized", zap.Strings("allow_origins", allowOrigins))
	return Middleware{
		logger:       logger,
		allowOrigins: allowOrigins,
	}
}

func (m Middleware) HandlerFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		if m.isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "3600")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}

// isOriginAllowed checks if the origin matches any allowed pattern
func (m Middleware) isOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for _, allowed := range m.allowOrigins {
		// Exact match with "*"
		if allowed == "*" {
			return true
		}

		// Exact match with full origin
		if allowed == origin {
			return true
		}

		// Wildcard subdomain match (e.g., *.sciedu.sdc.nycu.club)
		if strings.HasPrefix(allowed, "*.") {
			domain := allowed[2:] // Remove "*."

			// Extract hostname from origin (remove protocol)
			hostname := origin
			if idx := strings.Index(origin, "://"); idx != -1 {
				hostname = origin[idx+3:]
			}
			// Remove port if present
			if idx := strings.Index(hostname, ":"); idx != -1 {
				hostname = hostname[:idx]
			}

			// Check if hostname ends with the domain
			if strings.HasSuffix(hostname, domain) {
				// Ensure it's a proper subdomain match
				// e.g., dev.sciedu.sdc.nycu.club matches *.sciedu.sdc.nycu.club
				// but sciedu.sdc.nycu.club does NOT match *.sciedu.sdc.nycu.club
				if len(hostname) > len(domain) && hostname[len(hostname)-len(domain)-1] == '.' {
					return true
				}
			}
		}
	}

	return false
}
