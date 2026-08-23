package com.example.lumopaymentssvc.exception;

import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.ResponseStatus;

/**
 * Thrown when a Clerk user asks for a Billing Portal link before ever
 * starting checkout — there's no Stripe Customer to point the portal at yet.
 */
@ResponseStatus(HttpStatus.NOT_FOUND)
public class NoStripeCustomerException extends RuntimeException {
    public NoStripeCustomerException(String clerkId) {
        super("No Stripe customer on file for clerkId=" + clerkId);
    }
}
