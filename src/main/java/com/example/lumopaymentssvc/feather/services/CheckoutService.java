package com.example.lumopaymentssvc.feather.services;

import com.example.lumopaymentssvc.feather.dtos.CheckoutResultResponse;
import com.example.lumopaymentssvc.feather.dtos.UserSubscriptionResponse;
import com.example.lumopaymentssvc.feather.models.PaymentSubscription;
import com.example.lumopaymentssvc.feather.repositories.PaymentSubscriptionRepository;
import com.stripe.exception.StripeException;
import com.stripe.model.checkout.Session;
import com.stripe.param.checkout.SessionCreateParams;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestClient;
import org.springframework.web.client.RestClientException;

import java.util.Optional;

@Slf4j
@Service
public class CheckoutService {

    private final PaymentSubscriptionRepository paymentSubscriptionRepository;
    private final RestClient gatewayRestClient;
    private final String proPriceId;
    private final String successUrl;
    private final String cancelUrl;

    public CheckoutService(
            PaymentSubscriptionRepository paymentSubscriptionRepository,
            RestClient gatewayRestClient,
            @Value("${stripe.pro-price-id}") String proPriceId,
            @Value("${stripe.success-url}") String successUrl,
            @Value("${stripe.cancel-url}") String cancelUrl
    ) {
        this.paymentSubscriptionRepository = paymentSubscriptionRepository;
        this.gatewayRestClient = gatewayRestClient;
        this.proPriceId = proPriceId;
        this.successUrl = successUrl;
        this.cancelUrl = cancelUrl;
    }

    /**
     * Creates a Stripe Checkout Session for the Pro plan and returns its URL
     * for the frontend to redirect the browser to.
     *
     * Nothing is written to our own database here — starting or abandoning
     * checkout leaves no trace. A `subscriptions` row only ever appears once
     * Stripe reports real, confirmed status via customer.subscription.{created,updated}
     * (see SubscriptionService.syncFromStripeSubscription) — not on
     * checkout.session.completed, which only proves Checkout finished, not
     * that this system actually turned it into working Pro access.
     *
     * If this clerkId already has a linked Stripe Customer (from a previous
     * completed checkout), that customer is reused so Stripe doesn't end up
     * with duplicate Customer records for the same person. Otherwise Stripe
     * creates the Customer itself once/if checkout actually completes.
     */
    public String createCheckoutSessionUrl(String clerkId, String email) throws StripeException {
        String existingStripeCustomerId = paymentSubscriptionRepository.findByClerkId(clerkId)
                .map(PaymentSubscription::getStripeCustomerId)
                .orElse(null);

        SessionCreateParams.Builder params = SessionCreateParams.builder()
                .setMode(SessionCreateParams.Mode.SUBSCRIPTION)
                .setClientReferenceId(clerkId)
                // {CHECKOUT_SESSION_ID} is a literal placeholder Stripe substitutes
                // with the real session id at redirect time (its documented
                // mechanism for this) — the frontend's post-checkout wait screen
                // needs it to poll getCheckoutResult below.
                .setSuccessUrl(successUrl + "?checkout=success&session_id={CHECKOUT_SESSION_ID}")
                .setCancelUrl(cancelUrl + "?checkout=cancelled")
                .addLineItem(
                        SessionCreateParams.LineItem.builder()
                                .setPrice(proPriceId)
                                .setQuantity(1L)
                                .build()
                )
                .setSubscriptionData(
                        SessionCreateParams.SubscriptionData.builder()
                                .putMetadata("clerkId", clerkId)
                                .build()
                );

        if (existingStripeCustomerId != null) {
            params.setCustomer(existingStripeCustomerId);
        } else {
            params.setCustomerEmail(email);
        }

        Session session = Session.create(params.build());
        return session.getUrl();
    }

    /**
     * Backs the frontend's post-checkout wait screen — reads Stripe's own
     * Checkout Session directly rather than anything in our own database,
     * since a failed activation attempt leaves no row behind at all (see
     * SubscriptionService's doc comment) for this to poll otherwise.
     *
     * "Succeeded" is checked against lumo-user-svc's own subscriptionType
     * (via the gateway), not this service's local `subscriptions` row —
     * that row gets a Stripe-confirmed ACTIVE status written to it
     * synchronously, before SubscriptionActivationService's @Async sync to
     * lumo-user-svc has even run yet, so it briefly looks like a success even
     * when the sync is about to fail and the row is about to be deleted. The
     * only thing that's actually true the whole time is whatever
     * lumo-user-svc itself currently says.
     *
     * Deliberately never reports FAILED here — session.getCreated() is the
     * Checkout Session's creation time, i.e. the moment the customer clicked
     * "Buy Pro", not the moment they landed back on this site. Any time spent
     * filling in card details or completing a 3D Secure challenge on Stripe's
     * hosted page counts against that clock before this endpoint is ever
     * polled even once, so it's not a meaningful "how long have we actually
     * been waiting" measure. Giving up is the frontend's call instead (see
     * CheckoutResultOverlay), timed from when polling itself started.
     *
     * @param sessionId    the Stripe Checkout Session id from the success
     *                     redirect's session_id query param
     * @param callerClerkId the authenticated caller — checked against the
     *                     session's own clientReferenceId so one signed-in
     *                     user can't probe another's checkout session id
     * @return empty if the session doesn't belong to callerClerkId
     */
    public Optional<CheckoutResultResponse> getCheckoutResult(String sessionId, String callerClerkId) throws StripeException {
        Session session = Session.retrieve(sessionId);

        if (!callerClerkId.equals(session.getClientReferenceId())) {
            return Optional.empty();
        }

        return Optional.of(isProUser(callerClerkId) ? CheckoutResultResponse.succeeded() : CheckoutResultResponse.pending());
    }

    private boolean isProUser(String clerkId) {
        try {
            UserSubscriptionResponse user = gatewayRestClient.get()
                    .uri("/internal/users/{clerkId}", clerkId)
                    .retrieve()
                    .body(UserSubscriptionResponse.class);
            return user != null && "PRO_USER".equals(user.getSubscriptionType());
        } catch (RestClientException e) {
            // Treated as "not Pro yet" rather than an error — a transient
            // blip here just means one more pending poll, not a false failure.
            log.error("Failed to check lumo-user-svc subscriptionType for clerkId={} while polling checkout result", clerkId, e);
            return false;
        }
    }
}
