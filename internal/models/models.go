// Package models mirrors feather.models.PaymentSubscription (and its
// PlanType/SubscriptionStatus enums) field-for-field.
package models

import (
	"time"

	"github.com/google/uuid"
)

// PlanType mirrors feather.models.PlanType.
type PlanType string

const (
	PlanFree PlanType = "FREE"
	PlanPro  PlanType = "PRO"
)

// SubscriptionStatus mirrors feather.models.SubscriptionStatus — a local
// mirror of Stripe's own subscription statuses, plus NONE for a Clerk user
// who has never started checkout.
type SubscriptionStatus string

const (
	StatusNone              SubscriptionStatus = "NONE"
	StatusIncomplete        SubscriptionStatus = "INCOMPLETE"
	StatusIncompleteExpired SubscriptionStatus = "INCOMPLETE_EXPIRED"
	StatusTrialing          SubscriptionStatus = "TRIALING"
	StatusActive            SubscriptionStatus = "ACTIVE"
	StatusPastDue           SubscriptionStatus = "PAST_DUE"
	StatusCanceled          SubscriptionStatus = "CANCELED"
	StatusUnpaid            SubscriptionStatus = "UNPAID"
	StatusPaused            SubscriptionStatus = "PAUSED"
)

// StatusFromStripe mirrors SubscriptionStatus.fromStripeStatus.
func StatusFromStripe(stripeStatus string) SubscriptionStatus {
	switch stripeStatus {
	case "incomplete":
		return StatusIncomplete
	case "incomplete_expired":
		return StatusIncompleteExpired
	case "trialing":
		return StatusTrialing
	case "active":
		return StatusActive
	case "past_due":
		return StatusPastDue
	case "canceled":
		return StatusCanceled
	case "unpaid":
		return StatusUnpaid
	case "paused":
		return StatusPaused
	default:
		return StatusNone
	}
}

// PaymentSubscription mirrors feather.models.PaymentSubscription — one row
// per Clerk user, this service's own record of their Stripe billing state,
// kept in sync via webhooks (see internal/webhook and internal/subscriptions).
type PaymentSubscription struct {
	ID                   uuid.UUID
	ClerkID              string
	StripeCustomerID     *string
	StripeSubscriptionID *string
	StripePriceID        *string
	Status               SubscriptionStatus
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Plan mirrors PaymentSubscription.plan() — TRIALING counts as Pro access
// too, Stripe still considers the subscription current, it just hasn't been
// charged yet.
func (s *PaymentSubscription) Plan() PlanType {
	if s.Status == StatusActive || s.Status == StatusTrialing {
		return PlanPro
	}
	return PlanFree
}
