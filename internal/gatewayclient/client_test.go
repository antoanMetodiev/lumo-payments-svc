package gatewayclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- GetJSON ---

func TestGetJSON_Success_DecodesBody(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Internal-Api-Key")
		w.Write([]byte(`{"subscriptionType":"PRO_USER"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "supersecret")
	var out struct {
		SubscriptionType string `json:"subscriptionType"`
	}
	status, err := c.GetJSON(context.Background(), "/internal/users/user_123", &out)

	if err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if out.SubscriptionType != "PRO_USER" {
		t.Errorf("decoded SubscriptionType = %q, want %q", out.SubscriptionType, "PRO_USER")
	}
	if gotHeader != "supersecret" {
		t.Errorf("X-Internal-Api-Key header = %q, want %q", gotHeader, "supersecret")
	}
}

func TestGetJSON_NonOKStatus_NoDecodeAttempted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`not even valid json`))
	}))
	defer srv.Close()

	c := New(srv.URL, "supersecret")
	var out map[string]any
	status, err := c.GetJSON(context.Background(), "/internal/users/missing", &out)

	if err != nil {
		t.Fatalf("GetJSON() error = %v, want nil (a non-200 status is not a decode attempt)", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestGetJSON_MalformedJSONOn200_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not valid`))
	}))
	defer srv.Close()

	c := New(srv.URL, "supersecret")
	var out map[string]any
	status, err := c.GetJSON(context.Background(), "/x", &out)

	if err == nil {
		t.Fatal("GetJSON() error = nil, want a decode error for malformed JSON")
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (the HTTP layer succeeded, only decoding failed)", status)
	}
}

func TestGetJSON_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "supersecret")
	var out map[string]any
	_, err := c.GetJSON(context.Background(), "/x", &out)
	if err == nil {
		t.Fatal("GetJSON() error = nil, want a connection error")
	}
}

// --- PostJSON ---

func TestPostJSON_SendsCorrectMethodHeadersAndBody(t *testing.T) {
	var gotMethod, gotContentType, gotAPIKey, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotAPIKey = r.Header.Get("X-Internal-Api-Key")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "supersecret")
	status, err := c.PostJSON(context.Background(), "/internal/users/user_123/subscription", map[string]string{"subscriptionType": "PRO_USER"})

	if err != nil {
		t.Fatalf("PostJSON() error = %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotAPIKey != "supersecret" {
		t.Errorf("X-Internal-Api-Key = %q, want %q", gotAPIKey, "supersecret")
	}
	if gotBody != `{"subscriptionType":"PRO_USER"}` {
		t.Errorf("body = %q, want %q", gotBody, `{"subscriptionType":"PRO_USER"}`)
	}
}

func TestPostJSON_ReturnsStatusEvenWithoutDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`this body is never read by PostJSON`))
	}))
	defer srv.Close()

	c := New(srv.URL, "supersecret")
	status, err := c.PostJSON(context.Background(), "/x", map[string]string{"a": "b"})

	if err != nil {
		t.Fatalf("PostJSON() error = %v", err)
	}
	if status != http.StatusCreated {
		t.Errorf("status = %d, want 201", status)
	}
}

func TestPostJSON_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1", "supersecret")
	_, err := c.PostJSON(context.Background(), "/x", map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("PostJSON() error = nil, want a connection error")
	}
}
