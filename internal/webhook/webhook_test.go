package webhook

import (
	"encoding/json"
	"errors"
	"io"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v86"
	stripewebhook "github.com/stripe/stripe-go/v86/webhook"
)

const testWebhookSecret = "whsec_test_secret"

// signedEventRequest builds a real, validly-signed Stripe webhook request
// for the given event payload -- webhook.GenerateTestSignedPayload is the
// SDK's own exported test helper for exactly this ("for mocking webhook
// events"), so no fake/reimplemented signature scheme is involved.
func signedEventRequest(t *testing.T, eventType, dataObjectJSON string) *http.Request {
	t.Helper()
	payload := []byte(`{
		"id": "evt_test",
		"object": "event",
		"api_version": "` + stripe.APIVersion + `",
		"type": "` + eventType + `",
		"data": {"object": ` + dataObjectJSON + `}
	}`)

	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: payload,
		Secret:  testWebhookSecret,
	})

	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(signed.Payload)))
	r.Header.Set("Stripe-Signature", signed.Header)
	return r
}

// --- deserializeSubscription ---

func TestDeserializeSubscription_ValidJSON(t *testing.T) {
	event := stripe.Event{Data: &stripe.EventData{Raw: json.RawMessage(`{"id":"sub_123","status":"active"}`)}}

	sub, err := deserializeSubscription(event)
	if err != nil {
		t.Fatalf("deserializeSubscription() error = %v", err)
	}
	if sub.ID != "sub_123" {
		t.Errorf("sub.ID = %q, want %q", sub.ID, "sub_123")
	}
	if sub.Status != stripe.SubscriptionStatusActive {
		t.Errorf("sub.Status = %q, want active", sub.Status)
	}
}

func TestDeserializeSubscription_InvalidJSON_ReturnsError(t *testing.T) {
	event := stripe.Event{Data: &stripe.EventData{Raw: json.RawMessage(`not json`)}}

	if _, err := deserializeSubscription(event); err == nil {
		t.Fatal("deserializeSubscription() error = nil, want an error for malformed JSON")
	}
}

// --- Service.Handle: branches that never touch s.subscriptions are safe to
// exercise with a nil *subscriptions.Service. ---

func TestHandle_InvoicePaymentFailed_DoesNotTouchSubscriptionsService(t *testing.T) {
	s := NewService(nil)
	event := stripe.Event{ID: "evt_1", Type: "invoice.payment_failed"}

	// Must not panic against the nil subscriptions.Service.
	s.Handle(t.Context(), event)
}

func TestHandle_UnknownEventType_DoesNotTouchSubscriptionsService(t *testing.T) {
	s := NewService(nil)
	event := stripe.Event{ID: "evt_2", Type: "checkout.session.completed"}

	s.Handle(t.Context(), event)
}

// TestHandle_SubscriptionUpdated_DeserializeFailure_ReturnsBeforeTouchingRepo
// checks that a malformed data.object is caught (logged) before
// SyncFromStripeSubscription is ever called -- safe against a nil
// subscriptions.Service since that call is never reached.
func TestHandle_SubscriptionUpdated_DeserializeFailure_ReturnsBeforeTouchingRepo(t *testing.T) {
	s := NewService(nil)
	event := stripe.Event{
		ID:   "evt_3",
		Type: "customer.subscription.updated",
		Data: &stripe.EventData{Raw: json.RawMessage(`not json`)},
	}

	s.Handle(t.Context(), event)
}

func TestHandle_SubscriptionDeleted_DeserializeFailure_ReturnsBeforeTouchingRepo(t *testing.T) {
	s := NewService(nil)
	event := stripe.Event{
		ID:   "evt_4",
		Type: "customer.subscription.deleted",
		Data: &stripe.EventData{Raw: json.RawMessage(`not json`)},
	}

	s.Handle(t.Context(), event)
}

// --- Handlers.handleStripeWebhook ---

func TestHandleStripeWebhook_ValidSignature_UntrackedEventType_Returns200(t *testing.T) {
	svc := NewService(nil)
	h := NewHandlers(testWebhookSecret, svc)

	r := signedEventRequest(t, "invoice.payment_failed", `{}`)
	rec := httptest.NewRecorder()

	h.handleStripeWebhook(rec, r)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleStripeWebhook_InvalidSignature_Returns401(t *testing.T) {
	h := NewHandlers(testWebhookSecret, NewService(nil))

	r := signedEventRequest(t, "invoice.payment_failed", `{}`)
	// A current timestamp (so this isn't rejected for staleness instead) with
	// a v1 signature that can't possibly match any payload.
	badSig := "t=" + fmt.Sprint(time.Now().Unix()) + ",v1=" + strings.Repeat("0", 64)
	r.Header.Set("Stripe-Signature", badSig)
	rec := httptest.NewRecorder()

	h.handleStripeWebhook(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a signature that doesn't match", rec.Code)
	}
}

func TestHandleStripeWebhook_WrongSecret_Returns401(t *testing.T) {
	h := NewHandlers("whsec_a_totally_different_secret", NewService(nil))

	r := signedEventRequest(t, "invoice.payment_failed", `{}`)
	rec := httptest.NewRecorder()

	h.handleStripeWebhook(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when the handler's secret doesn't match the one used to sign", rec.Code)
	}
}

func TestHandleStripeWebhook_MissingSignatureHeader_Returns401(t *testing.T) {
	h := NewHandlers(testWebhookSecret, NewService(nil))

	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	h.handleStripeWebhook(rec, r)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when Stripe-Signature is absent", rec.Code)
	}
}

type erroringBody struct{}

func (erroringBody) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (erroringBody) Close() error             { return nil }

func TestHandleStripeWebhook_BodyReadError_Returns400(t *testing.T) {
	h := NewHandlers(testWebhookSecret, NewService(nil))

	r := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil)
	r.Body = erroringBody{}
	rec := httptest.NewRecorder()

	h.handleStripeWebhook(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when the request body can't be read", rec.Code)
	}
}

var _ io.ReadCloser = erroringBody{}
