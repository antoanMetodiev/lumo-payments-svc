package com.example.lumopaymentssvc.feather.dtos;

import com.example.lumopaymentssvc.feather.models.PaymentSubscription;
import com.example.lumopaymentssvc.feather.models.PlanType;
import com.example.lumopaymentssvc.feather.models.SubscriptionStatus;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.Instant;

/**
 * Deliberately doesn't expose Stripe customer/subscription ids to the
 * frontend — those are internal wiring, not something the UI needs.
 */
@Data
@Builder
@AllArgsConstructor
@NoArgsConstructor
public class SubscriptionResponse {

    private PlanType plan;
    private SubscriptionStatus status;
    private Instant currentPeriodEnd;
    private boolean cancelAtPeriodEnd;

    public static SubscriptionResponse from(PaymentSubscription subscription) {
        return SubscriptionResponse.builder()
                .plan(subscription.plan())
                .status(subscription.getStatus())
                .currentPeriodEnd(subscription.getCurrentPeriodEnd())
                .cancelAtPeriodEnd(subscription.isCancelAtPeriodEnd())
                .build();
    }

    public static SubscriptionResponse none() {
        return SubscriptionResponse.builder()
                .plan(PlanType.FREE)
                .status(SubscriptionStatus.NONE)
                .cancelAtPeriodEnd(false)
                .build();
    }
}
