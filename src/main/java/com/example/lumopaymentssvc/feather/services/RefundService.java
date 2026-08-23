package com.example.lumopaymentssvc.feather.services;

import com.stripe.exception.StripeException;
import com.stripe.model.InvoicePayment;
import com.stripe.model.InvoicePaymentCollection;
import com.stripe.model.Refund;
import com.stripe.model.Subscription;
import com.stripe.net.RequestOptions;
import com.stripe.param.InvoicePaymentListParams;
import com.stripe.param.RefundCreateParams;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.util.List;

/**
 * Last resort when we've confirmed Stripe took the customer's money but
 * couldn't confirm they actually got Pro access (see
 * SubscriptionActivationService) — refunds the payment behind the
 * subscription's most recent invoice in full and cancels the subscription so
 * it doesn't keep billing them for something they can't use.
 */
@Slf4j
@Service
public class RefundService {

    private static final int MAX_ATTEMPTS = 3;
    private static final List<Duration> RETRY_DELAYS = List.of(Duration.ofSeconds(1), Duration.ofSeconds(2));

    /**
     * Retries a few times over a few seconds, same as UserSyncService — most
     * failures here are a transient Stripe API blip, not a reason to give up
     * immediately. Safe to retry: the idempotency key on the refund itself
     * means a retried Refund.create() can't double-refund.
     *
     * @return true if both the refund and the cancellation succeeded.
     */
    public boolean refundAndCancel(String stripeSubscriptionId) {
        if (stripeSubscriptionId == null || stripeSubscriptionId.isBlank()) {
            log.error("Cannot refund — no stripeSubscriptionId");
            return false;
        }

        for (int attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
            try {
                RefundCreateParams.Builder refundParams = findRefundTarget(stripeSubscriptionId);
                if (refundParams == null) {
                    // Not a transient failure — the same lookup will return the
                    // same "nothing to refund" every time, so retrying is pointless.
                    log.error("No payment found to refund for stripeSubscriptionId={} — needs manual review", stripeSubscriptionId);
                    return false;
                }

                // Idempotency key ties this refund to the subscription so a retried
                // attempt (or a duplicate Stripe webhook delivery racing the same
                // failure) can't double-refund.
                RequestOptions idempotent = RequestOptions.builder()
                        .setIdempotencyKey("activation-failed-refund-" + stripeSubscriptionId)
                        .build();

                Refund.create(
                        refundParams.putMetadata("reason", "lumo_activation_sync_failed").build(),
                        idempotent
                );

                Subscription.retrieve(stripeSubscriptionId).cancel();

                log.warn("Refunded and canceled stripeSubscriptionId={} after activation sync failure", stripeSubscriptionId);
                return true;
            } catch (StripeException e) {
                log.error("Attempt {}/{} failed to refund/cancel stripeSubscriptionId={}",
                        attempt, MAX_ATTEMPTS, stripeSubscriptionId, e);
                if (attempt < MAX_ATTEMPTS) {
                    sleep(RETRY_DELAYS.get(attempt - 1));
                }
            }
        }

        log.error("Refund/cancel failed for stripeSubscriptionId={} after {} attempts — needs manual review",
                stripeSubscriptionId, MAX_ATTEMPTS);
        return false;
    }

    /**
     * Cancels a Stripe subscription with no refund attached — used when a
     * Clerk account is deleted (see SubscriptionService.deleteByClerkId):
     * the customer already had whatever access they paid for, so there's
     * nothing to refund, but Stripe would otherwise keep billing them
     * forever for an account that no longer exists on our side at all.
     *
     * Same retry approach as refundAndCancel — a transient Stripe API blip
     * shouldn't leave someone stuck being billed with no account left to
     * manage it from.
     *
     * @return true if the subscription ended up canceled (including if it
     * already was — nothing to do in that case).
     */
    public boolean cancelSubscription(String stripeSubscriptionId) {
        if (stripeSubscriptionId == null || stripeSubscriptionId.isBlank()) {
            return true;
        }

        for (int attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
            try {
                Subscription subscription = Subscription.retrieve(stripeSubscriptionId);
                if (!"canceled".equals(subscription.getStatus())) {
                    subscription.cancel();
                    log.warn("Canceled stripeSubscriptionId={} as part of account deletion", stripeSubscriptionId);
                }
                return true;
            } catch (StripeException e) {
                log.error("Attempt {}/{} failed to cancel stripeSubscriptionId={} during account deletion",
                        attempt, MAX_ATTEMPTS, stripeSubscriptionId, e);
                if (attempt < MAX_ATTEMPTS) {
                    sleep(RETRY_DELAYS.get(attempt - 1));
                }
            }
        }

        log.error("Could not cancel stripeSubscriptionId={} after account deletion after {} attempts — " +
                "customer has no account left to manage this from and will keep being billed, needs manual review in Stripe",
                stripeSubscriptionId, MAX_ATTEMPTS);
        return false;
    }

    /**
     * The subscription's latest invoice carries the payment that actually
     * needs refunding — as of recent Stripe API versions that's an
     * InvoicePayment (not a direct payment_intent field on the invoice
     * itself), which can point at either a PaymentIntent or, for older-style
     * charges, a Charge directly.
     */
    private RefundCreateParams.Builder findRefundTarget(String stripeSubscriptionId) throws StripeException {
        Subscription subscription = Subscription.retrieve(stripeSubscriptionId);
        String latestInvoiceId = subscription.getLatestInvoice();
        if (latestInvoiceId == null) {
            return null;
        }

        InvoicePaymentCollection payments = InvoicePayment.list(
                InvoicePaymentListParams.builder().setInvoice(latestInvoiceId).build()
        );
        if (payments.getData().isEmpty()) {
            return null;
        }

        InvoicePayment.Payment payment = payments.getData().get(0).getPayment();
        if (payment == null) {
            return null;
        }

        if ("payment_intent".equals(payment.getType()) && payment.getPaymentIntent() != null) {
            return RefundCreateParams.builder().setPaymentIntent(payment.getPaymentIntent());
        }
        if ("charge".equals(payment.getType()) && payment.getCharge() != null) {
            return RefundCreateParams.builder().setCharge(payment.getCharge());
        }

        return null;
    }

    private void sleep(Duration duration) {
        try {
            Thread.sleep(duration.toMillis());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
