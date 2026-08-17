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
	state := a.readUpdateState()
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	switch state {
	case "running":
		a.updateRunning = true
		if a.updateMessage == "" {
			a.updateMessage = "正在下载、构建并替换面板与核心，请勿关闭页面。"
		}
	case "success":
		a.updateRunning = false
		a.updateMessage = "更新完成，服务正在恢复。"
	case "failed":
		a.updateRunning = false
		a.updateMessage = "更新失败，请查看更新日志。"
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": a.updateRunning, "message": a.updateMessage})
}

func (a *App) updateStatePath() string {
	return filepath.Join(a.runtime.DataDir, "update.status")
}

func (a *App) writeUpdateState(state string) error {
	return os.WriteFile(a.updateStatePath(), []byte(state+"\n"), 0600)
}

func (a *App) readUpdateState() string {
	body, err := os.ReadFile(a.updateStatePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func (a *App) setUpdateMessage(message string) {
	a.updateMu.Lock()
	a.updateMessage = message
	a.updateMu.Unlock()
}

func (a *App) finishUpdate(state, message string) {
	_ = a.writeUpdateState(state)
	a.updateMu.Lock()
	a.updateRunning = false
	a.updateMessage = message
	a.updateMu.Unlock()
}
