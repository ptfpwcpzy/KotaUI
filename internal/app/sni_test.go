package app

import (
	"crypto/tls"
	"strings"
	"testing"
)

func TestStrictRealityTLSConfig(t *testing.T) {
	config := strictRealityTLSConfig("www.example.com")
	if config.ServerName != "www.example.com" || config.MinVersion != tls.VersionTLS13 || config.MaxVersion != tls.VersionTLS13 {
		t.Fatalf("unexpected TLS version configuration: %#v", config)
	}
	if len(config.CurvePreferences) != 1 || config.CurvePreferences[0] != tls.X25519 {
		t.Fatalf("unexpected curve preferences: %#v", config.CurvePreferences)
	}
	if len(config.NextProtos) != 1 || config.NextProtos[0] != "h2" {
		t.Fatalf("unexpected ALPN protocols: %#v", config.NextProtos)
	}
}

func TestValidateStrictRealityTLS(t *testing.T) {
	if err := validateStrictRealityTLS(tls.ConnectionState{Version: tls.VersionTLS13, NegotiatedProtocol: "h2"}); err != nil {
		t.Fatalf("strict TLS state should pass: %v", err)
	}
	if err := validateStrictRealityTLS(tls.ConnectionState{Version: tls.VersionTLS12, NegotiatedProtocol: "h2"}); err == nil || !strings.Contains(err.Error(), "TLS 1.3") {
		t.Fatalf("expected TLS version rejection, got %v", err)
	}
	if err := validateStrictRealityTLS(tls.ConnectionState{Version: tls.VersionTLS13}); err == nil || !strings.Contains(err.Error(), "h2") {
		t.Fatalf("expected ALPN rejection, got %v", err)
	}
}
