package com.example.lumopaymentssvc.feather.controllers;

import com.example.lumopaymentssvc.feather.dtos.CheckoutResultResponse;
import com.example.lumopaymentssvc.feather.dtos.CheckoutSessionResponse;
import com.example.lumopaymentssvc.feather.dtos.CreateCheckoutSessionRequest;
import com.example.lumopaymentssvc.feather.services.CheckoutService;
import com.stripe.exception.StripeException;
import jakarta.validation.Valid;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.security.core.Authentication;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.util.Optional;

// Protected by GatewayAuthenticationFilter — see SecurityConfig.PROTECTED_PATHS ("/payments").
@Slf4j
@RestController
@RequestMapping("/payments")
public class CheckoutController {

    private final CheckoutService checkoutService;

    public CheckoutController(CheckoutService checkoutService) {
        this.checkoutService = checkoutService;
    }

    /**
     * authentication.getName() is the verified Clerk user id, set by
     * GatewayAuthenticationFilter. Returns the Checkout Session URL for the
     * frontend to redirect the browser to — nothing gets charged and no local
     * subscription status changes until Stripe calls back via the webhook.
     */
    @PostMapping("/checkout-session")
    public ResponseEntity<CheckoutSessionResponse> createCheckoutSession(
            Authentication authentication,
            @RequestBody @Valid CreateCheckoutSessionRequest request
    ) {
        try {
            String url = checkoutService.createCheckoutSessionUrl(authentication.getName(), request.getEmail());
            return ResponseEntity.ok(new CheckoutSessionResponse(url));
        } catch (StripeException e) {
            log.error("Failed to create Stripe checkout session for clerkId={}", authentication.getName(), e);
            return ResponseEntity.status(HttpStatus.BAD_GATEWAY).build();
        }
    }

    /**
     * Backs the frontend's post-checkout wait screen (polled every couple of
     * seconds right after the Stripe redirect) — see CheckoutService's own
     * doc comment for why this reads Stripe directly instead of our DB.
     * 404 if sessionId doesn't belong to the caller (someone else's session,
     * or a garbled/tampered query param) rather than leaking whether it exists.
     */
    @GetMapping("/checkout-result")
    public ResponseEntity<CheckoutResultResponse> getCheckoutResult(
            Authentication authentication,
            @RequestParam String sessionId
    ) {
        try {
            Optional<CheckoutResultResponse> result = checkoutService.getCheckoutResult(sessionId, authentication.getName());
            return result.map(ResponseEntity::ok).orElseGet(() -> ResponseEntity.notFound().build());
        } catch (StripeException e) {
            log.error("Failed to look up Stripe checkout session {} for clerkId={}", sessionId, authentication.getName(), e);
            return ResponseEntity.status(HttpStatus.BAD_GATEWAY).build();
        }
    }
}
