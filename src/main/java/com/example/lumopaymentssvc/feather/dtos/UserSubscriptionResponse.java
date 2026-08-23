package com.example.lumopaymentssvc.feather.dtos;

import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * Only the one field this service actually needs from lumo-api-gateway's
 * GET /internal/users/{clerkId} (which itself mirrors lumo-user-svc's User) —
 * Jackson ignores the rest of that response's fields (email, displayName, ...)
 * by default, so no need to mirror them here too.
 */
@Data
@NoArgsConstructor
public class UserSubscriptionResponse {
    private String subscriptionType;
}
