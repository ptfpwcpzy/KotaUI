package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	runtime := config.Runtime{DataDir: dir, Listen: "127.0.0.1:0", PanelPath: "/ptf", SubscriptionPort: 1109, Domain: "example.test", AdminUser: "admin", AdminPassword: "test-password", SingBoxConfig: filepath.Join(dir, "sing-box", "config.json"), StatsPort: 9090}
	application, err := New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	return application
}
func request(t *testing.T, h http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, reader)
	r.Header.Set("content-type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func login(t *testing.T, a *App) *http.Cookie {
	w := request(t, a.Handler(), http.MethodPost, "/api/login", map[string]string{"username": "admin", "password": "test-password"}, nil)
	if w.Code != 200 {
		t.Fatalf("login %d: %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestAuthenticationProtectsState(t *testing.T) {
	a := testApp(t)
	w := request(t, a.Handler(), http.MethodGet, "/api/state", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Code)
	}
	w = request(t, a.Handler(), http.MethodGet, "/api/state", nil, login(t, a))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}
func TestInboundClientAndSubscription(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	c := login(t, a)
	inbound := map[string]any{"name": "hy2", "type": "hysteria2", "listen": "0.0.0.0", "port": 24443, "sni": "example.test", "upMbps": 50, "downMbps": 200}
	w := request(t, h, http.MethodPost, "/api/inbounds", inbound, c)
	if w.Code != http.StatusCreated {
		t.Fatalf("inbound: %d %s", w.Code, w.Body.String())
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	client := map[string]any{"username": "alice", "note": "测试", "inboundIds": []string{created.ID}, "totalLimitBytes": 1024, "monthlyLimitBytes": 512, "maxOnlineIps": 1}
	w = request(t, h, http.MethodPost, "/api/clients", client, c)
	if w.Code != http.StatusCreated {
		t.Fatalf("client: %d %s", w.Code, w.Body.String())
	}
	w = request(t, h, http.MethodGet, "/kota-sub/alice", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hysteria2://") {
		t.Fatalf("subscription: %d %s", w.Code, w.Body.String())
	}
	var saved config.Client
	_ = json.Unmarshal(request(t, h, http.MethodGet, "/api/clients", nil, c).Body.Bytes(), &[]config.Client{})
	state := a.store.Snapshot()
	saved = state.Clients[0]
	w = request(t, h, http.MethodPost, "/api/clients/"+saved.ID+"/pause", nil, c)
	if w.Code != http.StatusOK {
		t.Fatalf("pause: %d", w.Code)
	}
	w = request(t, h, http.MethodGet, "/kota-sub/alice", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("paused subscription: %d", w.Code)
	}
	if _, err := os.Stat(a.runtime.SingBoxConfig); err != nil {
		t.Fatal(err)
	}
}
func TestDashboardAndSNITest(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	cookie := login(t, a)
	if got := request(t, h, http.MethodGet, "/api/dashboard", nil, cookie).Code; got != http.StatusOK {
		t.Fatalf("dashboard status %d", got)
	}
	a.sniProbe = func(_ context.Context, host string, port int) (sniTestResult, error) {
		if host != "www.example.com" || port != 443 {
			t.Fatalf("unexpected target %s:%d", host, port)
		}
		return sniTestResult{Address: "203.0.113.20", LatencyMS: 43, TLSVersion: "TLS 1.3"}, nil
	}
	if got := request(t, h, http.MethodPost, "/api/reality/test", map[string]any{"host": "www.example.com", "port": 443}, nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("SNI auth status %d", got)
	}
	w := request(t, h, http.MethodPost, "/api/reality/test", map[string]any{"host": "www.example.com", "port": 443}, cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("SNI response %d: %s", w.Code, w.Body.String())
	}
	if got := request(t, h, http.MethodPost, "/api/reality/test", map[string]any{"host": "127.0.0.1", "port": 443}, cookie).Code; got != http.StatusBadRequest {
		t.Fatalf("IP SNI status %d", got)
	}
}

func TestRealityKeysAreGeneratedAutomatically(t *testing.T) {
	a := testApp(t)
	fake := filepath.Join(t.TempDir(), "sing-box")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'PrivateKey: private-auto\\nPublicKey: public-auto\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a.runtime.SingBoxBin = fake
	w := request(t, a.Handler(), http.MethodPost, "/api/inbounds", map[string]any{"name": "auto", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com"}, login(t, a))
	if w.Code != http.StatusCreated {
		t.Fatalf("auto key creation %d: %s", w.Code, w.Body.String())
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.PrivateKey != "private-auto" || created.PublicKey != "public-auto" || created.ShortID == "" || created.SNI != "www.cloudflare.com" {
		t.Fatalf("unexpected generated inbound %#v", created)
	}
}

func TestInboundEditPreservesRealityKeys(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	original := map[string]any{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": "private-old", "publicKey": "public-old", "shortId": "abcdef12"}
	w := request(t, h, http.MethodPost, "/api/inbounds", original, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d", w.Code)
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	updated := map[string]any{"name": "renamed", "type": "reality", "port": 24445, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com"}
	w = request(t, h, http.MethodPatch, "/api/inbounds/"+created.ID, updated, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", w.Code, w.Body.String())
	}
	saved := a.store.Snapshot().Inbounds[0]
	if saved.PrivateKey != "private-old" || saved.PublicKey != "public-old" || saved.ShortID != "abcdef12" {
		t.Fatalf("keys were rotated: %#v", saved)
	}
}

func TestInboundToggle(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbound := map[string]any{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": "private", "publicKey": "public"}
	w := request(t, h, http.MethodPost, "/api/inbounds", inbound, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	w = request(t, h, http.MethodPost, "/api/inbounds/"+created.ID+"/toggle", nil, cookie)
	if w.Code != http.StatusOK || a.store.Snapshot().Inbounds[0].Enabled {
		t.Fatalf("toggle failed: %d", w.Code)
	}
}

func TestSupportedInboundValidation(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	c := login(t, a)
	bad := map[string]any{"name": "bad", "type": "vmess", "port": 24443}
	if got := request(t, h, http.MethodPost, "/api/inbounds", bad, c).Code; got != http.StatusBadRequest {
		t.Fatalf("unsupported type %d", got)
	}
	reality := map[string]any{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": "private", "publicKey": "public"}
	if got := request(t, h, http.MethodPost, "/api/inbounds", reality, c).Code; got != http.StatusCreated {
		t.Fatalf("reality %d", got)
	}
}
