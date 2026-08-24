// Package usersync mirrors feather.services.UserSyncService: tells
// lumo-user-svc — via lumo-api-gateway, same as everything else in this
// system — to flip a Clerk user's subscription_type after a Stripe webhook
// changes their plan. There's no live Clerk session for this call (Stripe
// triggered it), so it goes through the gateway's /internal path
// (X-Internal-Api-Key only) instead of the usual Clerk-session-based routes.
//
// Retries a few times over a few seconds — not a 30+ minute backoff
// schedule — because a user staring at their account page right after
// paying will conclude within seconds that they got scammed, not that a
// background job will fix it eventually. If it still hasn't landed after
// that, the caller (internal/subscriptions.ActivationService) treats it as
// a hard failure and refunds instead of leaving the customer
// paid-but-locked-out.
package usersync

import (
	"context"
	"log"
	"net/url"
	"time"

	"lumo-payments-svc/internal/gatewayclient"
	"lumo-payments-svc/internal/models"
)

const maxAttempts = 3

var retryDelays = []time.Duration{time.Second, 2 * time.Second}

type Service struct {
	gateway *gatewayclient.Client
}

func New(gateway *gatewayclient.Client) *Service {
	return &Service{gateway: gateway}
}

// SyncSubscriptionType returns true once lumo-user-svc confirms the update,
// false if all attempts failed.
func (s *Service) SyncSubscriptionType(ctx context.Context, clerkID string, plan models.PlanType) bool {
	subscriptionType := "REGULAR_USER"
	if plan == models.PlanPro {
		subscriptionType = "PRO_USER"
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// POST, not PATCH — the gateway's own Feign client (used for the next
		// hop, to lumo-user-svc) can't send PATCH at all, so this endpoint is
		// POST end-to-end for consistency.
		path := "/internal/users/" + url.PathEscape(clerkID) + "/subscription"
		status, err := s.gateway.PostJSON(ctx, path, map[string]string{"subscriptionType": subscriptionType})
		if err == nil && status >= 200 && status < 300 {
			return true
		}

		log.Printf("usersync: attempt %d/%d failed to sync subscriptionType=%s to lumo-user-svc for clerkId=%s (status=%d): %v",
			attempt, maxAttempts, subscriptionType, clerkID, status, err)
		if attempt < maxAttempts {
			time.Sleep(retryDelays[attempt-1])
		}
	}

	return false
}
