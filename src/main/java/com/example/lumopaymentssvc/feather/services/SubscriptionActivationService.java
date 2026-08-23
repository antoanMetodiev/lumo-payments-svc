package com.example.lumopaymentssvc.feather.services;

import com.example.lumopaymentssvc.feather.models.PlanType;
import com.example.lumopaymentssvc.feather.repositories.PaymentSubscriptionRepository;
import com.stripe.exception.StripeException;
import com.stripe.model.Customer;
import lombok.extern.slf4j.Slf4j;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

import java.util.UUID;

/**
 * Reconciles what Stripe just told us (via SubscriptionService) with what
 * lumo-user-svc actually knows about the user, and decides what to do when
 * those two can't be brought in sync.
 *
 * Runs off the webhook request thread (@Async) — StripeWebhookController
 * already returned 200 to Stripe by the time this runs, so a slow refund
 * call here can't make Stripe think the whole webhook delivery failed and
 * retry it (which would just repeat everything, including the refund
 * attempt — the idempotency key on RefundService covers that, but there's no
 * reason to depend on it when responding fast avoids the race entirely).
 *
 * Needs to be a separate bean from SubscriptionService: @Async only takes
 * effect through the Spring proxy, so it can't be a self-invoked method on
 * the same class that calls it.
 */
@Slf4j
@Service
public class SubscriptionActivationService {

    private final UserSyncService userSyncService;
    private final RefundService refundService;
    private final EmailService emailService;
    private final PaymentSubscriptionRepository paymentSubscriptionRepository;

    public SubscriptionActivationService(
            UserSyncService userSyncService,
            RefundService refundService,
            EmailService emailService,
            PaymentSubscriptionRepository paymentSubscriptionRepository
    ) {
        this.userSyncService = userSyncService;
        this.refundService = refundService;
        this.emailService = emailService;
        this.paymentSubscriptionRepository = paymentSubscriptionRepository;
    }

    /**
     * Context is passed as plain values, not the PaymentSubscription entity
     * itself — the entity belongs to the caller's (now-closed) transaction
     * and persistence context, and this runs on a different thread.
     */
    public record ActivationSyncContext(
            UUID subscriptionId,
            String clerkId,
            PlanType plan,
            String stripeSubscriptionId,
            String stripeCustomerId
    ) {
    }

    @Async
    public void syncOrEscalate(ActivationSyncContext context) {
        boolean synced = userSyncService.syncSubscriptionType(context.clerkId(), context.plan());
        if (synced) {
            return;
        }

        if (context.plan() == PlanType.PRO) {
            // The customer paid and Stripe confirms it, but lumo-user-svc never
            // learned it — refund rather than leave them paid-but-locked-out.
            handleActivationFailure(context);
        } else {
            // Failing to revoke isn't a money problem (nobody lost anything —
            // if anything they keep Pro access a bit longer than entitled), so
            // no refund and no email — just a loud log line for whoever's
            // watching the logs to catch and fix by hand.
            log.error("Could not revoke Pro access in lumo-user-svc for clerkId={} stripeSubscriptionId={} after retries — " +
                    "they may retain Pro access longer than entitled, needs manual fix",
                    context.clerkId(), context.stripeSubscriptionId());
        }
    }

    // Not @Transactional: called via self-invocation from syncOrEscalate
    // within the same bean, which bypasses Spring's proxy — the annotation
    // would silently do nothing here. Not needed anyway: findById/save below
    // are each already transactional on their own (Spring Data JPA wraps
    // every repository method individually), and there's no multi-table
    // write here that needs the two to commit atomically together.
    private void handleActivationFailure(ActivationSyncContext context) {
        log.error("Could not activate Pro for clerkId={} after retries — refunding", context.clerkId());

        boolean refunded = refundService.refundAndCancel(context.stripeSubscriptionId());

        // No persisted trace of a failed activation — the customer got
        // refunded, so there's no real subscription relationship left to
        // record. Must not block the email step below if it fails (learned
        // that the hard way once already: a DB error here used to throw
        // straight through this method and skip the email entirely, even
        // though the refund itself had already gone through fine).
        deleteFailedSubscriptionRow(context.subscriptionId());

        if (refunded) {
            emailService.sendActivationFailedEmail(fetchCustomerEmail(context.stripeCustomerId()));
        } else {
            // Money is still charged and access still isn't granted, and
            // RefundService already retried a few times on its own — this is
            // as far as automation gets. Loud log line, no email (no ops
            // inbox wired up): needs a human to handle it by hand in Stripe.
            log.error("Refund FAILED after retries for clerkId={} stripeSubscriptionId={} — " +
                    "customer was charged but has no Pro access and no automatic refund, needs manual review",
                    context.clerkId(), context.stripeSubscriptionId());
        }
    }

    private void deleteFailedSubscriptionRow(UUID subscriptionId) {
        try {
            paymentSubscriptionRepository.deleteById(subscriptionId);
        } catch (RuntimeException e) {
            log.error("Failed to delete subscriptionId={} after activation failure — " +
                    "refund/email still proceed regardless, but a stale row may remain, needs manual cleanup",
                    subscriptionId, e);
        }
    }

    private String fetchCustomerEmail(String stripeCustomerId) {
        if (stripeCustomerId == null) {
            return null;
        }

        try {
            return Customer.retrieve(stripeCustomerId).getEmail();
        } catch (StripeException e) {
            log.error("Could not fetch customer email for stripeCustomerId={}", stripeCustomerId, e);
            return null;
        }
    }
}
