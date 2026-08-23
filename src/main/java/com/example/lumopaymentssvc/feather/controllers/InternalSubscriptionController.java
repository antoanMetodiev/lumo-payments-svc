package com.example.lumopaymentssvc.feather.controllers;

import com.example.lumopaymentssvc.feather.services.SubscriptionService;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * Service-to-service only — protected by GatewayAuthenticationFilter's
 * internal-only path list (X-Internal-Api-Key, no X-Clerk-User-Id), not the
 * usual Clerk-session-based /payments routes. Called by lumo-user-svc (via
 * the gateway) right after it deletes its own copy of a Clerk account, so
 * this service doesn't keep an orphaned row around for someone who no longer
 * has an account at all.
 */
@RestController
@RequestMapping("/internal/subscriptions")
public class InternalSubscriptionController {

    private final SubscriptionService subscriptionService;

    public InternalSubscriptionController(SubscriptionService subscriptionService) {
        this.subscriptionService = subscriptionService;
    }

    @DeleteMapping("/{clerkId}")
    public ResponseEntity<Void> deleteSubscription(@PathVariable String clerkId) {
        subscriptionService.deleteByClerkId(clerkId);
        return ResponseEntity.noContent().build();
    }
}
