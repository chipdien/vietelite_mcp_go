package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	t.Run("no token configured", func(t *testing.T) {
		middleware := AuthMiddleware("")
		req := httptest.NewRequest("GET", "/sse", nil)
		rec := httptest.NewRecorder()
		middleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		middleware := AuthMiddleware("secret123")
		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer secret123")
		rec := httptest.NewRecorder()
		middleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		middleware := AuthMiddleware("secret123")
		req := httptest.NewRequest("GET", "/sse", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		rec := httptest.NewRecorder()
		middleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		middleware := AuthMiddleware("secret123")
		req := httptest.NewRequest("GET", "/sse", nil)
		rec := httptest.NewRecorder()
		middleware(handler).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}
