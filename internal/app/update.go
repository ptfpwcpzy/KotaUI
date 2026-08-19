package app

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) updatePanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if a.readUpdateState() == "running" && systemdAvailable() && !updateServiceActive() && a.updateStateStale() {
		a.finishUpdate("failed", "上一更新任务已停止，请查看更新日志后重新尝试。")
	}
	a.updateMu.Lock()
	if a.updateRunning {
		a.updateMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "更新任务正在执行，请稍候"})
		return
	}
	a.updateRunning = true
	a.updateMessage = "正在准备更新…"
	a.updateMu.Unlock()

	if err := os.MkdirAll(a.runtime.DataDir, 0700); err != nil {
		a.finishUpdate("failed", "无法创建更新状态目录")
		serverError(w, err)
		return
	}
	if err := a.writeUpdateState("running"); err != nil {
		a.finishUpdate("failed", "无法记录更新状态")
		serverError(w, err)
		return
	}

	if systemdAvailable() {
		_ = exec.Command("systemctl", "reset-failed", "kota-update.service").Run()
		if output, err := exec.Command("systemctl", "start", "--no-block", "kota-update.service").CombinedOutput(); err != nil {
			a.finishUpdate("failed", "无法启动独立更新服务")
			serverError(w, errors.New("无法启动独立更新服务："+strings.TrimSpace(string(output))))
			return
		}
		a.setUpdateMessage("正在下载、构建并替换面板与核心，请勿关闭页面。")
		writeJSON(w, http.StatusAccepted, map[string]string{"message": "已启动独立更新任务，完成后会自动重启服务"})
		return
	}

	logFile, err := os.OpenFile(filepath.Join(a.runtime.DataDir, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		a.finishUpdate("failed", "无法写入更新日志")
		serverError(w, err)
		return
	}
	_, _ = logFile.WriteString("\n[" + time.Now().Format(time.RFC3339) + "] 面板请求更新\n")
	cmd := exec.Command("/usr/local/bin/kota", "update", "--yes")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		a.finishUpdate("failed", "无法启动更新脚本")
		serverError(w, err)
		return
	}
	a.setUpdateMessage("正在下载、构建并替换面板与核心，请勿关闭页面。")
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		if err != nil {
			a.finishUpdate("failed", "更新失败，请查看更新日志。")
			return
		}
		a.finishUpdate("success", "更新完成，服务正在恢复。")
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "已开始更新，完成后会自动重启服务"})
}

func (a *App) updateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	state, progress := a.readUpdateProgress()
	if state == "running" && systemdAvailable() && !updateServiceActive() && a.updateStateStale() {
		state = "failed"
		progress = "更新任务已停止，请查看更新日志后重新尝试。"
		_ = a.writeUpdateProgress(state, progress)
	}
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	switch state {
	case "running":
		a.updateRunning = true
		if progress != "" {
			a.updateMessage = progress
		} else if a.updateMessage == "" {
			a.updateMessage = "正在下载、构建并替换面板与核心，请勿关闭页面。"
		}
	case "success":
		a.updateRunning = false
		if progress != "" {
			a.updateMessage = progress
		} else {
			a.updateMessage = "更新完成，服务正在恢复。"
		}
	case "failed":
		a.updateRunning = false
		if progress != "" {
			a.updateMessage = progress
		} else {
			a.updateMessage = "更新失败，请查看更新日志。"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": a.updateRunning, "message": a.updateMessage})
}

func (a *App) updateStatePath() string {
	return filepath.Join(a.runtime.DataDir, "update.status")
}

func (a *App) updateStateStale() bool {
	info, err := os.Stat(a.updateStatePath())
	return err == nil && time.Since(info.ModTime()) > 15*time.Second
}

func (a *App) writeUpdateState(state string) error {
	return a.writeUpdateProgress(state, "")
}

func (a *App) readUpdateState() string {
	state, _ := a.readUpdateProgress()
	return state
}

func (a *App) writeUpdateProgress(state, message string) error {
	body := state + "\n"
	if message != "" {
		body += strings.TrimSpace(message) + "\n"
	}
	return os.WriteFile(a.updateStatePath(), []byte(body), 0600)
}

func (a *App) readUpdateProgress() (string, string) {
	body, err := os.ReadFile(a.updateStatePath())
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(body)), "\n", 2)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (a *App) setUpdateMessage(message string) {
	a.updateMu.Lock()
	a.updateMessage = message
	a.updateMu.Unlock()
}

func (a *App) finishUpdate(state, message string) {
	_ = a.writeUpdateProgress(state, message)
	a.updateMu.Lock()
	a.updateRunning = false
	a.updateMessage = message
	a.updateMu.Unlock()
}

func updateServiceActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "kota-update.service").Run() == nil
}
