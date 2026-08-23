package com.example.lumopaymentssvc.feather.services;

import com.example.lumopaymentssvc.exception.NoStripeCustomerException;
import com.example.lumopaymentssvc.feather.models.PaymentSubscription;
import com.example.lumopaymentssvc.feather.repositories.PaymentSubscriptionRepository;
import com.stripe.exception.StripeException;
import com.stripe.model.billingportal.Session;
import com.stripe.param.billingportal.SessionCreateParams;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

@Service
public class BillingPortalService {

    private final PaymentSubscriptionRepository paymentSubscriptionRepository;
    private final String returnUrl;

    public BillingPortalService(
            PaymentSubscriptionRepository paymentSubscriptionRepository,
            @Value("${stripe.success-url}") String returnUrl
    ) {
        this.paymentSubscriptionRepository = paymentSubscriptionRepository;
        this.returnUrl = returnUrl;
    }

    /**
     * Lets an existing subscriber manage/cancel their plan through Stripe's
     * own hosted UI instead of this service having to build that itself.
     */
    public String createPortalSessionUrl(String clerkId) throws StripeException {
        PaymentSubscription subscription = paymentSubscriptionRepository.findByClerkId(clerkId)
                .filter(s -> s.getStripeCustomerId() != null)
                .orElseThrow(() -> new NoStripeCustomerException(clerkId));

        SessionCreateParams params = SessionCreateParams.builder()
                .setCustomer(subscription.getStripeCustomerId())
                .setReturnUrl(returnUrl)
                .build();

        Session portalSession = Session.create(params);
        return portalSession.getUrl();
    }
}
