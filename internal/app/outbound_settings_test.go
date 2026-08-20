package app

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestSettingsUpdatesOutboundStrategy(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)

	w := request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]string{"outboundStrategy": "prefer_ipv4"}, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("update status = %d: %s", w.Code, w.Body.String())
	}
	if got := a.store.Snapshot().Settings.OutboundStrategy; got != "prefer_ipv4" {
		t.Fatalf("stored strategy = %q", got)
	}
	var response struct {
		Applied bool   `json:"applied"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.Message == "" {
		t.Fatalf("settings response = %#v", response)
	}
	deadline := time.Now().Add(time.Second)
	for a.settingsApplyInProgress() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.settingsApplyInProgress() {
		t.Fatal("settings apply task did not finish")
	}

	w = request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]string{"outboundStrategy": "invalid"}, cookie)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid strategy status = %d: %s", w.Code, w.Body.String())
	}
}

func TestSettingsSavesRealityCandidatesWithoutCoreReload(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	w := request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]any{
		"realityCandidates": []map[string]any{{"host": "www.example.com", "port": 443}},
	}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("candidate save status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Applied bool   `json:"applied"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Applied || response.Message == "" || a.settingsApplyInProgress() {
		t.Fatalf("candidate save response = %#v, applying=%v", response, a.settingsApplyInProgress())
	}
	candidates := a.store.Snapshot().Settings.RealityCandidates
	if len(candidates) != 1 || candidates[0].Host != "www.example.com" {
		t.Fatalf("stored candidates = %#v", candidates)
	}
}
