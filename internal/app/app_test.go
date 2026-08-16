package app

import (
	"bytes"
	"context"
	"encoding/base64"
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

func TestProtocolLinksAndClientEdit(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbounds := []map[string]any{
		{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": "private", "publicKey": "public", "shortId": "abcdef12"},
		{"name": "hy2", "type": "hysteria2", "port": 24445, "sni": "example.test"},
		{"name": "ss", "type": "shadowsocks2022", "port": 24446},
	}
	ids := make([]string, 0, len(inbounds))
	for _, input := range inbounds {
		w := request(t, h, http.MethodPost, "/api/inbounds", input, cookie)
		if w.Code != http.StatusCreated {
			t.Fatalf("inbound %v: %d %s", input["type"], w.Code, w.Body.String())
		}
		var inbound config.Inbound
		_ = json.Unmarshal(w.Body.Bytes(), &inbound)
		ids = append(ids, inbound.ID)
	}
	ss := a.store.Snapshot().Inbounds[2]
	if ss.ServerPassword == "" || !validSS2022Key(ss.ServerPassword) {
		t.Fatalf("invalid ss2022 server key: %q", ss.ServerPassword)
	}
	w := request(t, h, http.MethodPost, "/api/clients", map[string]any{"username": "alice", "inboundIds": ids}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("client create: %d %s", w.Code, w.Body.String())
	}
	var client config.Client
	_ = json.Unmarshal(w.Body.Bytes(), &client)
	w = request(t, h, http.MethodPatch, "/api/clients/"+client.ID, map[string]any{"username": "alice2", "note": "phone", "inboundIds": ids[:2], "maxOnlineIps": 2}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("client edit: %d %s", w.Code, w.Body.String())
	}
	if got := a.store.Snapshot().Clients[0]; got.Username != "alice2" || got.Note != "phone" || len(got.InboundIDs) != 2 {
		t.Fatalf("client edit not applied: %#v", got)
	}
	w = request(t, h, http.MethodGet, "/kota-sub/alice2", nil, nil)
	raw := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(raw, "vless://") || !strings.Contains(raw, "hysteria2://") || !strings.Contains(raw, "ss://") {
		t.Fatalf("raw subscription: %d %s", w.Code, raw)
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "ss://") {
			encoded := line[len("ss://"):strings.Index(line, "@")]
			decoded, err := base64.RawStdEncoding.DecodeString(encoded)
			if err != nil || !strings.HasPrefix(string(decoded), "2022-blake3-aes-256-gcm:") {
				t.Fatalf("invalid ss2022 URI encoding: %s", line)
			}
		}
	}
	browser := httptest.NewRequest(http.MethodGet, "/kota-sub/alice2", nil)
	browser.Header.Set("user-agent", "Mozilla/5.0")
	page := httptest.NewRecorder()
	h.ServeHTTP(page, browser)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "KotaUI") || strings.Contains(page.Body.String(), "vless://") {
		t.Fatalf("browser subscription page: %d %s", page.Code, page.Body.String())
	}
}

func TestIPv6ShareSwitchRequiresPublicAddress(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	w := request(t, h, http.MethodPost, "/api/inbounds", map[string]any{"name": "v6", "type": "hysteria2", "port": 24447, "sni": "example.test", "useIPv6": true}, cookie)
	if w.Code == http.StatusCreated {
		t.Log("test environment has a public IPv6 address; IPv6 inbound created")
	}
	if w.Code != http.StatusBadRequest && w.Code != http.StatusCreated {
		t.Fatalf("unexpected IPv6 status: %d %s", w.Code, w.Body.String())
	}
}

func TestPanelUpdateRequiresAuthentication(t *testing.T) {
	a := testApp(t)
	w := request(t, a.Handler(), http.MethodPost, "/api/update", map[string]string{}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("update auth status %d", w.Code)
	}
}
