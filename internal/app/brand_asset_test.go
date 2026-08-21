package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSharedLogoAssetIsAvailableToPanelAndSubscriptionService(t *testing.T) {
	a := testApp(t)
	for name, handler := range map[string]http.Handler{
		"panel":        a.Handler(),
		"subscription": a.subscriptionHandler(),
	} {
		for _, path := range []string{"/assets/kotaui-logo.png", "/favicon.ico"} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("content-type"), "image/png") || response.Body.Len() < 64 {
				t.Fatalf("%s %s: code=%d type=%q bytes=%d", name, path, response.Code, response.Header().Get("content-type"), response.Body.Len())
			}
		}
	}
}
