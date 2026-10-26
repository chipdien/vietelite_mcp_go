package middleware

import (
	"net/http"
	"strings"
	"vietelite-mcp/go-server/logger"
)

func AuthMiddleware(authToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authToken == "" {
				next.ServeHTTP(w, r)
				return
			}

			token := r.Header.Get("Authorization")
			if token == "" {
				logger.Warn("AUTH", "Missing Authorization header from %s", r.RemoteAddr)
				http.Error(w, "Unauthorized: Missing token", http.StatusUnauthorized)
				return
			}

			token = strings.TrimPrefix(token, "Bearer ")

			if token != authToken {
				logger.Warn("AUTH", "Invalid token from %s", r.RemoteAddr)
				http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
				return
			}

			logger.Info("AUTH", "Authenticated request from %s", r.RemoteAddr)
			next.ServeHTTP(w, r)
		})
	}
}
