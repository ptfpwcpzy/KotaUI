package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func TestInboundRejectsDuplicatePort(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	first := map[string]any{"name": "hy2-a", "type": "hysteria2", "port": 24443, "sni": "example.test"}
	if got := request(t, h, "POST", "/api/inbounds", first, cookie).Code; got != 201 {
		t.Fatalf("first inbound status = %d", got)
	}
	second := map[string]any{"name": "hy2-b", "type": "hysteria2", "port": 24443, "sni": "example.test"}
	if got := request(t, h, "POST", "/api/inbounds", second, cookie).Code; got != 400 {
		t.Fatalf("duplicate port status = %d", got)
	}
}

func TestClientInputRejectsInvalidDateAndDuplicateBindings(t *testing.T) {
	a := testApp(t)
	h, cookie := a.Handler(), login(t, a)
	inbound := map[string]any{"name": "hy2", "type": "hysteria2", "port": 24443, "sni": "example.test"}
	w := request(t, h, "POST", "/api/inbounds", inbound, cookie)
	if w.Code != 201 {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	id := a.store.Snapshot().Inbounds[0].ID
	invalidDate := map[string]any{"username": "alice", "inboundIds": []string{id}, "expiresAt": "not-a-date"}
	if got := request(t, h, "POST", "/api/clients", invalidDate, cookie).Code; got != 400 {
		t.Fatalf("invalid date status = %d", got)
	}
	duplicateBinding := map[string]any{"username": "alice", "inboundIds": []string{id, id}}
	if got := request(t, h, "POST", "/api/clients", duplicateBinding, cookie).Code; got != 400 {
		t.Fatalf("duplicate binding status = %d", got)
	}
}

func TestConfigApplyKeepsLiveFileOnValidationFailure(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "sing-box", "config.json")
	if err := os.MkdirAll(filepath.Dir(live), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("previous-config"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime := config.Runtime{DataDir: dir, Domain: "example.test", SingBoxBin: "/bin/false", SingBoxConfig: live, StatsPort: 9090}
	if err := writeAndValidateConfig(config.DefaultState("example.test"), runtime); err == nil {
		t.Fatal("expected validation failure")
	}
	body, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "previous-config" {
		t.Fatalf("live config changed after failed validation: %q", body)
	}
}

func TestTailFileUsesBoundedRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.log")
	body := strings.Repeat("discarded line\n", 30000) + "final line\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := tailFile(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "final line") || len(result) > 1024 {
		t.Fatalf("unexpected tail output: len=%d %q", len(result), result)
	}
}

func TestSubscriptionRouteUsesConfiguredPath(t *testing.T) {
	a := testApp(t)
	if err := a.store.Update(func(state *config.State) error {
		state.Settings.SubscriptionPath = "/private-sub"
		state.Inbounds = []config.Inbound{{ID: "hy2", Name: "hy2", Type: "hysteria2", Enabled: true, Port: 24443}}
		state.Clients = []config.Client{{Username: "alice", InboundIDs: []string{"hy2"}, Credentials: map[string]string{"hy2": "client-secret"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), "GET", "/private-sub/alice", nil, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hysteria2://") {
		t.Fatalf("configured subscription route failed: %d %q", w.Code, w.Body.String())
	}
}
