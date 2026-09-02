// Command server is the Go rewrite of lumo-payments-svc (originally Spring
// Boot). Endpoint paths, params, status codes, and stored data all mirror
// the Java service exactly — see internal/subscriptions, internal/checkout,
// internal/billingportal, and internal/webhook for the per-package notes on
// where behavior was intentionally preserved vs. where a Java-only
// implementation detail (Spring's @Async/@Transactional, a Bean Validation
// constraint) was replaced with a plain Go equivalent.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stripe/stripe-go/v86"

	"lumo-payments-svc/internal/billingportal"
	"lumo-payments-svc/internal/checkout"
	"lumo-payments-svc/internal/config"
	"lumo-payments-svc/internal/dbx"
	"lumo-payments-svc/internal/email"
	"lumo-payments-svc/internal/gatewayauth"
	"lumo-payments-svc/internal/gatewayclient"
	"lumo-payments-svc/internal/metrics"
	"lumo-payments-svc/internal/refund"
	"lumo-payments-svc/internal/reqlog"
	"lumo-payments-svc/internal/subscriptions"
	"lumo-payments-svc/internal/usersync"
	"lumo-payments-svc/internal/webhook"
)

// Same split as SecurityConfig.USER_SCOPED_PATHS / INTERNAL_PATHS.
var (
	userScopedPaths = []string{"/payments"}
	internalPaths   = []string{"/internal"}
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := dbx.Dial(ctx, cfg.DatabaseURL, cfg.DBUsername, cfg.DBPassword)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	dbx.StartKeepAlivePing(ctx, pool, log.Printf)

	shutdownMetrics, err := metrics.Start(ctx, cfg.GrafanaEnabled, cfg.GrafanaEndpoint, cfg.GrafanaAuth)
	if err != nil {
		log.Printf("metrics exporter did not start: %v", err)
		shutdownMetrics = func(context.Context) error { return nil }
	}
	defer shutdownMetrics(context.Background())

	sc := stripe.NewClient(cfg.StripeSecretKey)
	gatewayClient := gatewayclient.New(cfg.GatewayURL, cfg.InternalSecret)

	subsRepo := subscriptions.NewRepository(pool)
	refundSvc := refund.New(sc)
	emailSvc := email.New(cfg.ResendAPIKey, cfg.ResendFromAddress)
	userSyncSvc := usersync.New(gatewayClient)
	activationSvc := subscriptions.NewActivationService(userSyncSvc, refundSvc, emailSvc, subsRepo, sc)
	subsSvc := subscriptions.NewService(subsRepo, activationSvc, refundSvc)
	subsHandlers := subscriptions.NewHandlers(subsSvc)

	checkoutSvc := checkout.New(subsRepo, gatewayClient, sc, cfg.StripeProPriceID, cfg.StripeSuccessURL, cfg.StripeCancelURL)
	checkoutHandlers := checkout.NewHandlers(checkoutSvc)

	portalSvc := billingportal.New(subsRepo, sc, cfg.StripeSuccessURL)
	portalHandlers := billingportal.NewHandlers(portalSvc)

	webhookSvc := webhook.NewService(subsSvc)
	webhookHandlers := webhook.NewHandlers(cfg.StripeWebhookSecret, webhookSvc)

	mux := http.NewServeMux()
	subsHandlers.Register(mux)
	checkoutHandlers.Register(mux)
	portalHandlers.Register(mux)
	webhookHandlers.Register(mux)

	// Heartbeat — "{$}" matches only the exact "/" path (Go's ServeMux would
	// otherwise treat a bare "/" pattern as a catch-all for every unmatched
	// path, unlike Java's exact-match @GetMapping("/")).
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /heartbeat", func(w http.ResponseWriter, r *http.Request) {})
	// db-keepalive is separate from StartKeepAlivePing's twice-daily
	// in-process ticker above: that ticker only fires while this instance is
	// already running, so it can't stop the DB from idling out while Render
	// itself has the service asleep. This is a public, unauthenticated route
	// (outside userScopedPaths/internalPaths) meant to be hit by an external
	// cron so the request itself both wakes the instance and touches the DB.
	mux.HandleFunc("GET /db-keepalive", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		var one int
		if err := pool.QueryRow(pingCtx, "SELECT 1").Scan(&one); err != nil {
			log.Printf("db-keepalive: query failed: %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	// management.endpoints.web.exposure.include=health
	mux.HandleFunc("GET /actuator/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"UP"}`))
	})

	handler := reqlog.Middleware(withCORS(gatewayauth.Middleware(cfg.InternalSecret, userScopedPaths, internalPaths)(mux)))

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("lumo-payments-svc (Go) listening on :%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

// withCORS mirrors SecurityConfig.corsConfigurationSource — nothing calls
// this service directly from a browser anymore (the Next.js app's server
// talks to lumo-api-gateway, which talks to this service, server-to-server,
// no Origin header involved). Kept for local dev / testing this service
// directly without the gateway.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == "http://localhost:3000" {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
