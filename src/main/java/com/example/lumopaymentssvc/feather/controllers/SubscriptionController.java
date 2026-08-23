package com.example.lumopaymentssvc.feather.controllers;

import com.example.lumopaymentssvc.feather.dtos.SubscriptionResponse;
import com.example.lumopaymentssvc.feather.services.SubscriptionService;
import org.springframework.security.core.Authentication;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

// Protected by GatewayAuthenticationFilter — see SecurityConfig.PROTECTED_PATHS ("/payments").
@RestController
@RequestMapping("/payments")
public class SubscriptionController {

    private final SubscriptionService subscriptionService;

    public SubscriptionController(SubscriptionService subscriptionService) {
        this.subscriptionService = subscriptionService;
    }

    /**
     * A Clerk user who's never started checkout has no row in `subscriptions`
     * at all — that's the normal Free-plan state, not an error, so this
     * returns a default FREE/NONE response instead of 404.
     */
    @GetMapping("/subscription")
    public SubscriptionResponse getSubscription(Authentication authentication) {
        return subscriptionService.findByClerkId(authentication.getName())
                .map(SubscriptionResponse::from)
                .orElseGet(SubscriptionResponse::none);
    }
}
