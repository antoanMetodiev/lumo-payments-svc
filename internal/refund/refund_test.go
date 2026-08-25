package refund

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v86"
)

// mockBackend implements stripe.Backend entirely in-memory -- no real
// network call is ever made. Call/CallRaw route to test-supplied functions
// keyed by "METHOD path"; anything unhandled fails loudly rather than
// silently hitting the real Stripe API.
type mockBackend struct {
	call    map[string]func(params stripe.ParamsContainer, v stripe.LastResponseSetter) error
	callRaw map[string]func(body []byte, v stripe.LastResponseSetter) error
}

func (m *mockBackend) Call(method, path, _ string, params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
	key := method + " " + path
	fn, ok := m.call[key]
	if !ok {
		return fmt.Errorf("mockBackend: unexpected Call %s", key)
	}
	return fn(params, v)
}

func (m *mockBackend) CallRaw(method, path, _ string, body []byte, _ *stripe.Params, v stripe.LastResponseSetter) error {
	key := method + " " + path
	fn, ok := m.callRaw[key]
	if !ok {
		return fmt.Errorf("mockBackend: unexpected CallRaw %s", key)
	}
	return fn(body, v)
}

func (m *mockBackend) CallStreaming(method, path, _ string, _ stripe.ParamsContainer, _ stripe.StreamingLastResponseSetter) error {
	return fmt.Errorf("mockBackend: CallStreaming not supported (%s %s)", method, path)
}

func (m *mockBackend) CallMultipart(method, path, _, _ string, _ *bytes.Buffer, _ *stripe.Params, _ stripe.LastResponseSetter) error {
	return fmt.Errorf("mockBackend: CallMultipart not supported (%s %s)", method, path)
}

func (m *mockBackend) SetMaxNetworkRetries(int64) {}

func newTestClient(mb *mockBackend) *stripe.Client {
	return stripe.NewClient("sk_test_123", stripe.WithBackends(&stripe.Backends{API: mb}))
}

func jsonResponder(canned string) func(stripe.ParamsContainer, stripe.LastResponseSetter) error {
	return func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
		return json.Unmarshal([]byte(canned), v)
	}
}

func jsonRawResponder(canned string) func([]byte, stripe.LastResponseSetter) error {
	return func(_ []byte, v stripe.LastResponseSetter) error {
		return json.Unmarshal([]byte(canned), v)
	}
}

func speedUpRetries(t *testing.T) {
	t.Helper()
	orig := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryDelays = orig })
}

// --- RefundAndCancel / CancelSubscription: blank-id short-circuits, safe
// against a nil Stripe client since they return before ever touching s.sc. ---

func TestRefundAndCancel_BlankSubscriptionID_ReturnsFalse(t *testing.T) {
	s := &Service{}
	if got := s.RefundAndCancel(context.Background(), ""); got {
		t.Error("RefundAndCancel(\"\") = true, want false")
	}
}

func TestRefundAndCancel_WhitespaceSubscriptionID_ReturnsFalse(t *testing.T) {
	s := &Service{}
	if got := s.RefundAndCancel(context.Background(), "   "); got {
		t.Error("RefundAndCancel(\"   \") = true, want false")
	}
}

func TestCancelSubscription_BlankSubscriptionID_ReturnsTrue(t *testing.T) {
	s := &Service{}
	if got := s.CancelSubscription(context.Background(), ""); !got {
		t.Error("CancelSubscription(\"\") = false, want true (nothing to cancel)")
	}
}

func TestCancelSubscription_WhitespaceSubscriptionID_ReturnsTrue(t *testing.T) {
	s := &Service{}
	if got := s.CancelSubscription(context.Background(), "   "); !got {
		t.Error("CancelSubscription(\"   \") = false, want true")
	}
}

// --- RefundAndCancel: real Stripe-backed paths via the mock backend ---

func TestRefundAndCancel_PaymentIntentPath_Success(t *testing.T) {
	var refundCalls, cancelCalls int32
	var gotPaymentIntent, gotIdempotencyKey string
	var gotMetadata map[string]string

	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_1": jsonResponder(`{"id":"sub_1","status":"active","latest_invoice":{"id":"in_1"}}`),
			"POST /v1/refunds": func(params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				atomic.AddInt32(&refundCalls, 1)
				rp := params.(*stripe.RefundCreateParams)
				if rp.PaymentIntent != nil {
					gotPaymentIntent = *rp.PaymentIntent
				}
				if rp.IdempotencyKey != nil {
					gotIdempotencyKey = *rp.IdempotencyKey
				}
				gotMetadata = rp.Metadata
				return json.Unmarshal([]byte(`{"id":"re_1"}`), v)
			},
			"DELETE /v1/subscriptions/sub_1": func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				atomic.AddInt32(&cancelCalls, 1)
				return json.Unmarshal([]byte(`{"id":"sub_1","status":"canceled"}`), v)
			},
		},
		callRaw: map[string]func([]byte, stripe.LastResponseSetter) error{
			"GET /v1/invoice_payments": jsonRawResponder(`{"data":[{"payment":{"type":"payment_intent","payment_intent":{"id":"pi_1"}}}],"has_more":false}`),
		},
	}

	s := New(newTestClient(mb))
	if !s.RefundAndCancel(context.Background(), "sub_1") {
		t.Fatal("RefundAndCancel() = false, want true")
	}
	if refundCalls != 1 {
		t.Errorf("refund calls = %d, want 1", refundCalls)
	}
	if cancelCalls != 1 {
		t.Errorf("cancel calls = %d, want 1", cancelCalls)
	}
	if gotPaymentIntent != "pi_1" {
		t.Errorf("RefundCreateParams.PaymentIntent = %q, want %q", gotPaymentIntent, "pi_1")
	}
	if want := "activation-failed-refund-sub_1"; gotIdempotencyKey != want {
		t.Errorf("idempotency key = %q, want %q", gotIdempotencyKey, want)
	}
	if gotMetadata["reason"] != "lumo_activation_sync_failed" {
		t.Errorf("metadata[reason] = %q, want %q", gotMetadata["reason"], "lumo_activation_sync_failed")
	}
}

func TestRefundAndCancel_ChargePath_Success(t *testing.T) {
	var gotCharge string

	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_2": jsonResponder(`{"id":"sub_2","status":"active","latest_invoice":{"id":"in_2"}}`),
			"POST /v1/refunds": func(params stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				rp := params.(*stripe.RefundCreateParams)
				if rp.Charge != nil {
					gotCharge = *rp.Charge
				}
				return json.Unmarshal([]byte(`{"id":"re_2"}`), v)
			},
			"DELETE /v1/subscriptions/sub_2": jsonResponder(`{"id":"sub_2","status":"canceled"}`),
		},
		callRaw: map[string]func([]byte, stripe.LastResponseSetter) error{
			"GET /v1/invoice_payments": jsonRawResponder(`{"data":[{"payment":{"type":"charge","charge":{"id":"ch_1"}}}],"has_more":false}`),
		},
	}

	s := New(newTestClient(mb))
	if !s.RefundAndCancel(context.Background(), "sub_2") {
		t.Fatal("RefundAndCancel() = false, want true")
	}
	if gotCharge != "ch_1" {
		t.Errorf("RefundCreateParams.Charge = %q, want %q", gotCharge, "ch_1")
	}
}

// TestRefundAndCancel_NothingToRefund_NoRetry documents that "found the
// subscription/invoice but there's genuinely no payment to refund" is
// treated as final, not transient -- errNothingToRefund short-circuits the
// retry loop instead of burning through all 3 attempts.
func TestRefundAndCancel_NothingToRefund_NoRetry(t *testing.T) {
	var subscriptionGets int32

	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_3": func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				atomic.AddInt32(&subscriptionGets, 1)
				return json.Unmarshal([]byte(`{"id":"sub_3","status":"active","latest_invoice":{"id":"in_3"}}`), v)
			},
		},
		callRaw: map[string]func([]byte, stripe.LastResponseSetter) error{
			"GET /v1/invoice_payments": jsonRawResponder(`{"data":[],"has_more":false}`),
		},
	}

	s := New(newTestClient(mb))
	if s.RefundAndCancel(context.Background(), "sub_3") {
		t.Fatal("RefundAndCancel() = true, want false (nothing to refund)")
	}
	if subscriptionGets != 1 {
		t.Errorf("subscription GETs = %d, want 1 (must not retry on errNothingToRefund)", subscriptionGets)
	}
}

func TestRefundAndCancel_NoLatestInvoice_NothingToRefund(t *testing.T) {
	callRawInvoked := false
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_4": jsonResponder(`{"id":"sub_4","status":"active"}`),
		},
		callRaw: map[string]func([]byte, stripe.LastResponseSetter) error{
			"GET /v1/invoice_payments": func(_ []byte, v stripe.LastResponseSetter) error {
				callRawInvoked = true
				return json.Unmarshal([]byte(`{"data":[]}`), v)
			},
		},
	}

	s := New(newTestClient(mb))
	if s.RefundAndCancel(context.Background(), "sub_4") {
		t.Fatal("RefundAndCancel() = true, want false")
	}
	if callRawInvoked {
		t.Error("invoice_payments was queried even though the subscription has no latest_invoice at all")
	}
}

func TestRefundAndCancel_TransientFailureThenSucceeds(t *testing.T) {
	speedUpRetries(t)

	var attempt int32
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_5": func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				if atomic.AddInt32(&attempt, 1) == 1 {
					return errors.New("stripe: transient API error")
				}
				return json.Unmarshal([]byte(`{"id":"sub_5","status":"active","latest_invoice":{"id":"in_5"}}`), v)
			},
			"POST /v1/refunds":               jsonResponder(`{"id":"re_5"}`),
			"DELETE /v1/subscriptions/sub_5": jsonResponder(`{"id":"sub_5","status":"canceled"}`),
		},
		callRaw: map[string]func([]byte, stripe.LastResponseSetter) error{
			"GET /v1/invoice_payments": jsonRawResponder(`{"data":[{"payment":{"type":"payment_intent","payment_intent":{"id":"pi_5"}}}],"has_more":false}`),
		},
	}

	s := New(newTestClient(mb))
	if !s.RefundAndCancel(context.Background(), "sub_5") {
		t.Fatal("RefundAndCancel() = false, want true (should succeed on the 2nd attempt)")
	}
	if attempt != 2 {
		t.Errorf("subscription GET attempts = %d, want 2", attempt)
	}
}

func TestRefundAndCancel_AllAttemptsFail_ReturnsFalse(t *testing.T) {
	speedUpRetries(t)

	var attempts int32
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_6": func(_ stripe.ParamsContainer, _ stripe.LastResponseSetter) error {
				atomic.AddInt32(&attempts, 1)
				return errors.New("stripe: persistent API error")
			},
		},
	}

	s := New(newTestClient(mb))
	if s.RefundAndCancel(context.Background(), "sub_6") {
		t.Fatal("RefundAndCancel() = true, want false")
	}
	if attempts != maxAttempts {
		t.Errorf("attempts = %d, want %d", attempts, maxAttempts)
	}
}

// --- CancelSubscription: real Stripe-backed paths ---

func TestCancelSubscription_AlreadyCanceled_NoOpSuccess(t *testing.T) {
	var deleteCalls int32
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_7": jsonResponder(`{"id":"sub_7","status":"canceled"}`),
			"DELETE /v1/subscriptions/sub_7": func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				atomic.AddInt32(&deleteCalls, 1)
				return json.Unmarshal([]byte(`{"id":"sub_7","status":"canceled"}`), v)
			},
		},
	}

	s := New(newTestClient(mb))
	if !s.CancelSubscription(context.Background(), "sub_7") {
		t.Fatal("CancelSubscription() = false, want true")
	}
	if deleteCalls != 0 {
		t.Errorf("DELETE calls = %d, want 0 (already canceled, nothing to do)", deleteCalls)
	}
}

func TestCancelSubscription_ActiveSubscription_CancelsSuccessfully(t *testing.T) {
	var deleteCalls int32
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_8": jsonResponder(`{"id":"sub_8","status":"active"}`),
			"DELETE /v1/subscriptions/sub_8": func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				atomic.AddInt32(&deleteCalls, 1)
				return json.Unmarshal([]byte(`{"id":"sub_8","status":"canceled"}`), v)
			},
		},
	}

	s := New(newTestClient(mb))
	if !s.CancelSubscription(context.Background(), "sub_8") {
		t.Fatal("CancelSubscription() = false, want true")
	}
	if deleteCalls != 1 {
		t.Errorf("DELETE calls = %d, want 1", deleteCalls)
	}
}

func TestCancelSubscription_RetriesThenSucceeds(t *testing.T) {
	speedUpRetries(t)

	var attempt int32
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_9": func(_ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
				if atomic.AddInt32(&attempt, 1) == 1 {
					return errors.New("stripe: transient")
				}
				return json.Unmarshal([]byte(`{"id":"sub_9","status":"active"}`), v)
			},
			"DELETE /v1/subscriptions/sub_9": jsonResponder(`{"id":"sub_9","status":"canceled"}`),
		},
	}

	s := New(newTestClient(mb))
	if !s.CancelSubscription(context.Background(), "sub_9") {
		t.Fatal("CancelSubscription() = false, want true")
	}
	if attempt != 2 {
		t.Errorf("attempts = %d, want 2", attempt)
	}
}

func TestCancelSubscription_AllAttemptsFail_ReturnsFalse(t *testing.T) {
	speedUpRetries(t)

	var attempts int32
	mb := &mockBackend{
		call: map[string]func(stripe.ParamsContainer, stripe.LastResponseSetter) error{
			"GET /v1/subscriptions/sub_10": func(_ stripe.ParamsContainer, _ stripe.LastResponseSetter) error {
				atomic.AddInt32(&attempts, 1)
				return errors.New("stripe: persistent")
			},
		},
	}

	s := New(newTestClient(mb))
	if s.CancelSubscription(context.Background(), "sub_10") {
		t.Fatal("CancelSubscription() = true, want false")
	}
	if attempts != maxAttempts {
		t.Errorf("attempts = %d, want %d", attempts, maxAttempts)
	}
}
