package app

import (
	"encoding/json"
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

func TestSummarizePingSamplesUsesWeighted24HourCounts(t *testing.T) {
	got := summarizePingSamples([]pingSample{
		{TargetID: "v4", Sent: 2, Received: 2, AvgMs: 10, MaxMs: 12},
		{TargetID: "v4", Sent: 2, Received: 1, AvgMs: 40, MaxMs: 45},
		{TargetID: "v4", Sent: 2, Received: 0},
		{TargetID: "v6", Sent: 4, Received: 4, AvgMs: 20, MaxMs: 21},
		{TargetID: "ignored", Sent: 0, Received: 0},
	})
	v4 := got["v4"]
	if v4.Checks != 3 || v4.Sent != 6 || v4.Received != 3 {
		t.Fatalf("unexpected v4 counters: %#v", v4)
	}
	if v4.LossPercent != 50 || v4.AverageMs != 20 || v4.MaxMs != 45 {
		t.Fatalf("unexpected v4 summary: %#v", v4)
	}
	v6 := got["v6"]
	if v6.Checks != 1 || v6.LossPercent != 0 || v6.AverageMs != 20 {
		t.Fatalf("unexpected v6 summary: %#v", v6)
	}
	if _, ok := got["ignored"]; ok {
		t.Fatal("sample with no sent probes should not be counted")
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
	var payload struct {
		Summary24h map[string]pingSummary `json:"summary24h"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Summary24h == nil {
		t.Fatalf("network quality response missing 24-hour summaries: err=%v body=%s", err, response.Body.String())
	}
	response = request(t, a.Handler(), http.MethodDelete, "/api/network-quality/targets/"+state.Settings.PingTargets[0].ID, nil, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("delete target status = %d: %s", response.Code, response.Body.String())
	}
	if len(a.store.Snapshot().Settings.PingTargets) != 0 {
		t.Fatal("target was not deleted")
	}
}
