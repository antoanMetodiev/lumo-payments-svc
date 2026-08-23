package com.example.lumopaymentssvc.config;

import com.stripe.Stripe;
import jakarta.annotation.PostConstruct;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

/**
 * The Stripe SDK reads its API key off a static field rather than through
 * dependency injection, so this just sets it once at startup instead of
 * every caller having to remember to do it.
 */
@Component
public class StripeConfig {

    private final String secretKey;

    public StripeConfig(@Value("${stripe.secret-key}") String secretKey) {
        this.secretKey = secretKey;
    }

    @PostConstruct
    public void init() {
        Stripe.apiKey = secretKey;
    }
}
