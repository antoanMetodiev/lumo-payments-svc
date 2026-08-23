package com.example.lumopaymentssvc.feather.models;

import jakarta.persistence.*;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;
import lombok.experimental.Accessors;

import java.time.Instant;
import java.util.UUID;

/**
 * One row per Clerk user — this service's own record of their Stripe billing
 * state, kept in sync via webhooks (see StripeWebhookService). Named
 * "PaymentSubscription" rather than "Subscription" to avoid clashing with
 * com.stripe.model.Subscription, which every service class here also imports.
 */
@Entity
@Table(name = "subscriptions")
@Data
@Accessors(chain = true)
@Builder
@AllArgsConstructor
@NoArgsConstructor
public class PaymentSubscription {

    @Id
    @GeneratedValue(strategy = GenerationType.UUID)
    private UUID id;

    @Column(name = "clerk_id", nullable = false, unique = true, updatable = false)
    private String clerkId;

    @Column(name = "stripe_customer_id", unique = true)
    private String stripeCustomerId;

    @Column(name = "stripe_subscription_id", unique = true)
    private String stripeSubscriptionId;

    @Column(name = "stripe_price_id")
    private String stripePriceId;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    @Builder.Default
    private SubscriptionStatus status = SubscriptionStatus.NONE;

    @Column(name = "current_period_end")
    private Instant currentPeriodEnd;

    @Column(name = "cancel_at_period_end", nullable = false)
    @Builder.Default
    private boolean cancelAtPeriodEnd = false;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    /**
     * TRIALING counts as Pro access too — Stripe still considers the
     * subscription current, it just hasn't been charged yet.
     */
    public PlanType plan() {
        return (status == SubscriptionStatus.ACTIVE || status == SubscriptionStatus.TRIALING)
                ? PlanType.PRO
                : PlanType.FREE;
    }
}
