package billingportal

import "testing"

func TestNoStripeCustomerError_Error(t *testing.T) {
	err := &NoStripeCustomerError{ClerkID: "user_123"}
	want := "no Stripe customer on file for clerkId=user_123"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
