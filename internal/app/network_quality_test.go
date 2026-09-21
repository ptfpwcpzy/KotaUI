package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidatePingTarget(t *testing.T) {
	for _, test := range []struct {
		name, address string
		wantErr       bool
	}{
		{"IPv4", "1.1.1.1", false},
		{"IPv6", "2606:4700:4700::1111", false},
		{"domain", "example.com", false},
		{"empty", "", true},
		{"bad", "bad address", true},
		{"path", "example.com/path", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validatePingTarget("目标", test.address); (err != nil) != test.wantErr {
				t.Fatalf("validatePingTarget(%q) error = %v, wantErr %v", test.address, err, test.wantErr)
			}
		})
	}
}

func TestPingManagerPrunesHistoryAndRemovesTarget(t *testing.T) {
	dir := t.TempDir()
	manager := newPingManager(dir)
	now := time.Now().UTC()
	manager.add(pingSample{TargetID: "keep", CheckedAt: now, Received: 1})
	manager.add(pingSample{TargetID: "old", CheckedAt: now.Add(-25 * time.Hour), Received: 1})
	if got := len(manager.snapshot(map[string]bool{"keep": true}, now)); got != 1 {
		t.Fatalf("snapshot count = %d, want 1", got)
	}
	manager.removeTarget("keep")
	if got := len(manager.snapshot(nil, now)); got != 0 {
		t.Fatalf("snapshot after remove = %d, want 0", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "ping-history.json")); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkQualityTargetAPI(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	response := request(t, a.Handler(), http.MethodPost, "/api/network-quality/targets", map[string]string{"name": "Cloudflare", "address": "1.1.1.1"}, cookie)
	if response.Code != http.StatusCreated {
		t.Fatalf("create target status = %d: %s", response.Code, response.Body.String())
	}
	state := a.store.Snapshot()
	if len(state.Settings.PingTargets) != 1 || state.Settings.PingTargets[0].Address != "1.1.1.1" {
		t.Fatalf("unexpected targets: %#v", state.Settings.PingTargets)
	}
	response = request(t, a.Handler(), http.MethodGet, "/api/network-quality", nil, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("network quality status = %d", response.Code)
	}
	response = request(t, a.Handler(), http.MethodDelete, "/api/network-quality/targets/"+state.Settings.PingTargets[0].ID, nil, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("delete target status = %d: %s", response.Code, response.Body.String())
	}
	if len(a.store.Snapshot().Settings.PingTargets) != 0 {
		t.Fatal("target was not deleted")
	}
}
