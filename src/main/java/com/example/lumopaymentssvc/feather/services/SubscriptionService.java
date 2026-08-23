package com.example.lumopaymentssvc.feather.services;

import com.example.lumopaymentssvc.feather.models.PaymentSubscription;
import com.example.lumopaymentssvc.feather.models.SubscriptionStatus;
import com.example.lumopaymentssvc.feather.repositories.PaymentSubscriptionRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

import java.time.Instant;
import java.util.Optional;

/**
 * Owns reads and writes of the local `subscriptions` table. Reads back
 * PaymentSubscriptionController (via SubscriptionResponse); writes come from
 * StripeWebhookService, since Stripe — not this service — is the source of
 * truth for status/period/cancellation. Every write that can change which
 * plan the user is on also tells lumo-user-svc, via
 * SubscriptionActivationService (which retries and, if that still doesn't
 * land, refunds the customer rather than leave them paid-but-locked-out —
 * see its own doc comment).
 *
 * A row is only ever created once Stripe reports real subscription status
 * (in syncFromStripeSubscription) — never speculatively on
 * checkout.session.completed, which only proves Checkout finished, not that
 * this system actually managed to turn that into working Pro access. If
 * activation then fails, SubscriptionActivationService deletes the row again
 * rather than leaving a dummy record behind for a customer who got refunded
 * and may never come back.
 */
@Slf4j
@Service
public class SubscriptionService {

    private final PaymentSubscriptionRepository paymentSubscriptionRepository;
    private final SubscriptionActivationService activationService;
    private final RefundService refundService;

    public SubscriptionService(
            PaymentSubscriptionRepository paymentSubscriptionRepository,
            SubscriptionActivationService activationService,
            RefundService refundService
    ) {
        this.paymentSubscriptionRepository = paymentSubscriptionRepository;
        this.activationService = activationService;
        this.refundService = refundService;
    }

    public Optional<PaymentSubscription> findByClerkId(String clerkId) {
        return paymentSubscriptionRepository.findByClerkId(clerkId);
    }

    /**
     * Called when lumo-user-svc tells us (via the gateway) that a Clerk
     * account was deleted — see InternalSubscriptionController. If they still
     * have a live Stripe subscription, it gets canceled (no refund — they
     * already had whatever access they paid for; this is just to stop
     * billing an account that no longer exists at all on our side). Not
     * wrapped in @Transactional: findByClerkId/deleteByClerkId are each
     * already transactional on their own (Spring Data JPA), and this
     * shouldn't hold a DB transaction open for the duration of the Stripe
     * call (which retries over a few seconds — see RefundService).
     */
    public void deleteByClerkId(String clerkId) {
        paymentSubscriptionRepository.findByClerkId(clerkId)
                .map(PaymentSubscription::getStripeSubscriptionId)
                .ifPresent(refundService::cancelSubscription);

        paymentSubscriptionRepository.deleteByClerkId(clerkId);
    }

    /**
     * customer.subscription.{created,updated} — the first point at which
     * Stripe tells us a real status, so this is the only place a
     * `subscriptions` row is ever created (see class doc). Looked up by
     * Stripe subscription id first (the common case for a returning
     * customer); falls back to the clerkId CheckoutService stamped into the
     * Stripe subscription's own metadata at creation time for a brand new one.
     *
     * Deliberately does NOT create a fresh row for a subscription whose
     * incoming status is already "canceled" if we have no existing record of
     * it — that's exactly what happens right after RefundService.cancelSubscription
     * cancels someone's subscription as part of deleting their account:
     * Stripe fires this same event back at us moments after we've already
     * deleted their row, and without this guard it would get silently
     * resurrected here for a clerkId that no longer has an account at all.
     */
    @Transactional
    public void syncFromStripeSubscription(com.stripe.model.Subscription stripeSubscription) {
        String stripeSubscriptionId = stripeSubscription.getId();
        String clerkIdFromMetadata = stripeSubscription.getMetadata() != null
                ? stripeSubscription.getMetadata().get("clerkId")
                : null;

        Optional<PaymentSubscription> existing = paymentSubscriptionRepository.findByStripeSubscriptionId(stripeSubscriptionId);
        if (existing.isEmpty() && clerkIdFromMetadata != null) {
            existing = paymentSubscriptionRepository.findByClerkId(clerkIdFromMetadata);
        }

        PaymentSubscription subscription;
        if (existing.isPresent()) {
            subscription = existing.get();
        } else if (clerkIdFromMetadata == null) {
            log.warn("Received subscription update for unlinked subscription {} with no clerkId metadata — cannot create a record", stripeSubscriptionId);
            return;
        } else if ("canceled".equals(stripeSubscription.getStatus())) {
            log.warn("Ignoring canceled-status update for unlinked subscription {} (clerkId metadata: {}) — " +
                    "likely a stray webhook arriving after that account was already deleted", stripeSubscriptionId, clerkIdFromMetadata);
            return;
        } else {
            subscription = PaymentSubscription.builder()
                    .clerkId(clerkIdFromMetadata)
                    .status(SubscriptionStatus.NONE)
                    .createdAt(Instant.now())
                    .build();
        }

        Optional<com.stripe.model.SubscriptionItem> firstItem = firstItem(stripeSubscription);
        Long periodEndEpochSeconds = firstItem.map(com.stripe.model.SubscriptionItem::getCurrentPeriodEnd).orElse(null);

        subscription
                .setStripeSubscriptionId(stripeSubscriptionId)
                .setStripeCustomerId(stripeSubscription.getCustomer())
                .setStripePriceId(firstItem.map(item -> item.getPrice().getId()).orElse(null))
                .setStatus(SubscriptionStatus.fromStripeStatus(stripeSubscription.getStatus()))
                .setCancelAtPeriodEnd(Boolean.TRUE.equals(stripeSubscription.getCancelAtPeriodEnd()))
                .setCurrentPeriodEnd(periodEndEpochSeconds != null ? Instant.ofEpochSecond(periodEndEpochSeconds) : null)
                .setUpdatedAt(Instant.now());

        PaymentSubscription saved = paymentSubscriptionRepository.save(subscription);
        activationService.syncOrEscalate(activationContext(saved));
    }

    /**
     * customer.subscription.deleted — Stripe only fires this once the
     * subscription is truly gone (not on cancel-at-period-end, which instead
     * arrives as a customer.subscription.updated with cancelAtPeriodEnd=true
     * and the still-current status).
     */
    @Transactional
    public void markCanceled(String stripeSubscriptionId) {
        paymentSubscriptionRepository.findByStripeSubscriptionId(stripeSubscriptionId).ifPresentOrElse(
                subscription -> {
                    subscription.setStatus(SubscriptionStatus.CANCELED).setUpdatedAt(Instant.now());
                    paymentSubscriptionRepository.save(subscription);
                    activationService.syncOrEscalate(activationContext(subscription));
                },
                () -> log.warn("customer.subscription.deleted for unknown subscription {}", stripeSubscriptionId)
        );
    }

    private SubscriptionActivationService.ActivationSyncContext activationContext(PaymentSubscription subscription) {
        return new SubscriptionActivationService.ActivationSyncContext(
                subscription.getId(),
                subscription.getClerkId(),
                subscription.plan(),
                subscription.getStripeSubscriptionId(),
                subscription.getStripeCustomerId()
        );
    }

    // current_period_end (and the price actually being billed) live on each
    // subscription item as of newer Stripe API versions, not on the
    // subscription itself — a single-price subscription (the only kind this
    // service creates) always has exactly one.
    private Optional<com.stripe.model.SubscriptionItem> firstItem(com.stripe.model.Subscription stripeSubscription) {
        if (stripeSubscription.getItems() == null || stripeSubscription.getItems().getData().isEmpty()) {
            return Optional.empty();
        }
        return Optional.of(stripeSubscription.getItems().getData().get(0));
    }
}
