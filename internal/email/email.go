// Package email mirrors feather.services.EmailService — transactional email
// via Resend's HTTP API, a plain http.Client call rather than their SDK,
// since this service only ever sends one kind of email.
//
// Only audience: the customer, when SubscriptionActivationService has to
// refund a payment it couldn't turn into working Pro access. Failures in
// that same flow (refund itself failing, revoke sync failing) are only
// logged, not emailed anywhere — there's no ops inbox wired up for this.
package email

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

type Service struct {
	http        *http.Client
	apiKey      string
	fromAddress string
}

func New(apiKey, fromAddress string) *Service {
	return &Service{
		http:        &http.Client{Timeout: 10 * time.Second},
		apiKey:      apiKey,
		fromAddress: fromAddress,
	}
}

func (s *Service) SendActivationFailedEmail(toEmail string) {
	if strings.TrimSpace(toEmail) == "" {
		log.Printf("email: cannot send activation-failed email — no recipient address, needs manual follow-up")
		return
	}

	s.send(toEmail, "Проблем с активирането на твоя LumoFlix Pro абонамент", activationFailedHTML)
}

func (s *Service) send(to, subject, html string) {
	body, err := json.Marshal(map[string]any{
		"from":    s.fromAddress,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	})
	if err != nil {
		log.Printf("email: failed to encode Resend request for %s: %v", to, err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		log.Printf("email: failed to build Resend request for %s: %v", to, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.http.Do(req)
	if err != nil {
		log.Printf("email: failed to send email via Resend to %s: %v", to, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("email: Resend returned status %d sending to %s", resp.StatusCode, to)
	}
}

const activationFailedHTML = `<p>Здравей,</p>
<p>Опитахме да активираме твоя LumoFlix Pro абонамент, но възникна технически проблем от наша страна.</p>
<p><strong>Възстановихме пълната платена сума обратно към картата ти</strong> — плащането не беше довършено успешно, така че няма да видиш повторно начисляване.</p>
<p>Възстановяването може да отнеме няколко минути, докато се появи обратно по картата ти — това зависи от банката ти, не от нас.</p>
<p>Съжаляваме за неудобството — може да опиташ отново по всяко време.</p>
<p>Екипът на LumoFlix</p>
`
