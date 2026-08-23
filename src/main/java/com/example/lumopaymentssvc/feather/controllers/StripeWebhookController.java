package com.example.lumopaymentssvc.feather.controllers;

import com.example.lumopaymentssvc.feather.services.StripeWebhookService;
import com.stripe.exception.SignatureVerificationException;
import com.stripe.model.Event;
import com.stripe.net.Webhook;
import jakarta.servlet.http.HttpServletRequest;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.io.IOException;
import java.nio.charset.StandardCharsets;

/**
 * Receives Stripe's checkout/subscription/invoice events. Signature
 * verification uses the raw request body — never a re-serialized one, since
 * that would change the bytes being signed (same constraint as
 * ClerkWebhookController in lumo-user-svc).
 */
@Slf4j
@RestController
@RequestMapping("/webhooks")
public class StripeWebhookController {

    private final String webhookSecret;
    private final StripeWebhookService stripeWebhookService;

    public StripeWebhookController(
            @Value("${stripe.webhook-secret}") String webhookSecret,
            StripeWebhookService stripeWebhookService
    ) {
        this.webhookSecret = webhookSecret;
        this.stripeWebhookService = stripeWebhookService;
    }

    @PostMapping("/stripe")
    public ResponseEntity<Void> handleStripeWebhook(
            HttpServletRequest request,
            @RequestHeader("Stripe-Signature") String signature
    ) throws IOException {
        String payload = new String(request.getInputStream().readAllBytes(), StandardCharsets.UTF_8);

        Event event;
        try {
            event = Webhook.constructEvent(payload, signature, webhookSecret);
        } catch (SignatureVerificationException e) {
            log.warn("Rejected Stripe webhook with invalid signature");
            return ResponseEntity.status(HttpStatus.UNAUTHORIZED).build();
        }

        stripeWebhookService.handle(event);
        return ResponseEntity.ok().build();
    }
}
