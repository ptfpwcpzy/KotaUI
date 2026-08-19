package app

import (
	"net/http"
	"testing"
)

func TestSettingsUpdatesOutboundStrategy(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)

	w := request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]string{"outboundStrategy": "prefer_ipv4"}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", w.Code, w.Body.String())
	}
	if got := a.store.Snapshot().Settings.OutboundStrategy; got != "prefer_ipv4" {
		t.Fatalf("stored strategy = %q", got)
	}

	w = request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]string{"outboundStrategy": "invalid"}, cookie)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid strategy status = %d: %s", w.Code, w.Body.String())
	}
}
