package checkout

import (
	"encoding/json"
	"log"
	"net/http"
	"net/mail"

	"lumo-payments-svc/internal/gatewayauth"
)

// Result mirrors CheckoutResultResponse.Result — deliberately has no FAILED
// value: this service has no reliable clock to decide "it's been long
// enough, give up" (see Service.GetCheckoutResult) — that's the frontend's
// call, timed from when its own polling started.
type Result string

const (
	ResultPending   Result = "PENDING"
	ResultSucceeded Result = "SUCCEEDED"
)

type ResultResponse struct {
	Result Result `json:"result"`
}

type SessionResponse struct {
	URL string `json:"url"`
}

// CreateSessionRequest mirrors CreateCheckoutSessionRequest — Clerk's
// session JWT doesn't carry the account's email by default, so the frontend
// sends it along explicitly rather than this service needing its own Clerk
// Backend API credentials just to look it up.
type CreateSessionRequest struct {
	Email string `json:"email"`
}

// Handlers mirrors CheckoutController — same paths, same params, same
// status codes.
type Handlers struct {
	svc *Service
}

func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /payments/checkout-session", h.createCheckoutSession)
	mux.HandleFunc("GET /payments/checkout-result", h.getCheckoutResult)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("checkout: writeJSON: %v", err)
	}
}

// createCheckoutSession mirrors CheckoutController.createCheckoutSession.
// authentication is the verified Clerk user id, set by gatewayauth.Middleware.
// Returns the Checkout Session URL for the frontend to redirect the browser
// to — nothing gets charged and no local subscription status changes until
// Stripe calls back via the webhook.
func (h *Handlers) createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	clerkID := gatewayauth.ClerkUserID(r)

	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		http.Error(w, "email is required and must be a valid address", http.StatusBadRequest)
		return
	}

	url, err := h.svc.CreateCheckoutSessionURL(r.Context(), clerkID, req.Email)
	if err != nil {
		// Stripe's own error text (bad API key, unknown price id, ...) is
		// the whole diagnosis here, so it goes in the log verbatim rather
		// than being flattened into "checkout failed".
		log.Printf("checkout: failed to create Stripe checkout session for clerkId=%s: %v", clerkID, err)
		w.WriteHeader(http.StatusBadGateway)
		// An empty 502 body told the frontend (and anyone reading a browser
		// network tab) nothing at all — this at least names the failing
		// dependency without leaking the Stripe error itself to the client.
		writeJSON(w, map[string]string{"error": "stripe_checkout_unavailable"})
		return
	}
	writeJSON(w, SessionResponse{URL: url})
}

// getCheckoutResult mirrors CheckoutController.getCheckoutResult. 404 if
// sessionId doesn't belong to the caller (someone else's session, or a
// garbled/tampered query param) rather than leaking whether it exists.
func (h *Handlers) getCheckoutResult(w http.ResponseWriter, r *http.Request) {
	clerkID := gatewayauth.ClerkUserID(r)
	sessionID := r.URL.Query().Get("sessionId")

	result, err := h.svc.GetCheckoutResult(r.Context(), sessionID, clerkID)
	if err != nil {
		log.Printf("checkout: failed to look up Stripe checkout session %s for clerkId=%s: %v", sessionID, clerkID, err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if result == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, result)
}
