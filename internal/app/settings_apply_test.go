package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSettingsApplyStatusReportsFailure(t *testing.T) {
	a := testApp(t)
	if err := a.writeSettingsApplyProgress("failed", "核心未恢复"); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), http.MethodGet, "/api/settings/status", nil, login(t, a))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Running bool   `json:"running"`
		State   string `json:"state"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Running || response.State != "failed" || response.Message != "核心未恢复" {
		t.Fatalf("status response = %#v", response)
	}
}

func TestSettingsApplyStatusEndsOrphanedTask(t *testing.T) {
	a := testApp(t)
	if err := a.writeSettingsApplyProgress("running", "正在重启核心"); err != nil {
		t.Fatal(err)
	}
	w := request(t, a.Handler(), http.MethodGet, "/api/settings/status", nil, login(t, a))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "设置应用任务已停止") {
		t.Fatalf("orphan status = %d: %s", w.Code, w.Body.String())
	}
}
