package com.example.lumopaymentssvc.feather.services;

import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestClient;
import org.springframework.web.client.RestClientException;

import java.util.List;
import java.util.Map;

/**
 * Transactional email via Resend's HTTP API (see
 * https://resend.com/docs/api-reference/emails/send-email) — a plain
 * RestClient call rather than their SDK, since this service only ever sends
 * one kind of email and doesn't need the extra dependency.
 *
 * Only audience: the customer, when SubscriptionActivationService has to
 * refund a payment it couldn't turn into working Pro access. Failures in
 * that same flow (refund itself failing, revoke sync failing) are only
 * logged, not emailed anywhere — there's no ops inbox wired up for this.
 */
@Slf4j
@Service
public class EmailService {

    private final RestClient resendClient;
    private final String fromAddress;

    public EmailService(
            @Value("${resend.api-key}") String apiKey,
            @Value("${resend.from-address}") String fromAddress
    ) {
        this.resendClient = RestClient.builder()
                .baseUrl("https://api.resend.com")
                .defaultHeader("Authorization", "Bearer " + apiKey)
                .build();
        this.fromAddress = fromAddress;
    }

    public void sendActivationFailedEmail(String toEmail) {
        if (toEmail == null || toEmail.isBlank()) {
            log.error("Cannot send activation-failed email — no recipient address, needs manual follow-up");
            return;
        }

        send(toEmail, "Проблем с активирането на твоя LumoFlix Pro абонамент", activationFailedHtml());
    }

    private void send(String to, String subject, String html) {
        try {
            resendClient.post()
                    .uri("/emails")
                    .contentType(MediaType.APPLICATION_JSON)
                    .body(Map.of(
                            "from", fromAddress,
                            "to", List.of(to),
                            "subject", subject,
                            "html", html
                    ))
                    .retrieve()
                    .toBodilessEntity();
        } catch (RestClientException e) {
            log.error("Failed to send email via Resend to {}", to, e);
        }
    }

    private String activationFailedHtml() {
        return """
                <p>Здравей,</p>
                <p>Опитахме да активираме твоя LumoFlix Pro абонамент, но възникна технически проблем от наша страна.</p>
                <p><strong>Възстановихме пълната платена сума обратно към картата ти</strong> — плащането не беше \
                довършено успешно, така че няма да видиш повторно начисляване.</p>
                <p>Възстановяването може да отнеме няколко минути, докато се появи обратно по картата ти — \
                това зависи от банката ти, не от нас.</p>
                <p>Съжаляваме за неудобството — може да опиташ отново по всяко време.</p>
                <p>Екипът на LumoFlix</p>
                """;
    }
}
