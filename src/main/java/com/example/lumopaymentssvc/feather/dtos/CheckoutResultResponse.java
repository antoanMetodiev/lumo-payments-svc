package com.example.lumopaymentssvc.feather.dtos;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * Answers "what happened to this specific Checkout attempt" — deliberately
 * keyed by the Stripe Checkout Session id, not by clerkId, since (unlike
 * GET /payments/subscription) there's no persisted row to read once an
 * attempt has failed and been refunded (see SubscriptionService). Stripe's
 * own Session object is the only remaining source of truth for that case.
 *
 * No FAILED value: this service has no reliable clock to decide "it's been
 * long enough, give up" (see CheckoutService.getCheckoutResult) — that's the
 * frontend's call, timed from when its own polling started, not from
 * anything server-side.
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
public class CheckoutResultResponse {

    private Result result;

    public enum Result {
        PENDING, SUCCEEDED
    }

    public static CheckoutResultResponse pending() {
        return CheckoutResultResponse.builder().result(Result.PENDING).build();
    }

    public static CheckoutResultResponse succeeded() {
        return CheckoutResultResponse.builder().result(Result.SUCCEEDED).build();
    }
}
