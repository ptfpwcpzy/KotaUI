package app

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	a.updateMu.Unlock()

	if err := os.MkdirAll(a.runtime.DataDir, 0700); err != nil {
		a.finishUpdate()
		serverError(w, err)
		return
	}
	logFile, err := os.OpenFile(filepath.Join(a.runtime.DataDir, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		a.finishUpdate()
		serverError(w, err)
		return
	}
	_, _ = logFile.WriteString("\n[" + time.Now().Format(time.RFC3339) + "] 面板请求更新\n")
	cmd := exec.Command("/usr/local/bin/kota", "update", "--yes")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		a.finishUpdate()
		serverError(w, err)
		return
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		a.finishUpdate()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "已开始更新，面板将在服务重启后自动恢复"})
}

func (a *App) finishUpdate() {
	a.updateMu.Lock()
	a.updateRunning = false
	a.updateMu.Unlock()
}
