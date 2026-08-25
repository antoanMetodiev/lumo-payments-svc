package email

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc lets a test intercept outgoing requests at the Transport
// level -- no real network call is ever made, since send() hardcodes
// https://api.resend.com/emails and can't be redirected to a test server.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// --- SendActivationFailedEmail: blank/whitespace recipient short-circuits ---

func TestSendActivationFailedEmail_BlankRecipient_NoOp(t *testing.T) {
	called := false
	s := &Service{
		http:        &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { called = true; return newResponse(200, "{}"), nil })},
		apiKey:      "key",
		fromAddress: "from@example.com",
	}

	s.SendActivationFailedEmail("")

	if called {
		t.Error("send was called for a blank recipient, want no-op")
	}
}

func TestSendActivationFailedEmail_WhitespaceRecipient_NoOp(t *testing.T) {
	called := false
	s := &Service{
		http:        &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { called = true; return newResponse(200, "{}"), nil })},
		apiKey:      "key",
		fromAddress: "from@example.com",
	}

	s.SendActivationFailedEmail("   ")

	if called {
		t.Error("send was called for a whitespace-only recipient, want no-op")
	}
}

// --- SendActivationFailedEmail: valid recipient builds the expected request ---

func TestSendActivationFailedEmail_ValidRecipient_SendsCorrectRequest(t *testing.T) {
	var gotReq *http.Request
	var gotBody map[string]any

	s := &Service{
		http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotReq = r
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &gotBody)
			return newResponse(200, `{"id":"email_123"}`), nil
		})},
		apiKey:      "re_secret_key",
		fromAddress: "noreply@auroraflix.cloud",
	}

	s.SendActivationFailedEmail("customer@example.com")

	if gotReq == nil {
		t.Fatal("no request was sent for a valid recipient")
	}
	if gotReq.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", gotReq.Method)
	}
	if gotReq.URL.String() != "https://api.resend.com/emails" {
		t.Errorf("URL = %q, want the Resend emails endpoint", gotReq.URL.String())
	}
	if got := gotReq.Header.Get("Authorization"); got != "Bearer re_secret_key" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer re_secret_key")
	}
	if got := gotReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header = %q, want application/json", got)
	}

	if gotBody["from"] != "noreply@auroraflix.cloud" {
		t.Errorf("body[from] = %v, want %q", gotBody["from"], "noreply@auroraflix.cloud")
	}
	to, ok := gotBody["to"].([]any)
	if !ok || len(to) != 1 || to[0] != "customer@example.com" {
		t.Errorf("body[to] = %v, want [\"customer@example.com\"]", gotBody["to"])
	}
	if subject, _ := gotBody["subject"].(string); subject == "" {
		t.Error("body[subject] is empty, want the activation-failed subject line")
	}
	html, _ := gotBody["html"].(string)
	if !strings.Contains(html, "Възстановихме пълната платена сума") {
		t.Errorf("body[html] doesn't contain the expected refund confirmation text: %q", html)
	}
}

// --- send: failure modes are logged, never panic or propagate ---

func TestSend_NonOKStatus_DoesNotPanic(t *testing.T) {
	s := &Service{
		http:        &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return newResponse(500, `{"error":"boom"}`), nil })},
		apiKey:      "key",
		fromAddress: "from@example.com",
	}

	s.SendActivationFailedEmail("customer@example.com")
}

func TestSend_TransportError_DoesNotPanic(t *testing.T) {
	s := &Service{
		http:        &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, io.ErrClosedPipe })},
		apiKey:      "key",
		fromAddress: "from@example.com",
	}

	s.SendActivationFailedEmail("customer@example.com")
}
