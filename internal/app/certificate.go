package app

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) renewCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if a.updateIsRunning() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "面板更新正在执行，请等待更新完成后再检查证书"})
		return
	}
	if state, _ := a.readCertificateRenewProgress(); state == "running" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "证书续签检查正在执行，请稍候"})
		return
	}
	if err := os.MkdirAll(a.runtime.DataDir, 0700); err != nil {
		serverError(w, err)
		return
	}
	if err := a.writeCertificateRenewProgress("running", "正在检查证书续签条件…"); err != nil {
		serverError(w, err)
		return
	}
	logFile, err := os.OpenFile(filepath.Join(a.runtime.DataDir, "cert-renew.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		_ = a.writeCertificateRenewProgress("failed", "无法写入证书续签日志。")
		serverError(w, err)
		return
	}
	_, _ = logFile.WriteString("\n[" + time.Now().Format(time.RFC3339) + "] 面板请求续签检查\n")
	cmd := exec.Command("/usr/local/bin/kota-cert-renew")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		_ = a.writeCertificateRenewProgress("failed", "无法启动证书续签检查。")
		serverError(w, err)
		return
	}
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		if err != nil {
			_ = a.writeCertificateRenewProgress("failed", "证书续签检查失败，请查看续签日志。")
			return
		}
		_ = a.writeCertificateRenewProgress("success", "证书续签检查完成；若证书已更新，面板与核心已自动重载。")
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "已开始检查证书续签条件，完成后会显示结果"})
}

func (a *App) certificateRenewStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	state, message := a.readCertificateRenewProgress()
	writeJSON(w, http.StatusOK, map[string]any{"running": state == "running", "message": message})
}

func (a *App) certificateRenewStatusPath() string {
	return filepath.Join(a.runtime.DataDir, "certificate-renew.status")
}

func (a *App) writeCertificateRenewProgress(state, message string) error {
	path := a.certificateRenewStatusPath()
	temporary := path + ".tmp"
	body := strings.TrimSpace(state) + "\n" + strings.TrimSpace(message) + "\n"
	if err := os.WriteFile(temporary, []byte(body), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (a *App) readCertificateRenewProgress() (string, string) {
	body, err := os.ReadFile(a.certificateRenewStatusPath())
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(body)), "\n", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
