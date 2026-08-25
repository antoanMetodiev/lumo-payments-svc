package subscriptions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	"lumo-payments-svc/internal/models"
)

// --- firstItem ---

func TestFirstItem_NilItems_ReturnsNil(t *testing.T) {
	sub := &stripe.Subscription{}
	if got := firstItem(sub); got != nil {
		t.Errorf("firstItem() = %v, want nil", got)
	}
}

func TestFirstItem_EmptyData_ReturnsNil(t *testing.T) {
	sub := &stripe.Subscription{Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{}}}
	if got := firstItem(sub); got != nil {
		t.Errorf("firstItem() = %v, want nil", got)
	}
}

func TestFirstItem_ReturnsFirstElement(t *testing.T) {
	first := &stripe.SubscriptionItem{ID: "si_1"}
	second := &stripe.SubscriptionItem{ID: "si_2"}
	sub := &stripe.Subscription{Items: &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{first, second}}}

	got := firstItem(sub)
	if got == nil || got.ID != "si_1" {
		t.Errorf("firstItem() = %v, want the first item (si_1)", got)
	}
}

// --- activationContext ---

func TestActivationContext_MapsAllFields(t *testing.T) {
	id := uuid.New()
	sub := &models.PaymentSubscription{
		ID:                   id,
		ClerkID:              "user_123",
		Status:               models.StatusActive,
		StripeSubscriptionID: strPtr("sub_abc"),
		StripeCustomerID:     strPtr("cus_abc"),
	}

	ac := activationContext(sub)

	if ac.SubscriptionID != id {
		t.Errorf("SubscriptionID = %v, want %v", ac.SubscriptionID, id)
	}
	if ac.ClerkID != "user_123" {
		t.Errorf("ClerkID = %q, want %q", ac.ClerkID, "user_123")
	}
	if ac.Plan != models.PlanPro {
		t.Errorf("Plan = %q, want %q (derived from sub.Plan())", ac.Plan, models.PlanPro)
	}
	if ac.StripeSubscriptionID != "sub_abc" {
		t.Errorf("StripeSubscriptionID = %q, want %q", ac.StripeSubscriptionID, "sub_abc")
	}
	if ac.StripeCustomerID != "cus_abc" {
		t.Errorf("StripeCustomerID = %q, want %q", ac.StripeCustomerID, "cus_abc")
	}
}

// TestActivationContext_NilStripeIDs_LeavesEmptyStrings documents that a
// subscription without a linked Stripe subscription/customer id yet
// (shouldn't normally happen once synced, but defensively handled) maps to
// empty strings rather than panicking on the nil pointer deref.
func TestActivationContext_NilStripeIDs_LeavesEmptyStrings(t *testing.T) {
	sub := &models.PaymentSubscription{ClerkID: "user_456", Status: models.StatusNone}

	ac := activationContext(sub)

	if ac.StripeSubscriptionID != "" {
		t.Errorf("StripeSubscriptionID = %q, want empty string", ac.StripeSubscriptionID)
	}
	if ac.StripeCustomerID != "" {
		t.Errorf("StripeCustomerID = %q, want empty string", ac.StripeCustomerID)
	}
	if ac.Plan != models.PlanFree {
		t.Errorf("Plan = %q, want %q", ac.Plan, models.PlanFree)
	}
}

func strPtr(s string) *string { return &s }
