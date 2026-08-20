package app

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) updatePanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if a.readUpdateState() == "running" && !a.updateIsRunning() {
		a.finishUpdate("failed", "上一更新任务未完成或已停止，请查看更新日志后重新尝试。")
	}
	if a.updateIsRunning() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "更新任务正在执行，请稍候"})
		return
	}
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	a.updateMu.Lock()
	if a.updateRunning {
		a.updateMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "更新任务正在执行，请稍候"})
		return
	}
	a.updateRunning = true
	a.updateRunID = runID
	a.updateMessage = "正在准备更新…"
	a.updateMu.Unlock()

	if err := os.MkdirAll(a.runtime.DataDir, 0700); err != nil {
		a.finishUpdateForRun(runID, "failed", "无法创建更新状态目录")
		serverError(w, err)
		return
	}
	if err := a.writeUpdateProgressForRun(runID, "running", "正在启动独立更新服务…"); err != nil {
		a.finishUpdateForRun(runID, "failed", "无法记录更新状态")
		serverError(w, err)
		return
	}
	if err := os.WriteFile(filepath.Join(a.runtime.DataDir, "update.context"), []byte("KOTAUI_UPDATE_RUN_ID="+runID+"\n"), 0600); err != nil {
		a.finishUpdateForRun(runID, "failed", "无法准备更新任务上下文")
		serverError(w, err)
		return
	}

	if systemdAvailable() {
		_ = exec.Command("systemctl", "reset-failed", "kota-update.service").Run()
		if output, err := exec.Command("systemctl", "start", "--no-block", "kota-update.service").CombinedOutput(); err != nil {
			a.finishUpdateForRun(runID, "failed", "无法启动独立更新服务")
			serverError(w, errors.New("无法启动独立更新服务："+strings.TrimSpace(string(output))))
			return
		}
		a.setUpdateMessage("更新服务已启动，正在等待任务上报阶段进度…")
		writeJSON(w, http.StatusAccepted, map[string]string{"runId": runID, "message": "更新任务已接受，正在后台下载和构建。"})
		return
	}

	logFile, err := os.OpenFile(filepath.Join(a.runtime.DataDir, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		a.finishUpdateForRun(runID, "failed", "无法写入更新日志")
		serverError(w, err)
		return
	}
	_, _ = logFile.WriteString("\n[" + time.Now().Format(time.RFC3339) + "] 面板请求更新 run=" + runID + "\n")
	cmd := exec.Command("/usr/local/bin/kota", "update", "--yes")
	cmd.Env = append(os.Environ(), "KOTAUI_PANEL_UPDATE=1", "KOTAUI_UPDATE_RUN_ID="+runID)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		a.finishUpdateForRun(runID, "failed", "无法启动更新脚本")
		serverError(w, err)
		return
	}
	a.setUpdateMessage("正在下载、构建并替换面板与核心，请勿关闭页面。")
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		if err != nil {
			a.finishUpdateForRun(runID, "failed", "更新失败，请查看更新日志。")
			return
		}
		a.finishUpdateForRun(runID, "success", "更新完成，服务正在恢复。")
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"runId": runID, "message": "更新任务已接受，正在后台下载和构建。"})
}

func (a *App) updateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	expectedRunID := strings.TrimSpace(r.URL.Query().Get("runId"))
	progress := a.readUpdateProgressDetail()
	if expectedRunID != "" && progress.RunID != "" && progress.RunID != expectedRunID {
		writeJSON(w, http.StatusOK, map[string]any{"running": false, "state": "superseded", "runId": progress.RunID, "message": "本次更新已被新的更新任务替代。"})
		return
	}
	if progress.State == "running" && !a.updateIsRunning() {
		progress.State = "failed"
		progress.Message = "更新任务已停止，请查看更新日志后重新尝试。"
		_ = a.writeUpdateProgressForRun(progress.RunID, progress.State, progress.Message)
	}
	if progress.State == "running" && strings.Contains(progress.Message, "正在启动独立更新服务") && a.updateStateOlderThan(time.Minute) {
		progress.State = "failed"
		progress.Message = "更新服务启动后未上报进度，请查看更新日志后重新尝试。"
		_ = a.writeUpdateProgressForRun(progress.RunID, progress.State, progress.Message)
	}
	a.updateMu.Lock()
	switch progress.State {
	case "running":
		a.updateRunning = true
		if progress.RunID != "" {
			a.updateRunID = progress.RunID
		}
		if progress.Message != "" {
			a.updateMessage = progress.Message
		}
	case "success", "failed":
		a.updateRunning = false
		if progress.Message != "" {
			a.updateMessage = progress.Message
		}
	}
	running, message, runID := a.updateRunning, a.updateMessage, a.updateRunID
	a.updateMu.Unlock()
	if progress.State == "" {
		progress.State = "idle"
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": running, "state": progress.State, "runId": firstNonEmpty(progress.RunID, runID), "message": message, "updatedAt": progress.UpdatedAt})
}

func (a *App) updateStatePath() string { return filepath.Join(a.runtime.DataDir, "update.status") }

func (a *App) updateStateStale() bool { return a.updateStateOlderThan(15 * time.Second) }

func (a *App) updateStateOlderThan(age time.Duration) bool {
	info, err := os.Stat(a.updateStatePath())
	return err == nil && time.Since(info.ModTime()) > age
}

func (a *App) writeUpdateState(state string) error { return a.writeUpdateProgress(state, "") }

func (a *App) readUpdateState() string { return a.readUpdateProgressDetail().State }

func (a *App) writeUpdateProgress(state, message string) error {
	a.updateMu.Lock()
	runID := a.updateRunID
	a.updateMu.Unlock()
	return a.writeUpdateProgressForRun(runID, state, message)
}

func (a *App) writeUpdateProgressForRun(runID, state, message string) error {
	return writeTaskProgress(a.updateStatePath(), runID, state, message)
}

func (a *App) readUpdateProgress() (string, string) {
	progress := a.readUpdateProgressDetail()
	return progress.State, progress.Message
}

func (a *App) readUpdateProgressDetail() taskProgress {
	return readTaskProgress(a.updateStatePath())
}

func (a *App) setUpdateMessage(message string) {
	a.updateMu.Lock()
	a.updateMessage = message
	a.updateMu.Unlock()
}

func (a *App) finishUpdate(state, message string) {
	a.updateMu.Lock()
	runID := a.updateRunID
	a.updateMu.Unlock()
	a.finishUpdateForRun(runID, state, message)
}

func (a *App) finishUpdateForRun(runID, state, message string) {
	_ = a.writeUpdateProgressForRun(runID, state, message)
	a.updateMu.Lock()
	a.updateRunning = false
	if runID != "" {
		a.updateRunID = runID
	}
	a.updateMessage = message
	a.updateMu.Unlock()
}

func (a *App) updateIsRunning() bool {
	a.updateMu.Lock()
	inMemory := a.updateRunning
	a.updateMu.Unlock()
	state := a.readUpdateState()
	if state != "running" {
		return inMemory
	}
	if systemdAvailable() {
		return updateServiceActive() || !a.updateStateStale()
	}
	return inMemory || !a.updateStateStale()
}

func updateServiceActive() bool {
	output, err := exec.Command("systemctl", "show", "kota-update.service", "--property=ActiveState", "--value").Output()
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(output)) {
	case "active", "activating", "reloading":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
