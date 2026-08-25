package checkout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"lumo-payments-svc/internal/gatewayclient"
)

// --- isProUser: pure aside from the gatewayclient call, fully exercisable
// against a test HTTP server -- no Stripe or database involved. ---

func TestIsProUser_SubscriptionTypeProUser_ReturnsTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subscriptionType":"PRO_USER"}`))
	}))
	defer srv.Close()

	s := &Service{gateway: gatewayclient.New(srv.URL, "secret")}
	if !s.isProUser(context.Background(), "user_123") {
		t.Error("isProUser() = false, want true for subscriptionType=PRO_USER")
	}
}

func TestIsProUser_OtherSubscriptionType_ReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"subscriptionType":"REGULAR_USER"}`))
	}))
	defer srv.Close()

	s := &Service{gateway: gatewayclient.New(srv.URL, "secret")}
	if s.isProUser(context.Background(), "user_123") {
		t.Error("isProUser() = true, want false for subscriptionType=REGULAR_USER")
	}
}

func TestIsProUser_NonOKStatus_TreatedAsNotPro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := &Service{gateway: gatewayclient.New(srv.URL, "secret")}
	if s.isProUser(context.Background(), "user_123") {
		t.Error("isProUser() = true, want false on a non-200 response (transient blip, not a Pro confirmation)")
	}
}

func TestIsProUser_NetworkError_TreatedAsNotPro(t *testing.T) {
	s := &Service{gateway: gatewayclient.New("http://127.0.0.1:1", "secret")}
	if s.isProUser(context.Background(), "user_123") {
		t.Error("isProUser() = true, want false on a connection error")
	}
}

func TestIsProUser_RequestsCorrectPath(t *testing.T) {
	var gotPath, gotRawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawPath = r.URL.EscapedPath()
		w.Write([]byte(`{"subscriptionType":"PRO_USER"}`))
	}))
	defer srv.Close()

	s := &Service{gateway: gatewayclient.New(srv.URL, "secret")}
	s.isProUser(context.Background(), "user with spaces")

	if gotPath != "/internal/users/user with spaces" {
		t.Errorf("decoded path = %q, want the clerkId to round-trip back to %q", gotPath, "/internal/users/user with spaces")
	}
	if gotRawPath != "/internal/users/user%20with%20spaces" {
		t.Errorf("escaped path on the wire = %q, want %q (clerkId must be URL-path-escaped)", gotRawPath, "/internal/users/user%20with%20spaces")
	}
}
