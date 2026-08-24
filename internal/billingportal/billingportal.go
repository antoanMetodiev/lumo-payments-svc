// Package billingportal mirrors feather.services.BillingPortalService +
// feather.controllers.BillingPortalController.
package billingportal

import (
	"context"

	"github.com/stripe/stripe-go/v86"

	"lumo-payments-svc/internal/subscriptions"
)

// NoStripeCustomerError mirrors NoStripeCustomerException — thrown when a
// Clerk user asks for a Billing Portal link before ever starting checkout —
// there's no Stripe Customer to point the portal at yet.
type NoStripeCustomerError struct {
	ClerkID string
}

func (e *NoStripeCustomerError) Error() string {
	return "no Stripe customer on file for clerkId=" + e.ClerkID
}

type Service struct {
	repo      *subscriptions.Repository
	sc        *stripe.Client
	returnURL string
}

func New(repo *subscriptions.Repository, sc *stripe.Client, returnURL string) *Service {
	return &Service{repo: repo, sc: sc, returnURL: returnURL}
}

// CreatePortalSessionURL lets an existing subscriber manage/cancel their
// plan through Stripe's own hosted UI instead of this service having to
// build that itself.
func (s *Service) CreatePortalSessionURL(ctx context.Context, clerkID string) (string, error) {
	sub, err := s.repo.FindByClerkID(ctx, clerkID)
	if err != nil {
		return "", err
	}
	if sub == nil || sub.StripeCustomerID == nil {
		return "", &NoStripeCustomerError{ClerkID: clerkID}
	}

	session, err := s.sc.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  sub.StripeCustomerID,
		ReturnURL: stripe.String(s.returnURL),
	})
	if err != nil {
		return "", err
	}
	return session.URL, nil
}
