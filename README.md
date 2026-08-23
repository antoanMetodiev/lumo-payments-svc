# lumo-payments-svc

Handles Stripe billing for LumoFlix: creates Checkout Sessions for the Pro
plan, exposes a Billing Portal link so subscribers can manage/cancel, and
keeps a local record of each Clerk user's subscription state in sync via
Stripe webhooks.

**This service never talks to Clerk directly.** `lumo-api-gateway` is the
only place that verifies a Clerk session JWT; it forwards the already-verified
Clerk user id (`X-Clerk-User-Id`) plus a shared secret proving the call really
came from the gateway (`X-Internal-Api-Key`) — see `GatewayAuthenticationFilter`.
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

Every time a webhook changes which plan a user should be on, `SubscriptionService`
tells `lumo-user-svc` (via the gateway's `/internal` path) to flip their
`subscription_type` — see `SubscriptionActivationService`. That call is
retried a few times over a few seconds (`UserSyncService`); a user watching
their account page right after paying will assume they got scammed long
before a 30-minute or hour-long background retry would ever fire, so the
window is deliberately short.

If it still hasn't landed after those retries **and the user was supposed to
be upgraded to Pro**, this is treated as a hard failure, not something to
keep retrying quietly in the background:

1. `RefundService` refunds the subscription's latest payment in full and
   cancels the Stripe subscription, so the customer isn't charged again for
   something they can't use. This itself retries a few times too (same short
   backoff) before giving up.
2. The local `subscriptions` row is deleted, not marked as some kind of
   "failed" status — a `subscriptions` row only ever exists for a Clerk user
   this system actually confirmed is on a real plan (see `SubscriptionService`'s
   doc comment). A refunded customer has no real subscription relationship
   left, and may never come back, so there's nothing to keep a record of.
3. `EmailService` (Resend) emails the customer confirming the refund.

Since nothing is persisted for a failed activation, the frontend can't learn
about it by polling `GET /payments/subscription` later — see `my-app`'s
post-checkout wait screen, which polls `GET /payments/checkout-result` (keyed
by the Stripe Checkout Session id, not this service's own DB) for the outcome
of one specific checkout attempt instead.

If the refund itself fails even after retries, or if a *revoke* (cancellation)
sync fails after retries — no money involved there, so no refund — there's no
ops-alert email for either case; it's a loud `log.error` only. Both need a
human to reconcile Stripe/the DB by hand, found by watching the logs.

This whole flow runs off the Stripe webhook's request thread (`@Async`, see
`LumoPaymentsSvcApplication`) so a slow refund/email round-trip can't make
Stripe think webhook delivery itself failed and retry the whole event.
