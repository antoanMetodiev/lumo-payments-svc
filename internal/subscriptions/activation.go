package subscriptions

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	"lumo-payments-svc/internal/email"
	"lumo-payments-svc/internal/models"
	"lumo-payments-svc/internal/refund"
	"lumo-payments-svc/internal/usersync"
)

// ActivationSyncContext carries plain values rather than a *models.PaymentSubscription
// — the struct belongs to the caller's request-scoped work, and this runs
// detached from it (see ActivationService.SyncOrEscalate).
type ActivationSyncContext struct {
	SubscriptionID       uuid.UUID
	ClerkID              string
	Plan                 models.PlanType
	StripeSubscriptionID string
	StripeCustomerID     string
}

// ActivationService mirrors feather.services.SubscriptionActivationService:
// reconciles what Stripe just told us (via Service) with what lumo-user-svc
// actually knows about the user, and decides what to do when those two
// can't be brought in sync.
type ActivationService struct {
	userSync *usersync.Service
	refund   *refund.Service
	email    *email.Service
	repo     *Repository
	sc       *stripe.Client
}

func NewActivationService(userSync *usersync.Service, refundSvc *refund.Service, emailSvc *email.Service, repo *Repository, sc *stripe.Client) *ActivationService {
	return &ActivationService{userSync: userSync, refund: refundSvc, email: emailSvc, repo: repo, sc: sc}
}

// SyncOrEscalate mirrors the @Async entry point: dispatched off the caller
// (the Stripe webhook request) so a slow refund call here can't make Stripe
// think the whole webhook delivery failed and retry it. Runs on a context
// detached from the request — by the time this executes, the HTTP handler
// that triggered it has very likely already returned its response.
func (a *ActivationService) SyncOrEscalate(ac ActivationSyncContext) {
	go a.syncOrEscalate(ac)
}

func (a *ActivationService) syncOrEscalate(ac ActivationSyncContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if a.userSync.SyncSubscriptionType(ctx, ac.ClerkID, ac.Plan) {
		return
	}

	if ac.Plan == models.PlanPro {
		// The customer paid and Stripe confirms it, but lumo-user-svc never
		// learned it — refund rather than leave them paid-but-locked-out.
		a.handleActivationFailure(ctx, ac)
	} else {
		// Failing to revoke isn't a money problem (nobody lost anything — if
		// anything they keep Pro access a bit longer than entitled), so no
		// refund and no email — just a loud log line for whoever's watching
		// the logs to catch and fix by hand.
		log.Printf("subscriptions: could not revoke Pro access in lumo-user-svc for clerkId=%s stripeSubscriptionId=%s after retries — "+
			"they may retain Pro access longer than entitled, needs manual fix", ac.ClerkID, ac.StripeSubscriptionID)
	}
}

func (a *ActivationService) handleActivationFailure(ctx context.Context, ac ActivationSyncContext) {
	log.Printf("subscriptions: could not activate Pro for clerkId=%s after retries — refunding", ac.ClerkID)

	refunded := a.refund.RefundAndCancel(ctx, ac.StripeSubscriptionID)

	// No persisted trace of a failed activation — the customer got refunded,
	// so there's no real subscription relationship left to record. Must not
	// block the email step below if it fails.
	a.deleteFailedSubscriptionRow(ctx, ac.SubscriptionID)

	if refunded {
		a.email.SendActivationFailedEmail(a.fetchCustomerEmail(ctx, ac.StripeCustomerID))
	} else {
		// Money is still charged and access still isn't granted, and
		// refund.Service already retried a few times on its own — this is as
		// far as automation gets. Loud log line, no email (no ops inbox
		// wired up): needs a human to handle it by hand in Stripe.
		log.Printf("subscriptions: refund FAILED after retries for clerkId=%s stripeSubscriptionId=%s — "+
			"customer was charged but has no Pro access and no automatic refund, needs manual review", ac.ClerkID, ac.StripeSubscriptionID)
	}
}

func (a *ActivationService) deleteFailedSubscriptionRow(ctx context.Context, id uuid.UUID) {
	if err := a.repo.DeleteByID(ctx, id); err != nil {
		log.Printf("subscriptions: failed to delete subscriptionId=%s after activation failure — "+
			"refund/email still proceed regardless, but a stale row may remain, needs manual cleanup: %v", id, err)
	}
}

func (a *ActivationService) fetchCustomerEmail(ctx context.Context, stripeCustomerID string) string {
	if stripeCustomerID == "" {
		return ""
	}

	customer, err := a.sc.V1Customers.Retrieve(ctx, stripeCustomerID, &stripe.CustomerRetrieveParams{})
	if err != nil {
		log.Printf("subscriptions: could not fetch customer email for stripeCustomerId=%s: %v", stripeCustomerID, err)
		return ""
	}
	return customer.Email
}
