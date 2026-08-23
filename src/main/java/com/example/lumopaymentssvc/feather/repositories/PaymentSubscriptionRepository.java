package com.example.lumopaymentssvc.feather.repositories;

import com.example.lumopaymentssvc.feather.models.PaymentSubscription;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;
import java.util.UUID;

@Repository
public interface PaymentSubscriptionRepository extends JpaRepository<PaymentSubscription, UUID> {

    Optional<PaymentSubscription> findByClerkId(String clerkId);

    Optional<PaymentSubscription> findByStripeCustomerId(String stripeCustomerId);

    Optional<PaymentSubscription> findByStripeSubscriptionId(String stripeSubscriptionId);

    // Account deletion cascade (see InternalSubscriptionController) — a no-op
    // if this clerkId never had a row (e.g. they never subscribed, or their
    // one attempt failed and got cleaned up already).
    void deleteByClerkId(String clerkId);
}
