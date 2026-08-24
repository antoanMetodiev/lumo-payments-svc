// Package checkout mirrors feather.services.CheckoutService +
// feather.controllers.CheckoutController.
package checkout

import (
	"context"
	"log"
	"net/url"

	"github.com/stripe/stripe-go/v86"

	"lumo-payments-svc/internal/gatewayclient"
	"lumo-payments-svc/internal/subscriptions"
)

type Service struct {
	repo       *subscriptions.Repository
	gateway    *gatewayclient.Client
	sc         *stripe.Client
	proPriceID string
	successURL string
	cancelURL  string
}

func New(repo *subscriptions.Repository, gateway *gatewayclient.Client, sc *stripe.Client, proPriceID, successURL, cancelURL string) *Service {
	return &Service{repo: repo, gateway: gateway, sc: sc, proPriceID: proPriceID, successURL: successURL, cancelURL: cancelURL}
}

// CreateCheckoutSessionURL creates a Stripe Checkout Session for the Pro
// plan and returns its URL for the frontend to redirect the browser to.
//
// Nothing is written to our own database here — starting or abandoning
// checkout leaves no trace. A `subscriptions` row only ever appears once
// Stripe reports real, confirmed status via customer.subscription.{created,updated}
// (see subscriptions.Service.SyncFromStripeSubscription) — not on
// checkout.session.completed, which only proves Checkout finished, not that
// this system actually turned it into working Pro access.
//
// If this clerkId already has a linked Stripe Customer (from a previous
// completed checkout), that customer is reused so Stripe doesn't end up
// with duplicate Customer records for the same person. Otherwise Stripe
// creates the Customer itself once/if checkout actually completes.
func (s *Service) CreateCheckoutSessionURL(ctx context.Context, clerkID, email string) (string, error) {
	var existingCustomerID *string
	sub, err := s.repo.FindByClerkID(ctx, clerkID)
	if err != nil {
		return "", err
	}
	if sub != nil {
		existingCustomerID = sub.StripeCustomerID
	}

	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		ClientReferenceID: stripe.String(clerkID),
		// {CHECKOUT_SESSION_ID} is a literal placeholder Stripe substitutes
		// with the real session id at redirect time — the frontend's
		// post-checkout wait screen needs it to poll GetCheckoutResult below.
		SuccessURL: stripe.String(s.successURL + "?checkout=success&session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(s.cancelURL + "?checkout=cancelled"),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{Price: stripe.String(s.proPriceID), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{"clerkId": clerkID},
		},
	}
	if existingCustomerID != nil {
		params.Customer = existingCustomerID
	} else {
		params.CustomerEmail = stripe.String(email)
	}

	session, err := s.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

// GetCheckoutResult backs the frontend's post-checkout wait screen — reads
// Stripe's own Checkout Session directly rather than anything in our own
// database, since a failed activation attempt leaves no row behind at all.
//
// "Succeeded" is checked against lumo-user-svc's own subscriptionType (via
// the gateway), not this service's local `subscriptions` row — that row
// gets a Stripe-confirmed ACTIVE status written to it synchronously, before
// ActivationService's async sync to lumo-user-svc has even run yet, so it
// briefly looks like a success even when the sync is about to fail and the
// row is about to be deleted. The only thing that's actually true the whole
// time is whatever lumo-user-svc itself currently says.
//
// Deliberately never reports a "failed" result: the Checkout Session's
// creation time is the moment the customer clicked "Buy Pro", not the
// moment they landed back on this site — not a meaningful "how long have we
// actually been waiting" measure. Giving up is the frontend's call instead,
// timed from when its own polling started.
//
// Returns (nil, nil) if the session doesn't belong to callerClerkID — one
// signed-in user can't probe another's checkout session id.
func (s *Service) GetCheckoutResult(ctx context.Context, sessionID, callerClerkID string) (*ResultResponse, error) {
	session, err := s.sc.V1CheckoutSessions.Retrieve(ctx, sessionID, &stripe.CheckoutSessionRetrieveParams{})
	if err != nil {
		return nil, err
	}

	if session.ClientReferenceID != callerClerkID {
		return nil, nil
	}

	if s.isProUser(ctx, callerClerkID) {
		return &ResultResponse{Result: ResultSucceeded}, nil
	}
	return &ResultResponse{Result: ResultPending}, nil
}

func (s *Service) isProUser(ctx context.Context, clerkID string) bool {
	var user struct {
		SubscriptionType string `json:"subscriptionType"`
	}
	status, err := s.gateway.GetJSON(ctx, "/internal/users/"+url.PathEscape(clerkID), &user)
	if err != nil || status != 200 {
		// Treated as "not Pro yet" rather than an error — a transient blip
		// here just means one more pending poll, not a false failure.
		log.Printf("checkout: failed to check lumo-user-svc subscriptionType for clerkId=%s while polling checkout result (status=%d): %v", clerkID, status, err)
		return false
	}
	return user.SubscriptionType == "PRO_USER"
}
