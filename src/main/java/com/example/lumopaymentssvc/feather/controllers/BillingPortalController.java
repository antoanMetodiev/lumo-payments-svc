package com.example.lumopaymentssvc.feather.controllers;

import com.example.lumopaymentssvc.feather.dtos.PortalSessionResponse;
import com.example.lumopaymentssvc.feather.services.BillingPortalService;
import com.stripe.exception.StripeException;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.Authentication;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

// Protected by GatewayAuthenticationFilter — see SecurityConfig.PROTECTED_PATHS ("/payments").
// NoStripeCustomerException (thrown when the caller never started checkout)
// is mapped to 404 via its own @ResponseStatus.
@Slf4j
@RestController
@RequestMapping("/payments")
public class BillingPortalController {

    private final BillingPortalService billingPortalService;

    public BillingPortalController(BillingPortalService billingPortalService) {
        this.billingPortalService = billingPortalService;
    }

    @PostMapping("/portal-session")
    public ResponseEntity<PortalSessionResponse> createPortalSession(Authentication authentication) {
        try {
            String url = billingPortalService.createPortalSessionUrl(authentication.getName());
            return ResponseEntity.ok(new PortalSessionResponse(url));
        } catch (StripeException e) {
            log.error("Failed to create Stripe billing portal session for clerkId={}", authentication.getName(), e);
            return ResponseEntity.status(HttpStatus.BAD_GATEWAY).build();
        }
    }
}
