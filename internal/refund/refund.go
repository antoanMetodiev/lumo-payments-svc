// Package refund mirrors feather.services.RefundService — last resort when
// we've confirmed Stripe took the customer's money but couldn't confirm
// they actually got Pro access (see internal/subscriptions.ActivationService):
// refunds the payment behind the subscription's most recent invoice in full
// and cancels the subscription so it doesn't keep billing them for
// something they can't use.
package refund

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v86"
)

const maxAttempts = 3

var retryDelays = []time.Duration{time.Second, 2 * time.Second}

// errNothingToRefund distinguishes "looked, found nothing refundable" (not
// worth retrying — the same lookup will return the same answer every time)
// from a transient Stripe API failure (worth retrying).
var errNothingToRefund = errors.New("nothing to refund")

type Service struct {
	sc *stripe.Client
}

func New(sc *stripe.Client) *Service {
	return &Service{sc: sc}
}

// RefundAndCancel retries a few times over a few seconds — most failures
// here are a transient Stripe API blip, not a reason to give up immediately.
// Safe to retry: the idempotency key on the refund itself means a retried
// attempt can't double-refund.
//
// Returns true if both the refund and the cancellation succeeded.
func (s *Service) RefundAndCancel(ctx context.Context, stripeSubscriptionID string) bool {
	if strings.TrimSpace(stripeSubscriptionID) == "" {
		log.Printf("refund: cannot refund — no stripeSubscriptionId")
		return false
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := s.doRefundAndCancel(ctx, stripeSubscriptionID)
		if err == nil {
			log.Printf("refund: refunded and canceled stripeSubscriptionId=%s after activation sync failure", stripeSubscriptionID)
			return true
		}
		if errors.Is(err, errNothingToRefund) {
			log.Printf("refund: no payment found to refund for stripeSubscriptionId=%s — needs manual review", stripeSubscriptionID)
			return false
		}

		log.Printf("refund: attempt %d/%d failed to refund/cancel stripeSubscriptionId=%s: %v", attempt, maxAttempts, stripeSubscriptionID, err)
		if attempt < maxAttempts {
			sleep(retryDelays[attempt-1])
		}
	}

	log.Printf("refund: refund/cancel failed for stripeSubscriptionId=%s after %d attempts — needs manual review", stripeSubscriptionID, maxAttempts)
	return false
}

func (s *Service) doRefundAndCancel(ctx context.Context, stripeSubscriptionID string) error {
	refundParams, err := s.findRefundTarget(ctx, stripeSubscriptionID)
	if err != nil {
		return err
	}
	if refundParams == nil {
		return errNothingToRefund
	}

	refundParams.Metadata = map[string]string{"reason": "lumo_activation_sync_failed"}
	// Ties this refund to the subscription so a retried attempt (or a
	// duplicate Stripe webhook delivery racing the same failure) can't
	// double-refund.
	refundParams.SetIdempotencyKey("activation-failed-refund-" + stripeSubscriptionID)

	if _, err := s.sc.V1Refunds.Create(ctx, refundParams); err != nil {
		return err
	}

	_, err = s.sc.V1Subscriptions.Cancel(ctx, stripeSubscriptionID, &stripe.SubscriptionCancelParams{})
	return err
}

// CancelSubscription cancels a Stripe subscription with no refund attached
// — used when a Clerk account is deleted: the customer already had whatever
// access they paid for, so there's nothing to refund, but Stripe would
// otherwise keep billing them forever for an account that no longer exists
// on our side at all.
//
// Returns true if the subscription ended up canceled (including if it
// already was — nothing to do in that case).
func (s *Service) CancelSubscription(ctx context.Context, stripeSubscriptionID string) bool {
	if strings.TrimSpace(stripeSubscriptionID) == "" {
		return true
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := s.cancelIfNotAlready(ctx, stripeSubscriptionID); err != nil {
			log.Printf("refund: attempt %d/%d failed to cancel stripeSubscriptionId=%s during account deletion: %v", attempt, maxAttempts, stripeSubscriptionID, err)
			if attempt < maxAttempts {
				sleep(retryDelays[attempt-1])
			}
			continue
		}
		return true
	}

	log.Printf("refund: could not cancel stripeSubscriptionId=%s after account deletion after %d attempts — "+
		"customer has no account left to manage this from and will keep being billed, needs manual review in Stripe",
		stripeSubscriptionID, maxAttempts)
	return false
}

func (s *Service) cancelIfNotAlready(ctx context.Context, stripeSubscriptionID string) error {
	sub, err := s.sc.V1Subscriptions.Retrieve(ctx, stripeSubscriptionID, &stripe.SubscriptionRetrieveParams{})
	if err != nil {
		return err
	}
	if string(sub.Status) == "canceled" {
		return nil
	}
	if _, err := s.sc.V1Subscriptions.Cancel(ctx, stripeSubscriptionID, &stripe.SubscriptionCancelParams{}); err != nil {
		return err
	}
	log.Printf("refund: canceled stripeSubscriptionId=%s as part of account deletion", stripeSubscriptionID)
	return nil
}

// findRefundTarget mirrors RefundService.findRefundTarget: the
// subscription's latest invoice carries the payment that actually needs
// refunding, which as of recent Stripe API versions is an InvoicePayment
// (not a direct payment_intent field on the invoice itself), pointing at
// either a PaymentIntent or, for older-style charges, a Charge directly.
// Returns (nil, nil) when there's genuinely nothing to refund.
func (s *Service) findRefundTarget(ctx context.Context, stripeSubscriptionID string) (*stripe.RefundCreateParams, error) {
	sub, err := s.sc.V1Subscriptions.Retrieve(ctx, stripeSubscriptionID, &stripe.SubscriptionRetrieveParams{})
	if err != nil {
		return nil, err
	}
	if sub.LatestInvoice == nil {
		return nil, nil
	}

	list := s.sc.V1InvoicePayments.List(ctx, &stripe.InvoicePaymentListParams{
		Invoice: stripe.String(sub.LatestInvoice.ID),
	})

	var payment *stripe.InvoicePayment
	for p, err := range list.All(ctx) {
		if err != nil {
			return nil, err
		}
		payment = p
		break
	}
	if payment == nil || payment.Payment == nil {
		return nil, nil
	}

	switch payment.Payment.Type {
	case stripe.InvoicePaymentPaymentTypePaymentIntent:
		if payment.Payment.PaymentIntent == nil {
			return nil, nil
		}
		return &stripe.RefundCreateParams{PaymentIntent: stripe.String(payment.Payment.PaymentIntent.ID)}, nil
	case stripe.InvoicePaymentPaymentTypeCharge:
		if payment.Payment.Charge == nil {
			return nil, nil
		}
		return &stripe.RefundCreateParams{Charge: stripe.String(payment.Payment.Charge.ID)}, nil
	default:
		return nil, nil
	}
}

func sleep(d time.Duration) {
	time.Sleep(d)
}
