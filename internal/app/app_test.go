package app

import (
	"bytes"
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
