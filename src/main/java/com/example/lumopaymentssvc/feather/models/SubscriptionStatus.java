package com.example.lumopaymentssvc.feather.models;

/**
 * Local mirror of Stripe's subscription statuses (see
 * https://docs.stripe.com/api/subscriptions/object#subscription_object-status),
 * plus NONE for a Clerk user who has never started checkout — the default
 * Free-plan state, which has no Stripe object at all.
 *
 * No "activation failed" state here on purpose: if
 * SubscriptionActivationService ever has to refund a customer because it
 * couldn't confirm Pro access, it deletes the row entirely rather than
 * leaving a dummy record behind — see its own doc comment. A failed
 * checkout/(sync attempt just means no row exists at all, same as someone
 * who never tried.
 */
public enum SubscriptionStatus {
    NONE,
    INCOMPLETE,
    INCOMPLETE_EXPIRED,
    TRIALING,
    ACTIVE,
    PAST_DUE,
    CANCELED,
    UNPAID,
    PAUSED;

    public static SubscriptionStatus fromStripeStatus(String stripeStatus) {
        if (stripeStatus == null) return NONE;

        return switch (stripeStatus) {
            case "incomplete" -> INCOMPLETE;
            case "incomplete_expired" -> INCOMPLETE_EXPIRED;
            case "trialing" -> TRIALING;
            case "active" -> ACTIVE;
            case "past_due" -> PAST_DUE;
            case "canceled" -> CANCELED;
            case "unpaid" -> UNPAID;
            case "paused" -> PAUSED;
            default -> NONE;
        };
    }
}
