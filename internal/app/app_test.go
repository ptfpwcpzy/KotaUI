package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/store"
)

const (
	testRealityPrivateKey = "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc"
	testRealityPublicKey  = "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	runtime := config.Runtime{DataDir: dir, Listen: "127.0.0.1:0", PanelPath: "/ptf", SubscriptionPort: 1109, Domain: "example.test", AdminUser: "admin", AdminPassword: "test-password", SingBoxConfig: filepath.Join(dir, "sing-box", "config.json"), StatsPort: 9090}
	application, err := New(runtime)
	if err != nil {
		t.Fatal(err)
	}
	application.trafficSyncInterval = 0
	return application
}
func fakeRealityKeypairBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sing-box")
	body := "#!/bin/sh\nprintf 'PrivateKey: " + testRealityPrivateKey + "\\nPublicKey: " + testRealityPublicKey + "\\n'\n"
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
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
	client := map[string]any{"username": "alice", "inboundIds": []string{created.ID}, "totalLimitBytes": 1024, "monthlyLimitBytes": 512, "maxOnlineIps": 1}
	w = request(t, h, http.MethodPost, "/api/clients", client, c)
	if w.Code != http.StatusCreated {
		t.Fatalf("client: %d %s", w.Code, w.Body.String())
	}
	state := a.store.Snapshot()
	saved := state.Clients[0]
	if len(saved.SubscriptionSuffix) != 5 || strings.Trim(saved.SubscriptionSuffix, "abcdefghijklmnopqrstuvwxyz") != "" {
		t.Fatalf("unexpected subscription suffix: %q", saved.SubscriptionSuffix)
	}
	if w = request(t, h, http.MethodGet, "/kota-sub/alice", nil, nil); w.Code != http.StatusNotFound {
		t.Fatalf("predictable subscription path should not work: %d", w.Code)
	}
	w = request(t, h, http.MethodGet, "/kota-sub/"+clientSubscriptionID(saved), nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "hysteria2://") {
		t.Fatalf("subscription: %d %s", w.Code, w.Body.String())
	}
	directClient := map[string]any{"username": "bob", "subscriptionSuffix": "forced", "randomSubscriptionSuffix": false, "inboundIds": []string{created.ID}}
	w = request(t, h, http.MethodPost, "/api/clients", directClient, c)
	if w.Code != http.StatusCreated {
		t.Fatalf("direct subscription client: %d %s", w.Code, w.Body.String())
	}
	directSaved := a.store.Snapshot().Clients[1]
	if directSaved.SubscriptionSuffix != "" {
		t.Fatalf("subscription suffix should be disabled, got %q", directSaved.SubscriptionSuffix)
	}
	if w = request(t, h, http.MethodGet, "/kota-sub/bob", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("direct subscription path: %d", w.Code)
	}
	w = request(t, h, http.MethodPost, "/api/clients/"+saved.ID+"/pause", nil, c)
	if w.Code != http.StatusOK {
		t.Fatalf("pause: %d", w.Code)
	}
	w = request(t, h, http.MethodGet, "/kota-sub/"+clientSubscriptionID(saved), nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("paused subscription: %d", w.Code)
	}
	if _, err := os.Stat(a.runtime.SingBoxConfig); err != nil {
		t.Fatal(err)
	}
}
func TestTUICInboundCreatesRandomClientCredentials(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	w := request(t, h, http.MethodPost, "/api/inbounds", map[string]any{"name": "tuic", "type": "tuic", "port": 24443}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create TUIC inbound: %d %s", w.Code, w.Body.String())
	}
	var inbound config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &inbound)
	w = request(t, h, http.MethodPost, "/api/clients", map[string]any{"username": "tuic-user", "inboundIds": []string{inbound.ID}}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create TUIC client: %d %s", w.Code, w.Body.String())
	}
	client := a.store.Snapshot().Clients[0]
	if !config.ValidUUID(client.Credentials[inbound.ID]) || client.TUICPasswords[inbound.ID] == "" {
		t.Fatalf("invalid TUIC credentials: %#v", client)
	}
	w = request(t, h, http.MethodGet, "/kota-sub/"+clientSubscriptionID(client), nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "tuic://") {
		t.Fatalf("TUIC subscription: %d %s", w.Code, w.Body.String())
	}
}

func TestRealityClientUsesUUIDCredentials(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbound := map[string]any{"name": "reality", "type": "reality", "port": 24443, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": testRealityPrivateKey, "publicKey": testRealityPublicKey, "shortId": "1234abcd"}
	w := request(t, h, http.MethodPost, "/api/inbounds", inbound, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create reality inbound: %d %s", w.Code, w.Body.String())
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	w = request(t, h, http.MethodPost, "/api/clients", map[string]any{"username": "reality-user", "inboundIds": []string{created.ID}}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create reality client: %d %s", w.Code, w.Body.String())
	}
	client := a.store.Snapshot().Clients[0]
	if !config.ValidUUID(client.Credentials[created.ID]) {
		t.Fatalf("REALITY credential must be UUID, got %q", client.Credentials[created.ID])
	}
	w = request(t, h, http.MethodGet, "/kota-sub/"+clientSubscriptionID(client), nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "vless://"+client.Credentials[created.ID]+"@") {
		t.Fatalf("REALITY subscription: %d %s", w.Code, w.Body.String())
	}
}

func TestRejectsSubscriptionIDCollision(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	w := request(t, h, http.MethodPost, "/api/inbounds", map[string]any{"name": "hy2", "type": "hysteria2", "port": 24443, "sni": "example.test", "upMbps": 50, "downMbps": 200}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	var inbound config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &inbound)
	w = request(t, h, http.MethodPost, "/api/clients", map[string]any{"username": "alice", "inboundIds": []string{inbound.ID}}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create first client: %d %s", w.Code, w.Body.String())
	}
	first := a.store.Snapshot().Clients[0]
	w = request(t, h, http.MethodPost, "/api/clients", map[string]any{"username": clientSubscriptionID(first), "randomSubscriptionSuffix": false, "inboundIds": []string{inbound.ID}}, cookie)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "订阅地址与已有客户端冲突") {
		t.Fatalf("subscription collision should be rejected: %d %s", w.Code, w.Body.String())
	}
}
func TestNormalizeRealityCandidates(t *testing.T) {
	candidates, err := normalizeRealityCandidates([]config.RealityCandidate{
		{Host: "WWW.Cloudflare.COM.", Port: 8443},
		{Host: "www.cloudflare.com", Port: 443},
		{Host: "www.microsoft.com", Port: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Host != "www.cloudflare.com" || candidates[0].Port != 443 || candidates[1].Port != 443 {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
	if _, err := normalizeRealityCandidates([]config.RealityCandidate{{Host: "invalid"}}); err == nil {
		t.Fatal("expected invalid candidate to fail")
	}
}

func TestSubscriptionPageShowsMonthlyLimit(t *testing.T) {
	a := testApp(t)
	page := a.subscriptionPage(config.Client{Username: "alice", UsedBytes: 5 * 1024 * 1024, TotalLimitBytes: 100 * 1024 * 1024, MonthlyUsedBytes: 2 * 1024 * 1024, MonthlyLimitBytes: 10 * 1024 * 1024}, 2, "https://example.test/sub/alice")
	for _, text := range []string{"每月限额", "本月流量", "累计"} {
		if !strings.Contains(page, text) {
			t.Fatalf("subscription page missing %q", text)
		}
	}
}

func TestCertificateStatusShowsExpiry(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(9 * 24 * time.Hour).UTC()
	certificate, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiresAt}, &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiresAt}, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "certificate.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0600); err != nil {
		t.Fatal(err)
	}
	status := certificateStatus(path)
	if !status.Valid || status.ExpiresAt != expiresAt.Local().Format("2006-01-02") || status.Days < 8 {
		t.Fatalf("unexpected certificate status: %#v", status)
	}
}

func TestRecentOnlineUsers(t *testing.T) {
	now := time.Now().UTC()
	clients := []config.Client{
		{Username: "fresh", LastActiveAt: now.Add(-5 * time.Second)},
		{Username: "older", LastActiveAt: now.Add(-15 * time.Second)},
		{Username: "stale", LastActiveAt: now.Add(-onlineActivityWindow - time.Second)},
		{Username: "paused", Paused: true, LastActiveAt: now.Add(-time.Second)},
	}
	users := recentOnlineUsers(clients, now)
	if len(users) != 2 || users[0]["username"] != "fresh" || users[1]["username"] != "older" {
		t.Fatalf("unexpected recent users: %#v", users)
	}
	stamp := filepath.Join(t.TempDir(), "core.started")
	if err := os.WriteFile(stamp, []byte(strconv.FormatInt(now.Add(-90*time.Second).Unix(), 10)), 0600); err != nil {
		t.Fatal(err)
	}
	if got := recordedUptime(stamp, now); got < 89 || got > 91 {
		t.Fatalf("uptime = %d", got)
	}
}

func TestPublicNetworkAddressesFrom(t *testing.T) {
	addresses := []net.Addr{
		&net.IPAddr{IP: net.ParseIP("198.51.100.29")},
		&net.IPAddr{IP: net.ParseIP("2001:db8::29")},
		&net.IPAddr{IP: net.ParseIP("10.0.0.2")},
		&net.IPAddr{IP: net.ParseIP("fe80::1")},
		&net.IPAddr{IP: net.ParseIP("198.51.100.29")},
	}
	got := publicNetworkAddressesFrom(addresses)
	if len(got.IPv4) != 1 || got.IPv4[0] != "198.51.100.29" {
		t.Fatalf("unexpected IPv4 addresses: %#v", got.IPv4)
	}
	if len(got.IPv6) != 1 || got.IPv6[0] != "2001:db8::29" {
		t.Fatalf("unexpected IPv6 addresses: %#v", got.IPv6)
	}
}

func TestDashboardAndSNITest(t *testing.T) {
	a := testApp(t)
	h := a.Handler()
	cookie := login(t, a)
	dashboard := request(t, h, http.MethodGet, "/api/dashboard", nil, cookie)
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status %d", dashboard.Code)
	}
	if !strings.Contains(dashboard.Body.String(), `"network"`) {
		t.Fatalf("dashboard network field missing: %s", dashboard.Body.String())
	}
	if asset := request(t, h, http.MethodGet, "/assets/overview.js", nil, nil); asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "viewDashboard") {
		t.Fatalf("overview script asset %d: %s", asset.Code, asset.Body.String())
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
	a.runtime.SingBoxBin = fakeRealityKeypairBinary(t)
	w := request(t, a.Handler(), http.MethodPost, "/api/inbounds", map[string]any{"name": "auto", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com"}, login(t, a))
	if w.Code != http.StatusCreated {
		t.Fatalf("auto key creation %d: %s", w.Code, w.Body.String())
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.PrivateKey != testRealityPrivateKey || created.PublicKey != testRealityPublicKey || created.ShortID == "" || created.SNI != "www.cloudflare.com" {
		t.Fatalf("unexpected generated inbound %#v", created)
	}
}

func TestInvalidRealityKeysAreRegenerated(t *testing.T) {
	a := testApp(t)
	a.runtime.SingBoxBin = fakeRealityKeypairBinary(t)
	inbound := config.Inbound{Type: "reality", HandshakeServer: "www.cloudflare.com", PrivateKey: "legacy-private", PublicKey: "legacy-public"}
	if err := a.prepareRealityInbound(&inbound); err != nil {
		t.Fatalf("regenerate reality keys: %v", err)
	}
	if inbound.PrivateKey != testRealityPrivateKey || inbound.PublicKey != testRealityPublicKey || !validRealityKeyPair(inbound.PrivateKey, inbound.PublicKey) {
		t.Fatalf("invalid regenerated keypair: %#v", inbound)
	}
}

func TestNewRepairsInvalidRealityKeys(t *testing.T) {
	dir := t.TempDir()
	seed, err := store.Open(dir, "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Update(func(state *config.State) error {
		state.Inbounds = append(state.Inbounds, config.Inbound{ID: "legacy", Name: "legacy reality", Type: "reality", Listen: "0.0.0.0", Port: 24444, Enabled: true, HandshakeServer: "www.cloudflare.com", HandshakePort: 443, SNI: "www.cloudflare.com", PrivateKey: "legacy-private", PublicKey: "legacy-public", ShortID: "abcdef12"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime := config.Runtime{DataDir: dir, Listen: "127.0.0.1:0", PanelPath: "/ptf", Domain: "example.test", AdminUser: "admin", AdminPassword: "test-password", SingBoxBin: fakeRealityKeypairBinary(t), SingBoxConfig: filepath.Join(dir, "sing-box", "config.json")}
	a, err := New(runtime)
	if err != nil {
		t.Fatalf("start with legacy reality state: %v", err)
	}
	repaired := a.store.Snapshot().Inbounds[0]
	if repaired.PrivateKey != testRealityPrivateKey || repaired.PublicKey != testRealityPublicKey || !validRealityKeyPair(repaired.PrivateKey, repaired.PublicKey) {
		t.Fatalf("legacy state was not repaired: %#v", repaired)
	}
}

func TestInboundEditPreservesRealityKeys(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	original := map[string]any{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": testRealityPrivateKey, "publicKey": testRealityPublicKey, "shortId": "abcdef12"}
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
	if saved.PrivateKey != testRealityPrivateKey || saved.PublicKey != testRealityPublicKey || saved.ShortID != "abcdef12" {
		t.Fatalf("keys were rotated: %#v", saved)
	}
}

func TestInboundToggle(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbound := map[string]any{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": testRealityPrivateKey, "publicKey": testRealityPublicKey}
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
	reality := map[string]any{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": testRealityPrivateKey, "publicKey": testRealityPublicKey}
	if got := request(t, h, http.MethodPost, "/api/inbounds", reality, c).Code; got != http.StatusCreated {
		t.Fatalf("reality %d", got)
	}
}

func TestProtocolLinksAndClientEdit(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbounds := []map[string]any{
		{"name": "reality", "type": "reality", "port": 24444, "handshakeServer": "www.cloudflare.com", "sni": "www.cloudflare.com", "privateKey": testRealityPrivateKey, "publicKey": testRealityPublicKey, "shortId": "abcdef12"},
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
	w = request(t, h, http.MethodPatch, "/api/clients/"+client.ID, map[string]any{"username": "alice2", "inboundIds": ids[:2], "maxOnlineIps": 2}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("client edit: %d %s", w.Code, w.Body.String())
	}
	if got := a.store.Snapshot().Clients[0]; got.Username != "alice2" || got.SubscriptionSuffix != client.SubscriptionSuffix || len(got.InboundIDs) != 2 {
		t.Fatalf("client edit not applied: %#v", got)
	}
	w = request(t, h, http.MethodGet, "/kota-sub/"+clientSubscriptionID(a.store.Snapshot().Clients[0]), nil, nil)
	raw := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(raw, "vless://") || !strings.Contains(raw, "hysteria2://") || !strings.Contains(raw, "ss://") {
		t.Fatalf("raw subscription: %d %s", w.Code, raw)
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "ss://") {
			encoded := line[len("ss://"):strings.Index(line, "@")]
			decoded, err := base64.RawStdEncoding.DecodeString(encoded)
			parts := strings.SplitN(string(decoded), ":", 3)
			if err != nil || len(parts) != 3 || parts[0] != "2022-blake3-aes-256-gcm" || parts[1] != ss.ServerPassword || parts[2] != client.Credentials[ids[2]] {
				t.Fatalf("invalid SS2022 multi-user URI encoding: %s", line)
			}
		}
	}
	browser := httptest.NewRequest(http.MethodGet, "/kota-sub/"+clientSubscriptionID(a.store.Snapshot().Clients[0]), nil)
	browser.Header.Set("user-agent", "Mozilla/5.0")
	page := httptest.NewRecorder()
	h.ServeHTTP(page, browser)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "KotaUI") || !strings.Contains(page.Body.String(), "连接正常") || !strings.Contains(page.Body.String(), "viewBox") || strings.Contains(page.Body.String(), "vless://") {
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
	w = request(t, a.Handler(), http.MethodGet, "/api/update/status", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("update status auth %d", w.Code)
	}
	w = request(t, a.Handler(), http.MethodGet, "/api/logs/update", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("update logs auth %d", w.Code)
	}
}

func TestUpdateStatusReadsPersistentState(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	for state, wantRunning := range map[string]bool{"running": true, "success": false, "failed": false} {
		if err := a.writeUpdateState(state); err != nil {
			t.Fatal(err)
		}
		w := request(t, a.Handler(), http.MethodGet, "/api/update/status", nil, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("status %s: %d %s", state, w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if running, _ := result["running"].(bool); running != wantRunning {
			t.Fatalf("state %s running=%v", state, running)
		}
	}
}

func TestUpdateStatusReturnsPersistentProgressMessage(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	if err := a.writeUpdateProgress("running", "正在构建 KotaUI 面板程序…"); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), http.MethodGet, "/api/update/status", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["running"] != true || result["message"] != "正在构建 KotaUI 面板程序…" {
		t.Fatalf("unexpected progress: %#v", result)
	}
}

func TestUpdateStatusScopesResultToRequestedRun(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	if err := a.writeUpdateProgressForRun("200", "success", "更新完成，面板与核心已恢复。"); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), http.MethodGet, "/api/update/status?runId=100", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "superseded" || result["running"] != false {
		t.Fatalf("unexpected scoped result: %#v", result)
	}
}

func TestUpdateStatusReturnsStructuredRunMetadata(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	if err := a.writeUpdateProgressForRun("123456789", "running", "正在构建 KotaUI 面板程序…"); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), http.MethodGet, "/api/update/status?runId=123456789", nil, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["runId"] != "123456789" || result["state"] != "running" || result["updatedAt"] == nil {
		t.Fatalf("unexpected structured update result: %#v", result)
	}
}

func TestPanelRestartAcknowledgesBeforeServiceStop(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	w := request(t, h, http.MethodPost, "/api/services/panel/restart", map[string]string{}, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("panel restart status %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["ok"] != true {
		t.Fatalf("panel restart response: %#v", body)
	}
}

func TestServiceActionsBlockedDuringPanelUpdate(t *testing.T) {
	a := testApp(t)
	a.updateRunning = true
	w := request(t, a.Handler(), http.MethodPost, "/api/services/singbox/restart", map[string]string{}, login(t, a))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "更新正在执行") {
		t.Fatalf("service action during update: %d %s", w.Code, w.Body.String())
	}
}

func TestSubscriptionHeadersAndMaintenanceAuthentication(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbound := map[string]any{"name": "hy2", "type": "hysteria2", "port": 24443, "sni": "example.test"}
	w := request(t, h, http.MethodPost, "/api/inbounds", inbound, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	var created config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	client := map[string]any{"username": "headeruser", "inboundIds": []string{created.ID}, "totalLimitBytes": int64(1073741824), "expiresAt": "2030-01-02"}
	w = request(t, h, http.MethodPost, "/api/clients", client, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", w.Code, w.Body.String())
	}
	if err := a.store.Update(func(state *config.State) error {
		for i := range state.Clients {
			if state.Clients[i].Username == "headeruser" {
				state.Clients[i].UploadBytes = 123
				state.Clients[i].DownloadBytes = 456
				state.Clients[i].UsedBytes = 579
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	headerClient := a.store.Snapshot().Clients[0]
	w = request(t, h, http.MethodGet, "/kota-sub/"+clientSubscriptionID(headerClient), nil, nil)
	if got := w.Header().Get("Subscription-Userinfo"); !strings.Contains(got, "upload=123") || !strings.Contains(got, "download=456") || !strings.Contains(got, "total=1073741824") || !strings.Contains(got, "expire=") {
		t.Fatalf("subscription userinfo header: %q", got)
	}
	if got := w.Header().Get("Profile-Title"); got != "KotaUI · headeruser" {
		t.Fatalf("profile title: %q", got)
	}
	for _, endpoint := range []string{"/api/logs/panel", "/api/certificate/renew"} {
		w = request(t, h, http.MethodPost, endpoint, nil, nil)
		if endpoint == "/api/logs/panel" {
			w = request(t, h, http.MethodGet, endpoint, nil, nil)
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s auth status %d", endpoint, w.Code)
		}
	}
}

func TestHysteriaBandwidthCanBeConfigured(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	w := request(t, h, http.MethodPost, "/api/inbounds", map[string]any{"name": "custom-hy2", "type": "hysteria2", "port": 24448, "sni": "example.test", "upMbps": 100, "downMbps": 200}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create hysteria2: %d %s", w.Code, w.Body.String())
	}
	var custom config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &custom)
	if custom.UpMbps != 100 || custom.DownMbps != 200 {
		t.Fatalf("custom hysteria2 bandwidth %d/%d", custom.UpMbps, custom.DownMbps)
	}
	w = request(t, h, http.MethodPost, "/api/inbounds", map[string]any{"name": "default-hy2", "type": "hysteria2", "port": 24449, "sni": "example.test"}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create default hysteria2: %d %s", w.Code, w.Body.String())
	}
	var defaults config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &defaults)
	if defaults.UpMbps != 500 || defaults.DownMbps != 500 {
		t.Fatalf("default hysteria2 bandwidth %d/%d", defaults.UpMbps, defaults.DownMbps)
	}
}

func TestShadowsocks2022UsesPaddedStandardBase64(t *testing.T) {
	key := make([]byte, 32)
	legacyRaw := base64.RawStdEncoding.EncodeToString(key)
	if validSS2022Key(legacyRaw) {
		t.Fatalf("legacy raw Base64 key must be regenerated: %q", legacyRaw)
	}
	standard := config.RandomBase64(32)
	if !strings.HasSuffix(standard, "=") || !validSS2022Key(standard) {
		t.Fatalf("invalid standard Base64 key: %q", standard)
	}
}

func TestNormalizeProtocolSecretsRepairsLegacyShadowsocksKey(t *testing.T) {
	a := testApp(t)
	legacy := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	if err := a.store.Update(func(state *config.State) error {
		state.Inbounds = append(state.Inbounds, config.Inbound{ID: "legacy-ss", Name: "legacy ss", Type: "shadowsocks2022", ServerPassword: legacy})
		state.Clients = append(state.Clients, config.Client{ID: "client", Credentials: map[string]string{"legacy-ss": legacy}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := normalizeProtocolSecrets(a.store, a.runtime.SingBoxBin); err != nil {
		t.Fatal(err)
	}
	state := a.store.Snapshot()
	if !validSS2022Key(state.Inbounds[0].ServerPassword) || !validSS2022Key(state.Clients[0].Credentials["legacy-ss"]) {
		t.Fatalf("legacy SS2022 keys were not repaired: %#v", state)
	}
}

func TestNormalizeProtocolSecretsRepairsLegacyRealityCredential(t *testing.T) {
	a := testApp(t)
	if err := a.store.Update(func(state *config.State) error {
		state.Inbounds = append(state.Inbounds, config.Inbound{ID: "legacy-reality", Name: "legacy reality", Type: "reality", PrivateKey: testRealityPrivateKey, PublicKey: testRealityPublicKey})
		state.Clients = append(state.Clients, config.Client{ID: "client", Credentials: map[string]string{"legacy-reality": "0123456789abcdef0123456789abcdef"}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := normalizeProtocolSecrets(a.store, a.runtime.SingBoxBin); err != nil {
		t.Fatal(err)
	}
	if credential := a.store.Snapshot().Clients[0].Credentials["legacy-reality"]; !config.ValidUUID(credential) {
		t.Fatalf("legacy REALITY credential was not repaired: %q", credential)
	}
}

func TestResolveClientExpiry(t *testing.T) {
	now := time.Date(2028, time.January, 31, 15, 0, 0, 0, time.FixedZone("test", 8*3600))
	for _, test := range []struct {
		name    string
		current string
		legacy  string
		unit    string
		amount  int
		want    string
		wantErr bool
	}{
		{name: "legacy date", legacy: "2030-01-02", want: "2030-01-02"},
		{name: "unlimited", current: "2030-01-02", unit: "none", want: ""},
		{name: "keep", current: "2030-01-02", unit: "keep", want: "2030-01-02"},
		{name: "days", unit: "day", amount: 1, want: "2028-02-01"},
		{name: "months clamp month end", unit: "month", amount: 1, want: "2028-02-29"},
		{name: "years", unit: "year", amount: 1, want: "2029-01-31"},
		{name: "invalid amount", unit: "day", amount: 0, wantErr: true},
		{name: "invalid unit", unit: "week", amount: 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveClientExpiry(test.current, test.legacy, test.unit, test.amount, now)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected expiry error")
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("expiry = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestClientCreateAcceptsDurationExpiry(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	w := request(t, h, http.MethodPost, "/api/inbounds", map[string]any{"name": "expiry-hy2", "type": "hysteria2", "port": 24443, "sni": "example.test"}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	var inbound config.Inbound
	_ = json.Unmarshal(w.Body.Bytes(), &inbound)
	w = request(t, h, http.MethodPost, "/api/clients", map[string]any{"username": "expiry-user", "inboundIds": []string{inbound.ID}, "expiryUnit": "day", "expiryAmount": 1}, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("create duration client: %d %s", w.Code, w.Body.String())
	}
	want := time.Now().In(time.Local).AddDate(0, 0, 1).Format("2006-01-02")
	if got := a.store.Snapshot().Clients[0].ExpiresAt; got != want {
		t.Fatalf("duration expiry = %q, want %q", got, want)
	}
}

func TestInboundMutationUsesOpenRCServiceRestart(t *testing.T) {
	previous := systemdAvailable
	systemdAvailable = func() bool { return false }
	t.Cleanup(func() { systemdAvailable = previous })

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "rc-service.log")
	script := filepath.Join(binDir, "rc-service")
	body := "#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\" >> \"$KOTAUI_TEST_SERVICE_LOG\"\n[ \"$2\" = status ] && exit 0\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("KOTAUI_TEST_SERVICE_LOG", logPath)

	a := testApp(t)
	a.runtime.ManageSingBox = true
	a.runtime.SingBoxBin = fakeRealityKeypairBinary(t)
	w := request(t, a.Handler(), http.MethodPost, "/api/inbounds", map[string]any{
		"name": "alpine-hy2", "type": "hysteria2", "port": 24443, "sni": "example.test",
	}, login(t, a))
	if w.Code != http.StatusCreated {
		t.Fatalf("OpenRC inbound create: %d %s", w.Code, w.Body.String())
	}
	output, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(output); got != "kotaui-singbox restart\nkotaui-singbox status\n" {
		t.Fatalf("unexpected OpenRC command %q", got)
	}
}

func TestServiceCommandSelectsOpenRC(t *testing.T) {
	previous := systemdAvailable
	systemdAvailable = func() bool { return false }
	t.Cleanup(func() { systemdAvailable = previous })
	command := serviceCommand("kotaui-singbox", "restart")
	if len(command.Args) != 3 || command.Args[0] != "rc-service" || command.Args[1] != "kotaui-singbox" || command.Args[2] != "restart" {
		t.Fatalf("unexpected OpenRC command %#v", command.Args)
	}
}
