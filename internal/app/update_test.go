package app

import (
	"net/http"
	"strings"
	"testing"
)

// 中-2 回归：systemd 路径下更新由外部服务完成，面板侧从不调用
// finishUpdateForRun。更新失败（面板没重启）后，内存里的 updateRunning
// 若不清掉，所有配置修改会被长期阻塞，直到有人打开更新状态页或
// 重启面板。以下用例模拟“外部服务已把终态写进文件、面板内存标志
// 还在”的状态，且全程不 GET /api/update/status。

func TestUpdateIsRunningClearsStaleFlagAfterFailedUpdate(t *testing.T) {
	a := testApp(t)
	a.updateRunning = true
	a.updateRunID = "111"
	if err := a.writeUpdateProgressForRun("111", "failed", "更新失败，请查看更新日志。"); err != nil {
		t.Fatal(err)
	}
	if a.updateIsRunning() {
		t.Fatal("updateIsRunning() = true after failed update, want false")
	}
	a.updateMu.Lock()
	stillRunning := a.updateRunning
	a.updateMu.Unlock()
	if stillRunning {
		t.Fatal("updateRunning flag was not cleared after failed update")
	}
	if a.updateIsRunning() {
		t.Fatal("updateIsRunning() = true on second call after failed update, want false")
	}
}

func TestUpdateIsRunningClearsStaleFlagAfterSuccessfulUpdate(t *testing.T) {
	a := testApp(t)
	a.updateRunning = true
	a.updateRunID = "111"
	if err := a.writeUpdateProgressForRun("111", "success", "更新完成，面板与核心已恢复。"); err != nil {
		t.Fatal(err)
	}
	if a.updateIsRunning() {
		t.Fatal("updateIsRunning() = true after successful update, want false")
	}
	a.updateMu.Lock()
	stillRunning := a.updateRunning
	a.updateMu.Unlock()
	if stillRunning {
		t.Fatal("updateRunning flag was not cleared after successful update")
	}
}

func TestUpdateIsRunningClearsStaleFlagWhenTerminalStateHasNoRunID(t *testing.T) {
	a := testApp(t)
	a.updateRunning = true
	a.updateRunID = "111"
	// 外部脚本在拿不到 runId 时会写空 runId 的终态，同样要清标志。
	if err := a.writeUpdateProgressForRun("", "failed", "更新失败，请查看更新日志。"); err != nil {
		t.Fatal(err)
	}
	if a.updateIsRunning() {
		t.Fatal("updateIsRunning() = true after failed update without runId, want false")
	}
}

func TestUpdateIsRunningKeepsFlagForNewRunWhenFileHoldsOldTerminalState(t *testing.T) {
	a := testApp(t)
	// 新一轮更新刚置位、文件还没来得及写成 running：文件里还是
	// 上一轮的终态，不能把新一轮的标志清掉。
	a.updateRunning = true
	a.updateRunID = "222"
	if err := a.writeUpdateProgressForRun("111", "failed", "更新失败，请查看更新日志。"); err != nil {
		t.Fatal(err)
	}
	if !a.updateIsRunning() {
		t.Fatal("updateIsRunning() = false for a new run, want true")
	}
	a.updateMu.Lock()
	stillRunning := a.updateRunning
	a.updateMu.Unlock()
	if !stillRunning {
		t.Fatal("updateRunning flag was cleared for a new run in setup")
	}
}

func TestUpdateIsRunningKeepsFlagWithoutStateFile(t *testing.T) {
	a := testApp(t)
	// 还没有状态文件时（如更新刚置位、文件还没写出来），仍以内存为准。
	a.updateRunning = true
	a.updateRunID = "111"
	if !a.updateIsRunning() {
		t.Fatal("updateIsRunning() = false without state file, want true")
	}
}

func TestUpdateIsRunningKeepsFlagWhileUpdateRunning(t *testing.T) {
	a := testApp(t)
	a.updateRunning = true
	a.updateRunID = "111"
	if err := a.writeUpdateProgressForRun("111", "running", "正在构建 KotaUI 面板程序…"); err != nil {
		t.Fatal(err)
	}
	if !a.updateIsRunning() {
		t.Fatal("updateIsRunning() = false while update is running, want true")
	}
}

func TestSettingsSaveUnblockedAfterFailedUpdate(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	// 对照：内存标志在、无终态文件时，设置保存应被挡住。
	a.updateRunning = true
	a.updateRunID = "111"
	w := request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]any{
		"realityCandidates": []map[string]any{{"host": "www.example.com", "port": 443}},
	}, cookie)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "维护任务正在执行") {
		t.Fatalf("blocked save: status = %d, body = %s", w.Code, w.Body.String())
	}

	// 外部服务把失败终态写进文件后（面板没重启、没人打开过状态页），
	// 同样的保存不再被挡住。
	if err := a.writeUpdateProgressForRun("111", "failed", "更新失败，请查看更新日志。"); err != nil {
		t.Fatal(err)
	}
	w = request(t, a.Handler(), http.MethodPut, "/api/settings", map[string]any{
		"realityCandidates": []map[string]any{{"host": "www.example.com", "port": 443}},
	}, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("save after failed update: status = %d, body = %s", w.Code, w.Body.String())
	}
}
