package config

import "testing"

// --- envOr ---

func TestEnvOr_UsesEnvValueWhenSet(t *testing.T) {
	t.Setenv("CONFIG_TEST_KEY", "from-env")
	if got := envOr("CONFIG_TEST_KEY", "fallback"); got != "from-env" {
		t.Errorf("envOr() = %q, want %q", got, "from-env")
	}
}

func TestEnvOr_UsesFallbackWhenUnset(t *testing.T) {
	if got := envOr("CONFIG_TEST_KEY_NEVER_SET", "fallback"); got != "fallback" {
		t.Errorf("envOr() = %q, want %q", got, "fallback")
	}
}

func TestEnvOr_EmptyEnvValueUsesFallback(t *testing.T) {
	t.Setenv("CONFIG_TEST_KEY", "")
	if got := envOr("CONFIG_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("envOr() = %q, want fallback %q for an empty env value", got, "fallback")
	}
}

// --- firstNonEmpty ---

func TestFirstNonEmpty_ReturnsFirstNonEmptyValue(t *testing.T) {
	if got := firstNonEmpty("", "", "b", "c"); got != "b" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "b")
	}
}

func TestFirstNonEmpty_FirstArgWins(t *testing.T) {
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "a")
	}
}

func TestFirstNonEmpty_AllEmpty_ReturnsEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", ""); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty string", got)
	}
}

func TestFirstNonEmpty_NoArgs_ReturnsEmpty(t *testing.T) {
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty string", got)
	}
}

// --- Load ---

func TestLoad_ReadsAllConfiguredEnvVars(t *testing.T) {
	t.Setenv("DATABASE_URL", "jdbc:postgresql://host/db")
	t.Setenv("DATABASE_USERNAME", "svc_user")
	t.Setenv("DATABASE_PASSWORD", "svc_pass")
	t.Setenv("PORT", "9094")
	t.Setenv("INTERNAL_SERVICE_SECRET", "internal-secret")
	t.Setenv("GRAFANA_OTLP_ENABLED", "true")
	t.Setenv("GRAFANA_OTLP_ENDPOINT", "https://otlp.example.com")
	t.Setenv("GRAFANA_OTLP_AUTH", "Basic abc123")
	t.Setenv("GATEWAY_URL", "https://gateway.example.com")
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_WEBHOOK_SECRET_LOCAL", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_prod")
	t.Setenv("STRIPE_PRO_PRICE_ID", "price_123")
	t.Setenv("STRIPE_SUCCESS_URL", "https://app.example.com/success")
	t.Setenv("STRIPE_CANCEL_URL", "https://app.example.com/cancel")
	t.Setenv("RESEND_API_KEY", "re_123")
	t.Setenv("RESEND_FROM_EMAIL", "noreply@example.com")

	cfg := Load()

	want := Config{
		DatabaseURL:         "jdbc:postgresql://host/db",
		DBUsername:          "svc_user",
		DBPassword:          "svc_pass",
		Port:                "9094",
		InternalSecret:      "internal-secret",
		GrafanaEnabled:      true,
		GrafanaEndpoint:     "https://otlp.example.com",
		GrafanaAuth:         "Basic abc123",
		GatewayURL:          "https://gateway.example.com",
		StripeSecretKey:     "sk_test_123",
		StripeWebhookSecret: "whsec_prod",
		StripeProPriceID:    "price_123",
		StripeSuccessURL:    "https://app.example.com/success",
		StripeCancelURL:     "https://app.example.com/cancel",
		ResendAPIKey:        "re_123",
		ResendFromAddress:   "noreply@example.com",
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoad_DefaultsWhenUnset(t *testing.T) {
	for _, key := range []string{
		"DATABASE_URL", "DATABASE_USERNAME", "DATABASE_PASSWORD", "PORT",
		"INTERNAL_SERVICE_SECRET", "GRAFANA_OTLP_ENABLED", "GRAFANA_OTLP_ENDPOINT",
		"GRAFANA_OTLP_AUTH", "GATEWAY_URL", "STRIPE_SECRET_KEY",
		"STRIPE_WEBHOOK_SECRET_LOCAL", "STRIPE_WEBHOOK_SECRET", "STRIPE_PRO_PRICE_ID",
		"STRIPE_SUCCESS_URL", "STRIPE_CANCEL_URL", "RESEND_API_KEY", "RESEND_FROM_EMAIL",
	} {
		t.Setenv(key, "")
	}

	cfg := Load()

	if cfg.Port != "8084" {
		t.Errorf("Port = %q, want default %q (Java's server.port=8084)", cfg.Port, "8084")
	}
	if cfg.GatewayURL != "https://lumo-api-gateway.onrender.com" {
		t.Errorf("GatewayURL = %q, want the default production gateway URL", cfg.GatewayURL)
	}
	if cfg.StripeSuccessURL != "https://auroraflix.cloud/" {
		t.Errorf("StripeSuccessURL = %q, want default", cfg.StripeSuccessURL)
	}
	if cfg.StripeCancelURL != "https://auroraflix.cloud/" {
		t.Errorf("StripeCancelURL = %q, want default", cfg.StripeCancelURL)
	}
	if cfg.GrafanaEnabled {
		t.Error("GrafanaEnabled = true, want false when unset")
	}
}

// TestLoad_StripeWebhookSecret_PrefersLocalOverride documents the dev
// convenience: STRIPE_WEBHOOK_SECRET_LOCAL (the Stripe CLI's own signing
// secret from `stripe listen`) wins whenever it's set, so the real
// production secret never has to be touched locally.
func TestLoad_StripeWebhookSecret_PrefersLocalOverride(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET_LOCAL", "whsec_local_dev")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_production")

	if got := Load().StripeWebhookSecret; got != "whsec_local_dev" {
		t.Errorf("StripeWebhookSecret = %q, want the local override %q", got, "whsec_local_dev")
	}
}

func TestLoad_StripeWebhookSecret_FallsBackToProductionWhenLocalUnset(t *testing.T) {
	t.Setenv("STRIPE_WEBHOOK_SECRET_LOCAL", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_production")

	if got := Load().StripeWebhookSecret; got != "whsec_production" {
		t.Errorf("StripeWebhookSecret = %q, want the production secret %q", got, "whsec_production")
	}
}

// TestLoad_GrafanaEnabled_OnlyExactStringTrue mirrors the `== "true"` check
// -- "1", "TRUE", "yes" etc. must NOT enable it.
func TestLoad_GrafanaEnabled_OnlyExactStringTrue(t *testing.T) {
	cases := []struct {
		envVal string
		want   bool
	}{
		{"true", true},
		{"TRUE", false},
		{"1", false},
		{"false", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run("value="+tc.envVal, func(t *testing.T) {
			t.Setenv("GRAFANA_OTLP_ENABLED", tc.envVal)
			if got := Load().GrafanaEnabled; got != tc.want {
				t.Errorf("GrafanaEnabled = %v for GRAFANA_OTLP_ENABLED=%q, want %v", got, tc.envVal, tc.want)
			}
		})
	}
}
