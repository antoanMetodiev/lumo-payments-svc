package com.example.lumopaymentssvc.feather.services;

import com.example.lumopaymentssvc.feather.models.PlanType;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestClient;
import org.springframework.web.client.RestClientException;

import java.time.Duration;
import java.util.List;
import java.util.Map;

/**
 * Tells lumo-user-svc — via lumo-api-gateway, same as everything else in
 * this system — to flip a Clerk user's subscription_type after a Stripe
 * webhook changes their plan. There's no live Clerk session for this call
 * (Stripe triggered it), so it goes through the gateway's /internal path
 * (X-Internal-Api-Key only, see InternalServiceAuthenticationFilter) instead
 * of the usual Clerk-session-based routes.
 *
 * Retries a few times over a few seconds — not the 30+ minutes a typical
 * backoff schedule would use — because a user staring at their account page
 * right after paying will conclude within seconds that they got scammed, not
 * that a background job will fix it eventually. If it still hasn't landed
 * after that, the caller (SubscriptionActivationService) treats it as a hard
 * failure and refunds instead of leaving the customer paid-but-locked-out.
 */
@Slf4j
@Service
public class UserSyncService {

    private static final int MAX_ATTEMPTS = 3;
    private static final List<Duration> RETRY_DELAYS = List.of(Duration.ofSeconds(1), Duration.ofSeconds(2));

    private final RestClient gatewayRestClient;

    public UserSyncService(RestClient gatewayRestClient) {
        this.gatewayRestClient = gatewayRestClient;
    }

    /**
     * @return true once lumo-user-svc confirms the update, false if all
     * {@value #MAX_ATTEMPTS} attempts failed.
     */
    public boolean syncSubscriptionType(String clerkId, PlanType plan) {
        String subscriptionType = plan == PlanType.PRO ? "PRO_USER" : "REGULAR_USER";

        for (int attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
            try {
                // POST, not PATCH — the gateway's own Feign client (used for the next
                // hop, to lumo-user-svc) can't send PATCH at all, so this endpoint is
                // POST end-to-end for consistency.
                gatewayRestClient.post()
                        .uri("/internal/users/{clerkId}/subscription", clerkId)
                        .contentType(MediaType.APPLICATION_JSON)
                        .body(Map.of("subscriptionType", subscriptionType))
                        .retrieve()
                        .toBodilessEntity();
                return true;
            } catch (RestClientException e) {
                log.error("Attempt {}/{} failed to sync subscriptionType={} to lumo-user-svc for clerkId={}",
                        attempt, MAX_ATTEMPTS, subscriptionType, clerkId, e);
                if (attempt < MAX_ATTEMPTS) {
                    sleep(RETRY_DELAYS.get(attempt - 1));
                }
            }
        }

        return false;
    }

    private void sleep(Duration duration) {
        try {
            Thread.sleep(duration.toMillis());
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
