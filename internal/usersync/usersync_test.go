package usersync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"lumo-payments-svc/internal/gatewayclient"
	"lumo-payments-svc/internal/models"
)

// speedUpRetries shrinks the real retry delays (1s, 2s) down to
// near-instant for the duration of a test, restoring them afterward --
// legitimate whitebox tweak since this test file is in the same package.
func speedUpRetries(t *testing.T) {
	t.Helper()
	orig := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = orig })
}

func TestSyncSubscriptionType_SucceedsOnFirstAttempt(t *testing.T) {
	var attempts int32
	var gotPath, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		gotPath = r.URL.Path
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(gatewayclient.New(srv.URL, "secret"))
	ok := s.SyncSubscriptionType(context.Background(), "user_123", models.PlanPro)

	if !ok {
		t.Fatal("SyncSubscriptionType() = false, want true")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (should succeed without retrying)", got)
	}
	if gotPath != "/internal/users/user_123/subscription" {
		t.Errorf("path = %q, want %q", gotPath, "/internal/users/user_123/subscription")
	}
	if gotBody != `{"subscriptionType":"PRO_USER"}` {
		t.Errorf("body = %q, want %q", gotBody, `{"subscriptionType":"PRO_USER"}`)
	}
}

func TestSyncSubscriptionType_PlanFree_SendsRegularUser(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(gatewayclient.New(srv.URL, "secret"))
	s.SyncSubscriptionType(context.Background(), "user_123", models.PlanFree)

	if gotBody != `{"subscriptionType":"REGULAR_USER"}` {
		t.Errorf("body = %q, want %q", gotBody, `{"subscriptionType":"REGULAR_USER"}`)
	}
}

func TestSyncSubscriptionType_RetriesThenSucceeds(t *testing.T) {
	speedUpRetries(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(gatewayclient.New(srv.URL, "secret"))
	ok := s.SyncSubscriptionType(context.Background(), "user_123", models.PlanPro)

	if !ok {
		t.Fatal("SyncSubscriptionType() = false, want true (should succeed on the 3rd attempt)")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestSyncSubscriptionType_AllAttemptsFail_ReturnsFalse(t *testing.T) {
	speedUpRetries(t)

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := New(gatewayclient.New(srv.URL, "secret"))
	ok := s.SyncSubscriptionType(context.Background(), "user_123", models.PlanPro)

	if ok {
		t.Fatal("SyncSubscriptionType() = true, want false after all attempts fail")
	}
	if got := atomic.LoadInt32(&attempts); got != maxAttempts {
		t.Errorf("attempts = %d, want %d (maxAttempts)", got, maxAttempts)
	}
}

func TestSyncSubscriptionType_NetworkError_RetriesAndReturnsFalse(t *testing.T) {
	speedUpRetries(t)

	s := New(gatewayclient.New("http://127.0.0.1:1", "secret"))
	ok := s.SyncSubscriptionType(context.Background(), "user_123", models.PlanPro)

	if ok {
		t.Fatal("SyncSubscriptionType() = true, want false on a persistent connection error")
	}
}
