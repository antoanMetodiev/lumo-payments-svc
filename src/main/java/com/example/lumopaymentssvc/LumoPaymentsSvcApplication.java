package com.example.lumopaymentssvc;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.scheduling.annotation.EnableAsync;

// @EnableAsync backs SubscriptionActivationService's @Async sync/refund/email
// flow, so it runs off the Stripe webhook request thread instead of risking
// a slow refund call making Stripe think the whole webhook delivery timed out.
@EnableAsync
@SpringBootApplication
public class LumoPaymentsSvcApplication {
    public static void main(String[] args) {
        SpringApplication.run(LumoPaymentsSvcApplication.class, args);
    }
}
