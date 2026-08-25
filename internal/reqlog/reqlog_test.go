package reqlog

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- statusRecorder ---

func TestStatusRecorder_WriteHeaderCapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	sr.WriteHeader(http.StatusNotFound)

	if sr.status != http.StatusNotFound {
		t.Errorf("sr.status = %d, want %d", sr.status, http.StatusNotFound)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("underlying recorder Code = %d, want %d (WriteHeader must still reach the wrapped ResponseWriter)", rec.Code, http.StatusNotFound)
	}
}

// --- Middleware ---

func TestMiddleware_PassesRequestThroughToNextHandler(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("hello"))
	})

	req := httptest.NewRequest(http.MethodGet, "/foo?bar=1", nil)
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was never called")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "hello")
	}
}

// TestMiddleware_DefaultsTo200WhenHandlerNeverCallsWriteHeader mirrors the
// standard net/http contract: no explicit WriteHeader call means 200, and
// the wrapping recorder must report that same default rather than a zero
// value.
func TestMiddleware_DefaultsTo200WhenHandlerNeverCallsWriteHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no explicit status set"))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestMiddleware_HandlesRequestWithNoQueryString(t *testing.T) {
	// Just exercising the no-query-string branch (query stays "") without a
	// panic or malformed log line -- nothing externally observable to assert
	// on besides the response still going through untouched.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "/no-query", nil)
	rec := httptest.NewRecorder()
	Middleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}
