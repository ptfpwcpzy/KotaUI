package app

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCertificateRenewStatusExpiresStaleTask(t *testing.T) {
	a := testApp(t)
	if err := a.writeCertificateRenewProgress("running", "正在检查证书续签条件…"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-16 * time.Minute)
	if err := os.Chtimes(a.certificateRenewStatusPath(), old, old); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), http.MethodGet, "/api/certificate/status", nil, login(t, a))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "长时间未完成") || strings.Contains(w.Body.String(), `"running":true`) {
		t.Fatalf("unexpected certificate status: %d %s", w.Code, w.Body.String())
	}
}
