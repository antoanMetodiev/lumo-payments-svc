// Package config reads the exact same environment variables the Java service
// used (application.properties), so this drop-in replacement needs zero
// changes to whatever's already set on Render.
package config

import "os"

type Config struct {
	DatabaseURL     string
	DBUsername      string
	DBPassword      string
	Port            string
	InternalSecret  string // internal.service-secret — shared with lumo-api-gateway
	GrafanaEnabled  bool
	GrafanaEndpoint string
	GrafanaAuth     string

	// Where lumo-api-gateway lives — used by usersync to push subscription_type
	// changes back to lumo-user-svc, and by checkout to poll it. Java hardcoded
	// this in application.properties rather than reading an env var; this Go
	// rewrite exposes an override the same way lumo-api-gateway's own
	// downstream URLs do, defaulting to the exact same production value.
	GatewayURL string

	StripeSecretKey     string
	StripeWebhookSecret string
	StripeProPriceID    string
	StripeSuccessURL    string
	StripeCancelURL     string

	ResendAPIKey      string
	ResendFromAddress string
}

func Load() Config {
	return Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		DBUsername:      os.Getenv("DATABASE_USERNAME"),
		DBPassword:      os.Getenv("DATABASE_PASSWORD"),
		Port:            envOr("PORT", "8084"), // Java's server.port=8084
		InternalSecret:  os.Getenv("INTERNAL_SERVICE_SECRET"),
		GrafanaEnabled:  os.Getenv("GRAFANA_OTLP_ENABLED") == "true",
		GrafanaEndpoint: os.Getenv("GRAFANA_OTLP_ENDPOINT"),
		GrafanaAuth:     os.Getenv("GRAFANA_OTLP_AUTH"),

		GatewayURL: envOr("GATEWAY_URL", "https://lumo-api-gateway.onrender.com"),

		StripeSecretKey: os.Getenv("STRIPE_SECRET_KEY"),
		// STRIPE_WEBHOOK_SECRET_LOCAL is optional — set only in local dev (the
		// Stripe CLI's `stripe listen` prints its own signing secret) so the
		// real STRIPE_WEBHOOK_SECRET never has to be touched. Falls through to
		// the real one whenever unset (i.e. always, in production).
		StripeWebhookSecret: firstNonEmpty(os.Getenv("STRIPE_WEBHOOK_SECRET_LOCAL"), os.Getenv("STRIPE_WEBHOOK_SECRET")),
		StripeProPriceID:    os.Getenv("STRIPE_PRO_PRICE_ID"),
		StripeSuccessURL:    envOr("STRIPE_SUCCESS_URL", "https://auroraflix.cloud/"),
		StripeCancelURL:     envOr("STRIPE_CANCEL_URL", "https://auroraflix.cloud/"),

		ResendAPIKey:      os.Getenv("RESEND_API_KEY"),
		ResendFromAddress: os.Getenv("RESEND_FROM_EMAIL"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
