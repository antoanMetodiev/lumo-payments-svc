package checkout

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createCheckoutSession validates the request body (JSON shape, then email
// format) before ever touching h.svc, so these are safe to exercise with a
// nil Service -- a valid-email request would reach h.svc.CreateCheckoutSessionURL
// and panic against the nil Stripe/repo fields, so that path isn't tested here.

func newHandlersWithNilSvc() *Handlers {
	return &Handlers{svc: nil}
}

func TestCreateCheckoutSession_InvalidJSONBody_Returns400(t *testing.T) {
	h := newHandlersWithNilSvc()
	r := httptest.NewRequest(http.MethodPost, "/payments/checkout-session", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()

	h.createCheckoutSession(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for malformed JSON", rec.Code)
	}
}

func TestCreateCheckoutSession_MissingEmail_Returns400(t *testing.T) {
	h := newHandlersWithNilSvc()
	r := httptest.NewRequest(http.MethodPost, "/payments/checkout-session", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	h.createCheckoutSession(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when email is missing", rec.Code)
	}
}

func TestCreateCheckoutSession_InvalidEmailFormat_Returns400(t *testing.T) {
	h := newHandlersWithNilSvc()
	r := httptest.NewRequest(http.MethodPost, "/payments/checkout-session", strings.NewReader(`{"email":"not-an-email"}`))
	rec := httptest.NewRecorder()

	h.createCheckoutSession(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an invalid email address", rec.Code)
	}
}
