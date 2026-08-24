// Package webhook mirrors feather.services.StripeWebhookService +
// feather.controllers.StripeWebhookController: receives Stripe's
// checkout/subscription/invoice events. Signature verification uses the raw
// request body — never a re-serialized one, since that would change the
// bytes being signed.
package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/stripe/stripe-go/v86"
	stripewebhook "github.com/stripe/stripe-go/v86/webhook"

	"lumo-payments-svc/internal/subscriptions"
)

// Service mirrors StripeWebhookService. Signature verification already
// happened in Handlers before Handle is ever called.
//
// checkout.session.completed is deliberately not handled here — it only
// proves Stripe Checkout finished, not that this system turned that into
// actual Pro access, and subscriptions.Service only ever creates a
// `subscriptions` row once customer.subscription.{created,updated} reports
// real status. Still subscribed to in the Stripe dashboard for now
// (harmless, just falls through to the default case) — fine to unsubscribe
// it there whenever convenient.
type Service struct {
	subscriptions *subscriptions.Service
}

func NewService(s *subscriptions.Service) *Service {
	return &Service{subscriptions: s}
}

func (s *Service) Handle(ctx context.Context, event stripe.Event) {
	switch event.Type {
	case "customer.subscription.created", "customer.subscription.updated":
		s.handleSubscriptionUpdated(ctx, event)
	case "customer.subscription.deleted":
		s.handleSubscriptionDeleted(ctx, event)
	case "invoice.payment_failed":
		log.Printf("webhook: Stripe invoice payment failed, event=%s", event.ID)
	default:
		// not something we track
	}
}

func (s *Service) handleSubscriptionUpdated(ctx context.Context, event stripe.Event) {
	sub, err := deserializeSubscription(event)
	if err != nil {
		log.Printf("webhook: failed to deserialize subscription for event %s (%s): %v", event.ID, event.Type, err)
		return
	}
	s.subscriptions.SyncFromStripeSubscription(ctx, sub)
}

func (s *Service) handleSubscriptionDeleted(ctx context.Context, event stripe.Event) {
	sub, err := deserializeSubscription(event)
	if err != nil {
		log.Printf("webhook: failed to deserialize subscription for event %s (%s): %v", event.ID, event.Type, err)
		return
	}
	s.subscriptions.MarkCanceled(ctx, sub.ID)
}

// deserializeSubscription unmarshals the event's raw data object straight
// into a stripe.Subscription. Unlike the Java SDK (whose getObject() can
// come back empty if the event was serialized against a newer API version
// than the SDK's pinned default, needing a deserializeUnsafe() fallback),
// stripe-go always exposes the untouched raw JSON via event.Data.Raw, so no
// such fallback is needed here.
func deserializeSubscription(event stripe.Event) (*stripe.Subscription, error) {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}

// Handlers mirrors StripeWebhookController.
type Handlers struct {
	webhookSecret string
	svc           *Service
}

func NewHandlers(webhookSecret string, svc *Service) *Handlers {
	return &Handlers{webhookSecret: webhookSecret, svc: svc}
}

func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /webhooks/stripe", h.handleStripeWebhook)
}

func (h *Handlers) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	event, err := stripewebhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), h.webhookSecret)
	if err != nil {
		log.Printf("webhook: rejected Stripe webhook with invalid signature: %v", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	h.svc.Handle(r.Context(), event)
	w.WriteHeader(http.StatusOK)
}
