package dbx

import (
	"strings"
	"testing"
)

func TestBuildDSN_TranslatesJDBCSSLParam(t *testing.T) {
	dsn, err := buildDSN("jdbc:postgresql://stream-mate-movies-stream-mate-1.e.aivencloud.com:13766/defaultdb?ssl=true", "avnadmin", "secret")
	if err != nil {
		t.Fatalf("buildDSN error: %v", err)
	}

	if strings.Contains(dsn, "ssl=true") {
		t.Errorf("expected the raw JDBC ssl=true param to be gone, got: %s", dsn)
	}
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("expected sslmode=require, got: %s", dsn)
	}
	if !strings.HasPrefix(dsn, "postgresql://avnadmin:secret@") {
		t.Errorf("expected credentials injected from the separate username/password, got: %s", dsn)
	}
	if strings.Contains(dsn, "jdbc:") {
		t.Errorf("expected the jdbc: prefix to be stripped, got: %s", dsn)
	}
}

func TestBuildDSN_DefaultsToRequireWithNoSSLParamAtAll(t *testing.T) {
	dsn, err := buildDSN("postgresql://host:5432/db", "user", "pass")
	if err != nil {
		t.Fatalf("buildDSN error: %v", err)
	}
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("expected sslmode=require to be added by default, got: %s", dsn)
	}
}

func TestBuildDSN_RespectsExplicitSslmode(t *testing.T) {
	dsn, err := buildDSN("postgresql://host:5432/db?sslmode=verify-full", "user", "pass")
	if err != nil {
		t.Fatalf("buildDSN error: %v", err)
	}
	if !strings.Contains(dsn, "sslmode=verify-full") {
		t.Errorf("expected the explicit sslmode to be preserved, got: %s", dsn)
	}
}
