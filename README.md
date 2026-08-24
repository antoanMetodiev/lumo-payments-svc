# lumo-payments-svc

Handles Stripe billing for LumoFlix: creates Checkout Sessions for the Pro
plan, exposes a Billing Portal link so subscribers can manage/cancel, and
keeps a local record of each Clerk user's subscription state in sync via
Stripe webhooks.

Rewritten from Java/Spring Boot to Go, endpoint-for-endpoint and
behavior-for-behavior compatible, to compare real-world RAM usage on
Render's free tier — same motivation and conventions as
[lumo-movies-svc](../lumo-movies-svc). Uses the official
[stripe-go](https://github.com/stripe/stripe-go) SDK (`stripe.Client`, the
current recommended API style as of v82.1+) rather than hand-rolling Stripe
API calls.

**This service never talks to Clerk directly.** `lumo-api-gateway` is the
only place that verifies a Clerk session JWT; it forwards the already-verified
Clerk user id (`X-Clerk-User-Id`) plus a shared secret proving the call really
came from the gateway (`X-Internal-Api-Key`) — see `internal/gatewayauth`.
If you're calling this service directly (e.g. local testing without the
gateway in front of it), you need to send both headers yourself.

## Environment variables

| Variable | Where to find it |
|---|---|
| `INTERNAL_SERVICE_SECRET` | Any long random string you generate yourself — must be the **exact same value** set on `lumo-api-gateway` (and `lumo-user-svc`). |
| `STRIPE_SECRET_KEY` | Stripe dashboard → **Developers → API keys** (`sk_test_...` / `sk_live_...`). |
| `STRIPE_WEBHOOK_SECRET` | Stripe dashboard → **Developers → Webhooks** → the endpoint's signing secret (`whsec_...`). This is the **production** value; leave it as-is once set. |
| `STRIPE_WEBHOOK_SECRET_LOCAL` | Optional, local dev only. `stripe listen` (see below) prints its own signing secret — put that one here instead of overwriting `STRIPE_WEBHOOK_SECRET`. Unset in production, so it always falls through to the real one there. |
| `STRIPE_PRO_PRICE_ID` | Stripe dashboard → **Product catalog** → the recurring Price id for the Pro plan (`price_...`). |
| `STRIPE_SUCCESS_URL` / `STRIPE_CANCEL_URL` | Where Stripe redirects after Checkout. Defaults to the local frontend's homepage if unset. |
| `DATABASE_URL` / `DATABASE_USERNAME` / `DATABASE_PASSWORD` | Its own Postgres database — same mechanism as the other services. |
| `RESEND_API_KEY` | Resend dashboard → **API Keys**. Used by `EmailService` to send the "we refunded you" email — see "Activation failures" below. |
| `RESEND_FROM_EMAIL` | Must be an address on a domain verified in the Resend account. |

## Webhooks

### Local dev

No dashboard registration or tunnel (ngrok etc.) needed for Stripe — the
Stripe CLI does both jobs at once:

1. Install it and run `stripe login` once (opens a browser to link it to
   your Stripe account — sign up free at https://dashboard.stripe.com/register
   if you don't have one yet; test mode needs no business verification).
2. With this service running locally: `stripe listen --forward-to localhost:8084/webhooks/stripe`.
3. It prints `Ready! Your webhook signing secret is whsec_...` — put that in
   `STRIPE_WEBHOOK_SECRET_LOCAL`. Leave it running while you test; it forwards
   real Stripe events (or ones you fire with `stripe trigger checkout.session.completed`)
   straight to your local service.

### Production

1. Stripe dashboard → **Developers → Webhooks → Add endpoint**.
2. Endpoint URL: the deployed service's URL, e.g. `https://lumo-payments-svc.onrender.com/webhooks/stripe`.
3. Subscribe to events: `checkout.session.completed`, `customer.subscription.updated`,
   `customer.subscription.deleted`, `invoice.payment_failed`.
4. Copy the **Signing secret** into `STRIPE_WEBHOOK_SECRET`.

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/payments/checkout-session` | Gateway (`X-Clerk-User-Id` + `X-Internal-Api-Key`) | Creates (or reuses) a Stripe Customer for the caller and returns a Checkout Session URL for the Pro plan. |
| `POST` | `/payments/portal-session` | Gateway | Returns a Stripe Billing Portal URL so the caller can manage/cancel their existing subscription. |
| `GET` | `/payments/subscription` | Gateway | Returns the caller's current plan/status as this service sees it. |
| `GET` | `/payments/checkout-result?sessionId=...` | Gateway | Polled by the frontend's post-checkout wait screen — reads the outcome of one specific Stripe Checkout Session directly from Stripe, not our DB (see "Activation failures" below). |
| `POST` | `/webhooks/stripe` | Stripe signature | Keeps the local `subscriptions` table in sync with Stripe. Called directly by Stripe, not through the gateway. |
| `DELETE` | `/internal/subscriptions/{clerkId}` | Gateway (`X-Internal-Api-Key` only) | Cancels any live Stripe subscription (no refund — they already had the access) and deletes this clerkId's row, called by lumo-user-svc right after it deletes its own copy of a Clerk account. |
| `GET` | `/heartbeat`, `GET /` | none | Render free-tier keep-alive, same as the other services. |

## Activation failures (refund + email)

Every time a webhook changes which plan a user should be on,
`internal/subscriptions.Service` tells `lumo-user-svc` (via the gateway's
`/internal` path) to flip their `subscription_type` — see
`internal/subscriptions.ActivationService`. That call is retried a few times
over a few seconds (`internal/usersync`); a user watching their account page
right after paying will assume they got scammed long before a 30-minute or
hour-long background retry would ever fire, so the window is deliberately
short.

If it still hasn't landed after those retries **and the user was supposed to
be upgraded to Pro**, this is treated as a hard failure, not something to
keep retrying quietly in the background:

1. `internal/refund` refunds the subscription's latest payment in full and
   cancels the Stripe subscription, so the customer isn't charged again for
   something they can't use. This itself retries a few times too (same short
   backoff) before giving up.
2. The local `subscriptions` row is deleted, not marked as some kind of
   "failed" status — a `subscriptions` row only ever exists for a Clerk user
   this system actually confirmed is on a real plan (see
   `internal/subscriptions.Service`'s doc comment). A refunded customer has
   no real subscription relationship left, and may never come back, so
   there's nothing to keep a record of.
3. `internal/email` (Resend) emails the customer confirming the refund.

Since nothing is persisted for a failed activation, the frontend can't learn
about it by polling `GET /payments/subscription` later — see `my-app`'s
post-checkout wait screen, which polls `GET /payments/checkout-result` (keyed
by the Stripe Checkout Session id, not this service's own DB) for the outcome
of one specific checkout attempt instead.

If the refund itself fails even after retries, or if a *revoke* (cancellation)
sync fails after retries — no money involved there, so no refund — there's no
ops-alert email for either case; it's a loud log line only. Both need a
human to reconcile Stripe/the DB by hand, found by watching the logs.

This whole flow runs off the Stripe webhook's request goroutine
(`ActivationService.SyncOrEscalate` dispatches its own detached goroutine —
the Go equivalent of the Java version's `@Async`) so a slow refund/email
round-trip can't make Stripe think webhook delivery itself failed and retry
the whole event.

## Structure

- `cmd/server` — entrypoint, wiring
- `internal/models` — wire/DB types (mirrors `feather.models.PaymentSubscription`
  and its enums)
- `internal/subscriptions` — the local `subscriptions` table repository, the
  webhook-driven sync/read service, and the activation-failure escalation
  logic (mirrors `PaymentSubscriptionRepository` +
  `SubscriptionService`/`SubscriptionActivationService` +
  `SubscriptionController`/`InternalSubscriptionController`)
- `internal/checkout` — Checkout Session creation + the post-checkout result
  poll (mirrors `CheckoutService`/`CheckoutController`)
- `internal/billingportal` — Billing Portal session creation (mirrors
  `BillingPortalService`/`BillingPortalController`)
- `internal/webhook` — Stripe webhook signature verification + event
  dispatch (mirrors `StripeWebhookService`/`StripeWebhookController`)
- `internal/refund`, `internal/email`, `internal/usersync` — supporting
  Stripe/Resend/gateway calls (mirror `RefundService`/`EmailService`/`UserSyncService`)
- `internal/gatewayauth`, `internal/gatewayclient`, `internal/dbx`,
  `internal/metrics`, `internal/reqlog`, `internal/config` — supporting
  pieces, each with a package doc comment pointing at the Java class(es) it
  replaces

## Scope note

The Java source's `PaymentSubscriptionRepository.findByStripeCustomerId`
method was declared but never called from any live endpoint — not ported.
