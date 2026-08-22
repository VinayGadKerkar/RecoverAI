package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyMerchantID contextKey = "merchant_id"
	ContextKeyUserRole   contextKey = "user_role"
)

// JWTClaims represents the payload of a RecoverAI JWT.
type JWTClaims struct {
	MerchantID string `json:"merchant_id"`
	Role       string `json:"role"` // "merchant" | "admin"
	jwt.RegisteredClaims
}

// JWTAuth returns a middleware that validates Bearer JWTs and injects claims into context.
func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"missing or invalid authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims := &JWTClaims{}

			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})

			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ContextKeyMerchantID, claims.MerchantID)
			ctx = context.WithValue(ctx, ContextKeyUserRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MerchantIDFromContext extracts the merchant ID injected by JWTAuth.
func MerchantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyMerchantID).(string)
	return v
}
