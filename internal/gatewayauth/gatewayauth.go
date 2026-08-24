// Package gatewayauth mirrors GatewayAuthenticationFilter + SecurityConfig:
// trusts headers set by lumo-api-gateway instead of verifying a Clerk
// session JWT itself — the gateway is the single place that talks to
// Clerk's JWKS. Two kinds of protected paths:
//
//   - userScopedPrefixes ("/payments"): a real end user is asking about their
//     own billing. Requires X-Internal-Api-Key (proves the caller is the
//     gateway) AND X-Clerk-User-Id (the Clerk id the gateway already
//     verified) — the latter becomes the request's Clerk user id, read back
//     via ClerkUserID.
//   - internalPrefixes ("/internal"): a service-to-service call with no live
//     end user behind it (e.g. lumo-user-svc telling us an account was
//     deleted) — only X-Internal-Api-Key is required; the target user is a
//     path variable the handler reads directly.
//
// X-Internal-Api-Key is the only thing standing between "trust whatever
// these headers say" and full impersonation, since this service still has a
// public URL for now (no private network yet) — a stopgap until it does.
//
// Public endpoints (the Stripe webhook, heartbeat, health) skip this
// entirely — see main.go's route registration.
package gatewayauth

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	ClerkUserIDHeader = "X-Clerk-User-Id"
	InternalAPIKeyHdr = "X-Internal-Api-Key"
)

type contextKey int

const clerkUserIDKey contextKey = iota

// ClerkUserID returns the Clerk user id Middleware verified for this
// request, or "" if the request wasn't user-scoped.
func ClerkUserID(r *http.Request) string {
	v, _ := r.Context().Value(clerkUserIDKey).(string)
	return v
}

// Middleware mirrors GatewayAuthenticationFilter.doFilterInternal.
func Middleware(internalSecret string, userScopedPrefixes, internalPrefixes []string) func(http.Handler) http.Handler {
	secret := []byte(internalSecret)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uri := r.URL.Path
			isUserScoped := matches(uri, userScopedPrefixes)
			isInternal := matches(uri, internalPrefixes)

			if !isUserScoped && !isInternal {
				next.ServeHTTP(w, r)
				return
			}

			if !isFromGateway(r, secret) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if isUserScoped {
				clerkUserID := r.Header.Get(ClerkUserIDHeader)
				if strings.TrimSpace(clerkUserID) == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				r = r.WithContext(context.WithValue(r.Context(), clerkUserIDKey, clerkUserID))
			}

			next.ServeHTTP(w, r)
		})
	}
}

func matches(uri string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if uri == prefix || strings.HasPrefix(uri, prefix+"/") {
			return true
		}
	}
	return false
}

// Constant-time comparison so a mistyped/guessed key can't be brute-forced
// faster by timing how quickly a naive comparison bails out on the first
// mismatched byte.
func isFromGateway(r *http.Request, internalSecret []byte) bool {
	provided := r.Header.Get(InternalAPIKeyHdr)
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), internalSecret) == 1
}
