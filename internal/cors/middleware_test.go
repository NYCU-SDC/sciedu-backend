package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestCORSMiddleware(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	tests := []struct {
		name          string
		allowOrigins  []string
		requestOrigin string
		expectAllowed bool
		description   string
	}{
		{
			name:          "Wildcard subdomain - dev",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "https://dev.sciedu.sdc.nycu.club",
			expectAllowed: true,
			description:   "Should allow dev.sciedu.sdc.nycu.club",
		},
		{
			name:          "Wildcard subdomain - stage",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "https://stage.sciedu.sdc.nycu.club",
			expectAllowed: true,
			description:   "Should allow stage.sciedu.sdc.nycu.club",
		},
		{
			name:          "Wildcard subdomain - api.dev",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "https://api.dev.sciedu.sdc.nycu.club",
			expectAllowed: true,
			description:   "Should allow api.dev.sciedu.sdc.nycu.club",
		},
		{
			name:          "Wildcard subdomain - root domain rejected",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "https://sciedu.sdc.nycu.club",
			expectAllowed: false,
			description:   "Should reject sciedu.sdc.nycu.club (no subdomain)",
		},
		{
			name:          "Exact match - localhost",
			allowOrigins:  []string{"http://localhost:5173"},
			requestOrigin: "http://localhost:5173",
			expectAllowed: true,
			description:   "Should allow exact match localhost:5173",
		},
		{
			name:          "Multiple origins - first match",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club", "http://localhost:5173"},
			requestOrigin: "https://dev.sciedu.sdc.nycu.club",
			expectAllowed: true,
			description:   "Should allow from first pattern",
		},
		{
			name:          "Multiple origins - second match",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club", "http://localhost:5173"},
			requestOrigin: "http://localhost:5173",
			expectAllowed: true,
			description:   "Should allow from second pattern",
		},
		{
			name:          "Malicious domain",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "https://malicious.com",
			expectAllowed: false,
			description:   "Should reject malicious.com",
		},
		{
			name:          "Wildcard all",
			allowOrigins:  []string{"*"},
			requestOrigin: "https://any-domain.com",
			expectAllowed: true,
			description:   "Should allow any origin with *",
		},
		{
			name:          "Empty origin",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "",
			expectAllowed: false,
			description:   "Should reject empty origin",
		},
		{
			name:          "Origin with port",
			allowOrigins:  []string{"*.sciedu.sdc.nycu.club"},
			requestOrigin: "https://dev.sciedu.sdc.nycu.club:8080",
			expectAllowed: true,
			description:   "Should handle origin with port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create middleware
			middleware := NewMiddleware(logger, tt.allowOrigins)

			// Create a test handler
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			})

			// Wrap with CORS middleware
			handler := middleware.HandlerFunc(nextHandler)

			// Create test request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}
			rec := httptest.NewRecorder()

			// Execute
			handler(rec, req)

			// Check CORS headers
			allowOriginHeader := rec.Header().Get("Access-Control-Allow-Origin")

			if tt.expectAllowed {
				if allowOriginHeader == "" {
					t.Errorf("%s: expected CORS header to be set, but it was empty", tt.description)
				} else if allowOriginHeader != tt.requestOrigin {
					t.Errorf("%s: expected Access-Control-Allow-Origin=%s, got %s",
						tt.description, tt.requestOrigin, allowOriginHeader)
				}
			} else {
				if allowOriginHeader != "" {
					t.Errorf("%s: expected no CORS header, but got %s", tt.description, allowOriginHeader)
				}
			}
		})
	}
}

func TestCORSPreflightRequest(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	middleware := NewMiddleware(logger, []string{"*.sciedu.sdc.nycu.club"})

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called for OPTIONS request")
	})

	handler := middleware.HandlerFunc(nextHandler)

	// Create OPTIONS request
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://dev.sciedu.sdc.nycu.club")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	// Execute
	handler(rec, req)

	// Verify preflight response
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status 204 for OPTIONS, got %d", rec.Code)
	}

	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected Access-Control-Allow-Origin header to be set")
	}

	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected Access-Control-Allow-Methods header to be set")
	}

	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("Expected Access-Control-Allow-Headers header to be set")
	}
}
