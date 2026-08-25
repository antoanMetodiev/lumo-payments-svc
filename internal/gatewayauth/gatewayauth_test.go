package gatewayauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

var userScoped = []string{"/payments"}
var internalPaths = []string{"/internal"}

func newDownstream(t *testing.T, wantCalled bool, wantClerkUserID string) http.Handler {
	t.Helper()
	called := false
	t.Cleanup(func() {
		if called != wantCalled {
			t.Errorf("downstream handler called = %v, want %v", called, wantCalled)
		}
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := ClerkUserID(r); got != wantClerkUserID {
			t.Errorf("ClerkUserID(r) = %q, want %q", got, wantClerkUserID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

// --- Public paths (neither user-scoped nor internal): always pass through ---

func TestMiddleware_PublicPath_PassesThroughWithoutHeaders(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, true, ""))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// --- User-scoped paths ("/payments"): require both headers, and expose ClerkUserID ---

func TestMiddleware_UserScopedPath_MissingInternalKey_Unauthorized(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, false, ""))

	req := httptest.NewRequest(http.MethodGet, "/payments/subscription", nil)
	req.Header.Set(ClerkUserIDHeader, "user_123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when X-Internal-Api-Key is absent", rec.Code)
	}
}

func TestMiddleware_UserScopedPath_WrongInternalKey_Unauthorized(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, false, ""))

	req := httptest.NewRequest(http.MethodGet, "/payments/subscription", nil)
	req.Header.Set(InternalAPIKeyHdr, "wrong-key")
	req.Header.Set(ClerkUserIDHeader, "user_123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a wrong X-Internal-Api-Key", rec.Code)
	}
}

func TestMiddleware_UserScopedPath_CorrectKeyButNoClerkUserID_Unauthorized(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, false, ""))

	req := httptest.NewRequest(http.MethodGet, "/payments/subscription", nil)
	req.Header.Set(InternalAPIKeyHdr, "supersecret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when X-Clerk-User-Id is absent", rec.Code)
	}
}

func TestMiddleware_UserScopedPath_BlankClerkUserID_Unauthorized(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, false, ""))

	req := httptest.NewRequest(http.MethodGet, "/payments/subscription", nil)
	req.Header.Set(InternalAPIKeyHdr, "supersecret")
	req.Header.Set(ClerkUserIDHeader, "   ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a whitespace-only X-Clerk-User-Id", rec.Code)
	}
}

func TestMiddleware_UserScopedPath_ValidAuth_PassesAndExposesClerkUserID(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, true, "user_abc123"))

	req := httptest.NewRequest(http.MethodGet, "/payments/subscription", nil)
	req.Header.Set(InternalAPIKeyHdr, "supersecret")
	req.Header.Set(ClerkUserIDHeader, "user_abc123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// --- Internal paths ("/internal"): only the internal key is required, no
// Clerk user id ends up in the request context. ---

func TestMiddleware_InternalPath_MissingKey_Unauthorized(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, false, ""))

	req := httptest.NewRequest(http.MethodDelete, "/internal/subscriptions/user_123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when X-Internal-Api-Key is absent", rec.Code)
	}
}

func TestMiddleware_InternalPath_CorrectKey_PassesWithoutRequiringClerkUserID(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, true, ""))

	req := httptest.NewRequest(http.MethodDelete, "/internal/subscriptions/user_123", nil)
	req.Header.Set(InternalAPIKeyHdr, "supersecret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (internal paths don't need X-Clerk-User-Id)", rec.Code)
	}
}

// --- Path matching: exact and "/prefix/..." subpaths, not a naive prefix ---

func TestMiddleware_SubPathUnderUserScopedPrefix_AlsoRequiresAuth(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, false, ""))

	req := httptest.NewRequest(http.MethodGet, "/payments/checkout-session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestMiddleware_SimilarButUnlistedPath_NotTreatedAsProtected guards against
// a naive strings.HasPrefix implementation (without the "/" boundary) that
// would wrongly treat "/payments-extra" as under "/payments".
func TestMiddleware_SimilarButUnlistedPath_NotTreatedAsProtected(t *testing.T) {
	handler := Middleware("supersecret", userScoped, internalPaths)(newDownstream(t, true, ""))

	req := httptest.NewRequest(http.MethodGet, "/payments-extra", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (this path isn't actually under /payments)", rec.Code)
	}
}

// TestClerkUserID_OutsideMiddleware_ReturnsEmpty makes sure reading the
// context value on a request that never went through Middleware doesn't
// panic and just yields the zero value.
func TestClerkUserID_OutsideMiddleware_ReturnsEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := ClerkUserID(req); got != "" {
		t.Errorf("ClerkUserID() = %q, want empty string outside the middleware", got)
	}
}
