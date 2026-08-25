package subscriptions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stripe/stripe-go/v86"
)

// mockBackend implements stripe.Backend entirely in-memory for
// fetchCustomerEmail -- no real network call is made.
type mockBackend struct {
	call map[string]func(v stripe.LastResponseSetter) error
}

func (m *mockBackend) Call(method, path, _ string, _ stripe.ParamsContainer, v stripe.LastResponseSetter) error {
	key := method + " " + path
	fn, ok := m.call[key]
	if !ok {
		return fmt.Errorf("mockBackend: unexpected Call %s", key)
	}
	return fn(v)
}
func (m *mockBackend) CallRaw(method, path, _ string, _ []byte, _ *stripe.Params, _ stripe.LastResponseSetter) error {
	return fmt.Errorf("mockBackend: CallRaw not supported (%s %s)", method, path)
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

// --- fetchCustomerEmail ---

func TestFetchCustomerEmail_BlankCustomerID_ReturnsEmptyWithoutCallingStripe(t *testing.T) {
	a := &ActivationService{}
	if got := a.fetchCustomerEmail(context.Background(), ""); got != "" {
		t.Errorf("fetchCustomerEmail(\"\") = %q, want empty string", got)
	}
}

func TestFetchCustomerEmail_Success(t *testing.T) {
	mb := &mockBackend{
		call: map[string]func(stripe.LastResponseSetter) error{
			"GET /v1/customers/cus_123": func(v stripe.LastResponseSetter) error {
				return json.Unmarshal([]byte(`{"id":"cus_123","email":"customer@example.com"}`), v)
			},
		},
	}
	a := &ActivationService{sc: newTestClient(mb)}

	got := a.fetchCustomerEmail(context.Background(), "cus_123")
	if got != "customer@example.com" {
		t.Errorf("fetchCustomerEmail() = %q, want %q", got, "customer@example.com")
	}
}

func TestFetchCustomerEmail_StripeError_ReturnsEmpty(t *testing.T) {
	mb := &mockBackend{
		call: map[string]func(stripe.LastResponseSetter) error{
			"GET /v1/customers/cus_missing": func(v stripe.LastResponseSetter) error {
				return fmt.Errorf("stripe: no such customer")
			},
		},
	}
	a := &ActivationService{sc: newTestClient(mb)}

	got := a.fetchCustomerEmail(context.Background(), "cus_missing")
	if got != "" {
		t.Errorf("fetchCustomerEmail() = %q, want empty string on a Stripe error", got)
	}
}
