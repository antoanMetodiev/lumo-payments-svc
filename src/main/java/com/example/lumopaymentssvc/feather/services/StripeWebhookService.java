package com.example.lumopaymentssvc.feather.services;

import com.stripe.exception.EventDataObjectDeserializationException;
import com.stripe.model.Event;
import com.stripe.model.EventDataObjectDeserializer;
import com.stripe.model.StripeObject;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

/**
 * Reacts to the Stripe events this service subscribes to (see README.md).
 * Signature verification already happened in StripeWebhookController before
 * this is ever called.
 *
 * checkout.session.completed is deliberately not handled here — it only
 * proves Stripe Checkout finished, not that this system turned that into
 * actual Pro access, and SubscriptionService only ever creates a
 * `subscriptions` row once customer.subscription.{created,updated} reports
 * real status (see its own doc comment). Still subscribed to in the Stripe
 * dashboard for now (harmless, just falls through to the default case) —
 * fine to unsubscribe it there whenever convenient.
 */
@Slf4j
@Service
public class StripeWebhookService {

    private final SubscriptionService subscriptionService;

    public StripeWebhookService(SubscriptionService subscriptionService) {
        this.subscriptionService = subscriptionService;
    }

    public void handle(Event event) {
        switch (event.getType()) {
            case "customer.subscription.created", "customer.subscription.updated" -> handleSubscriptionUpdated(event);
            case "customer.subscription.deleted" -> handleSubscriptionDeleted(event);
            case "invoice.payment_failed" -> log.warn("Stripe invoice payment failed, event={}", event.getId());
            default -> { /* not something we track */ }
        }
    }

    private void handleSubscriptionUpdated(Event event) {
        com.stripe.model.Subscription subscription = deserialize(event, com.stripe.model.Subscription.class);
        if (subscription == null) return;

        subscriptionService.syncFromStripeSubscription(subscription);
    }

    private void handleSubscriptionDeleted(Event event) {
        com.stripe.model.Subscription subscription = deserialize(event, com.stripe.model.Subscription.class);
        if (subscription == null) return;

        subscriptionService.markCanceled(subscription.getId());
    }

    /**
     * getObject() comes back empty when the event was serialized against a
     * newer Stripe API version than this SDK's pinned default — deserializeUnsafe()
     * ignores that mismatch and parses the raw JSON directly instead, as
     * recommended by Stripe's own webhook guide.
     */
    @SuppressWarnings("unchecked")
    private <T extends StripeObject> T deserialize(Event event, Class<T> type) {
        EventDataObjectDeserializer deserializer = event.getDataObjectDeserializer();
        StripeObject stripeObject = deserializer.getObject().orElseGet(() -> {
            try {
                return deserializer.deserializeUnsafe();
            } catch (EventDataObjectDeserializationException e) {
                log.error("deserializeUnsafe() failed for event {} ({})", event.getId(), event.getType(), e);
                return null;
            }
        });

        if (!type.isInstance(stripeObject)) {
            log.warn("Unexpected payload for event {} ({}): expected {}, got {}",
                    event.getId(), event.getType(), type.getSimpleName(),
                    stripeObject == null ? "null" : stripeObject.getClass().getSimpleName());
            return null;
        }

        return (T) stripeObject;
    }
}
