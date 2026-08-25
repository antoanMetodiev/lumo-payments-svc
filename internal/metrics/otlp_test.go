package metrics

import (
	"context"
	"testing"
)

// TestStart_NoSchemaURLConflict guards against the exact bug hit on Render:
// resource.Merge(resource.Default(), ...) failing with "conflicting Schema
// URL" because our semconv import didn't match the SDK's own internal
// semconv version. The resource merge happens synchronously in Start(),
// before any real network call, so a fake endpoint is enough to catch it.
func TestStart_NoSchemaURLConflict(t *testing.T) {
	shutdown, err := Start(context.Background(), true, "http://127.0.0.1:0/v1/metrics", "Basic fake")
	if err != nil {
		t.Fatalf("Start() returned an error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Start() returned a nil shutdown func")
	}
	// shutdown() flushes a final export to the fake, unreachable endpoint
	// above — that network failure is expected here and irrelevant to what
	// this test actually guards against (the schema conflict, which would
	// have failed synchronously in Start() itself, above).
	_ = shutdown(context.Background())
}

func TestStart_DisabledIsNoop(t *testing.T) {
	shutdown, err := Start(context.Background(), false, "", "")
	if err != nil {
		t.Fatalf("Start() returned an error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() returned an error: %v", err)
	}
}
