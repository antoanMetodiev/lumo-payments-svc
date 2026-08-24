package subscriptions

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"lumo-payments-svc/internal/gatewayauth"
	"lumo-payments-svc/internal/models"
)

// Handlers mirrors SubscriptionController + InternalSubscriptionController
// — same paths, same status codes.
type Handlers struct {
	svc *Service
}

func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /payments/subscription", h.getSubscription)
	mux.HandleFunc("DELETE /internal/subscriptions/{clerkId}", h.deleteSubscription)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("subscriptions: writeJSON: %v", err)
	}
}

type subscriptionResponse struct {
	Plan              models.PlanType           `json:"plan"`
	Status            models.SubscriptionStatus `json:"status"`
	CurrentPeriodEnd  *time.Time                `json:"currentPeriodEnd"`
	CancelAtPeriodEnd bool                      `json:"cancelAtPeriodEnd"`
}

// getSubscription mirrors SubscriptionController.getSubscription — a Clerk
// user who's never started checkout has no row in `subscriptions` at all —
// that's the normal Free-plan state, not an error, so this returns a
// default FREE/NONE response instead of 404.
func (h *Handlers) getSubscription(w http.ResponseWriter, r *http.Request) {
	clerkID := gatewayauth.ClerkUserID(r)

	sub, err := h.svc.FindByClerkID(r.Context(), clerkID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := subscriptionResponse{Plan: models.PlanFree, Status: models.StatusNone}
	if sub != nil {
		resp = subscriptionResponse{
			Plan:              sub.Plan(),
			Status:            sub.Status,
			CurrentPeriodEnd:  sub.CurrentPeriodEnd,
			CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		}
	}
	writeJSON(w, resp)
}

// deleteSubscription mirrors InternalSubscriptionController.deleteSubscription
// — service-to-service only, called by lumo-user-svc (via the gateway)
// right after it deletes its own copy of a Clerk account.
func (h *Handlers) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	clerkID := r.PathValue("clerkId")

	if err := h.svc.DeleteByClerkID(r.Context(), clerkID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
