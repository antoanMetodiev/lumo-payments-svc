package billingportal

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"lumo-payments-svc/internal/gatewayauth"
)

type SessionResponse struct {
	URL string `json:"url"`
}

// Handlers mirrors BillingPortalController — same path, same status codes.
// NoStripeCustomerError (thrown when the caller never started checkout) is
// mapped to 404.
type Handlers struct {
	svc *Service
}

func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /payments/portal-session", h.createPortalSession)
}

func (h *Handlers) createPortalSession(w http.ResponseWriter, r *http.Request) {
	clerkID := gatewayauth.ClerkUserID(r)

	url, err := h.svc.CreatePortalSessionURL(r.Context(), clerkID)
	if err != nil {
		var noCustomer *NoStripeCustomerError
		if errors.As(err, &noCustomer) {
			http.Error(w, noCustomer.Error(), http.StatusNotFound)
			return
		}
		log.Printf("billingportal: failed to create Stripe billing portal session for clerkId=%s: %v", clerkID, err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(SessionResponse{URL: url}); err != nil {
		log.Printf("billingportal: writeJSON: %v", err)
	}
}
