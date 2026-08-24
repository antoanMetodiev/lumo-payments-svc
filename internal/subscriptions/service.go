package subscriptions

import (
	"context"
	"log"
	"time"

	"github.com/stripe/stripe-go/v86"

	"lumo-payments-svc/internal/models"
	"lumo-payments-svc/internal/refund"
)

// Service mirrors feather.services.SubscriptionService. Reads back
// SubscriptionController (via Handlers); writes come from
// internal/webhook, since Stripe — not this service — is the source of
// truth for status/period/cancellation. Every write that can change which
// plan the user is on also tells lumo-user-svc, via ActivationService
// (which retries and, if that still doesn't land, refunds the customer
// rather than leave them paid-but-locked-out).
//
// A row is only ever created once Stripe reports real subscription status
// (in SyncFromStripeSubscription) — never speculatively on
// checkout.session.completed, which only proves Checkout finished, not that
// this system actually managed to turn that into working Pro access. If
// activation then fails, ActivationService deletes the row again rather
// than leaving a dummy record behind for a customer who got refunded and
// may never come back.
type Service struct {
	repo       *Repository
	activation *ActivationService
	refund     *refund.Service
}

func NewService(repo *Repository, activation *ActivationService, refundSvc *refund.Service) *Service {
	return &Service{repo: repo, activation: activation, refund: refundSvc}
}

func (s *Service) FindByClerkID(ctx context.Context, clerkID string) (*models.PaymentSubscription, error) {
	return s.repo.FindByClerkID(ctx, clerkID)
}

// DeleteByClerkID is called when lumo-user-svc tells us (via the gateway)
// that a Clerk account was deleted. If they still have a live Stripe
// subscription, it gets canceled (no refund — they already had whatever
// access they paid for; this is just to stop billing an account that no
// longer exists at all on our side).
func (s *Service) DeleteByClerkID(ctx context.Context, clerkID string) error {
	sub, err := s.repo.FindByClerkID(ctx, clerkID)
	if err != nil {
		return err
	}
	if sub != nil && sub.StripeSubscriptionID != nil {
		s.refund.CancelSubscription(ctx, *sub.StripeSubscriptionID)
	}
	return s.repo.DeleteByClerkID(ctx, clerkID)
}

// SyncFromStripeSubscription mirrors SubscriptionService.syncFromStripeSubscription
// — the first point at which Stripe tells us a real status, so this is the
// only place a `subscriptions` row is ever created. Looked up by Stripe
// subscription id first (the common case for a returning customer); falls
// back to the clerkId CheckoutService stamped into the Stripe subscription's
// own metadata at creation time for a brand new one.
//
// Deliberately does NOT create a fresh row for a subscription whose
// incoming status is already "canceled" if we have no existing record of
// it — that's exactly what happens right after refund.Service.CancelSubscription
// cancels someone's subscription as part of deleting their account: Stripe
// fires this same event back at us moments after we've already deleted
// their row, and without this guard it would get silently resurrected here
// for a clerkId that no longer has an account at all.
func (s *Service) SyncFromStripeSubscription(ctx context.Context, stripeSub *stripe.Subscription) {
	stripeSubscriptionID := stripeSub.ID
	clerkIDFromMetadata := ""
	if stripeSub.Metadata != nil {
		clerkIDFromMetadata = stripeSub.Metadata["clerkId"]
	}

	existing, err := s.repo.FindByStripeSubscriptionID(ctx, stripeSubscriptionID)
	if err != nil {
		log.Printf("subscriptions: lookup by stripeSubscriptionId=%s failed: %v", stripeSubscriptionID, err)
		return
	}
	if existing == nil && clerkIDFromMetadata != "" {
		existing, err = s.repo.FindByClerkID(ctx, clerkIDFromMetadata)
		if err != nil {
			log.Printf("subscriptions: lookup by clerkId=%s failed: %v", clerkIDFromMetadata, err)
			return
		}
	}

	var sub *models.PaymentSubscription
	switch {
	case existing != nil:
		sub = existing
	case clerkIDFromMetadata == "":
		log.Printf("subscriptions: received subscription update for unlinked subscription %s with no clerkId metadata — cannot create a record", stripeSubscriptionID)
		return
	case string(stripeSub.Status) == "canceled":
		log.Printf("subscriptions: ignoring canceled-status update for unlinked subscription %s (clerkId metadata: %s) — likely a stray webhook arriving after that account was already deleted", stripeSubscriptionID, clerkIDFromMetadata)
		return
	default:
		sub = &models.PaymentSubscription{
			ClerkID:   clerkIDFromMetadata,
			Status:    models.StatusNone,
			CreatedAt: time.Now().UTC(),
		}
	}

	// current_period_end (and the price actually being billed) live on each
	// subscription item as of newer Stripe API versions, not on the
	// subscription itself — a single-price subscription (the only kind this
	// service creates) always has exactly one.
	var priceID *string
	var periodEnd *time.Time
	if item := firstItem(stripeSub); item != nil {
		if item.Price != nil {
			priceID = stripe.String(item.Price.ID)
		}
		t := time.Unix(item.CurrentPeriodEnd, 0).UTC()
		periodEnd = &t
	}

	var customerID *string
	if stripeSub.Customer != nil {
		customerID = stripe.String(stripeSub.Customer.ID)
	}

	sub.StripeSubscriptionID = stripe.String(stripeSubscriptionID)
	sub.StripeCustomerID = customerID
	sub.StripePriceID = priceID
	sub.Status = models.StatusFromStripe(string(stripeSub.Status))
	sub.CancelAtPeriodEnd = stripeSub.CancelAtPeriodEnd
	sub.CurrentPeriodEnd = periodEnd
	sub.UpdatedAt = time.Now().UTC()

	if err := s.repo.Upsert(ctx, sub); err != nil {
		log.Printf("subscriptions: failed to save subscription for clerkId=%s: %v", sub.ClerkID, err)
		return
	}

	s.activation.SyncOrEscalate(activationContext(sub))
}

// MarkCanceled mirrors SubscriptionService.markCanceled —
// customer.subscription.deleted: Stripe only fires this once the
// subscription is truly gone (not on cancel-at-period-end, which instead
// arrives as a customer.subscription.updated with cancelAtPeriodEnd=true
// and the still-current status).
func (s *Service) MarkCanceled(ctx context.Context, stripeSubscriptionID string) {
	sub, err := s.repo.FindByStripeSubscriptionID(ctx, stripeSubscriptionID)
	if err != nil {
		log.Printf("subscriptions: lookup by stripeSubscriptionId=%s failed: %v", stripeSubscriptionID, err)
		return
	}
	if sub == nil {
		log.Printf("subscriptions: customer.subscription.deleted for unknown subscription %s", stripeSubscriptionID)
		return
	}

	sub.Status = models.StatusCanceled
	sub.UpdatedAt = time.Now().UTC()
	if err := s.repo.Upsert(ctx, sub); err != nil {
		log.Printf("subscriptions: failed to mark canceled for stripeSubscriptionId=%s: %v", stripeSubscriptionID, err)
		return
	}

	s.activation.SyncOrEscalate(activationContext(sub))
}

func activationContext(sub *models.PaymentSubscription) ActivationSyncContext {
	ac := ActivationSyncContext{
		SubscriptionID: sub.ID,
		ClerkID:        sub.ClerkID,
		Plan:           sub.Plan(),
	}
	if sub.StripeSubscriptionID != nil {
		ac.StripeSubscriptionID = *sub.StripeSubscriptionID
	}
	if sub.StripeCustomerID != nil {
		ac.StripeCustomerID = *sub.StripeCustomerID
	}
	return ac
}

func firstItem(sub *stripe.Subscription) *stripe.SubscriptionItem {
	if sub.Items == nil || len(sub.Items.Data) == 0 {
		return nil
	}
	return sub.Items.Data[0]
}
