package models

import "testing"

// --- StatusFromStripe ---

func TestStatusFromStripe_KnownStatuses(t *testing.T) {
	cases := []struct {
		stripeStatus string
		want         SubscriptionStatus
	}{
		{"incomplete", StatusIncomplete},
		{"incomplete_expired", StatusIncompleteExpired},
		{"trialing", StatusTrialing},
		{"active", StatusActive},
		{"past_due", StatusPastDue},
		{"canceled", StatusCanceled},
		{"unpaid", StatusUnpaid},
		{"paused", StatusPaused},
	}

	for _, tc := range cases {
		t.Run(tc.stripeStatus, func(t *testing.T) {
			if got := StatusFromStripe(tc.stripeStatus); got != tc.want {
				t.Errorf("StatusFromStripe(%q) = %q, want %q", tc.stripeStatus, got, tc.want)
			}
		})
	}
}

func TestStatusFromStripe_UnknownStatus_DefaultsToNone(t *testing.T) {
	for _, in := range []string{"", "some_future_stripe_status", "ACTIVE", "Active"} {
		if got := StatusFromStripe(in); got != StatusNone {
			t.Errorf("StatusFromStripe(%q) = %q, want %q (unknown/wrong-case values default to NONE)", in, got, StatusNone)
		}
	}
}

// --- PaymentSubscription.Plan ---

func TestPlan_ActiveIsPro(t *testing.T) {
	s := &PaymentSubscription{Status: StatusActive}
	if got := s.Plan(); got != PlanPro {
		t.Errorf("Plan() = %q, want %q for ACTIVE", got, PlanPro)
	}
}

// TestPlan_TrialingIsPro documents the deliberate inclusion: Stripe still
// considers a trialing subscription current even though it hasn't been
// charged yet, so trial users get Pro access too.
func TestPlan_TrialingIsPro(t *testing.T) {
	s := &PaymentSubscription{Status: StatusTrialing}
	if got := s.Plan(); got != PlanPro {
		t.Errorf("Plan() = %q, want %q for TRIALING", got, PlanPro)
	}
}

func TestPlan_EveryOtherStatusIsFree(t *testing.T) {
	statuses := []SubscriptionStatus{
		StatusNone, StatusIncomplete, StatusIncompleteExpired, StatusPastDue,
		StatusCanceled, StatusUnpaid, StatusPaused, SubscriptionStatus("unknown"),
	}
	for _, status := range statuses {
		s := &PaymentSubscription{Status: status}
		if got := s.Plan(); got != PlanFree {
			t.Errorf("Plan() = %q, want %q for status %q", got, PlanFree, status)
		}
	}
}
