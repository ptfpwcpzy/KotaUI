package app

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (a *App) renewCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := os.MkdirAll(a.runtime.DataDir, 0700); err != nil {
		serverError(w, err)
		return
	}
	logFile, err := os.OpenFile(filepath.Join(a.runtime.DataDir, "cert-renew.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		serverError(w, err)
		return
	}
	_, _ = logFile.WriteString("\n[" + time.Now().Format(time.RFC3339) + "] 面板请求续签检查\n")
	cmd := exec.Command("/usr/local/bin/kota-cert-renew")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		serverError(w, err)
		return
	}
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"message": "已开始证书续签检查，完成后会自动重载服务"})
}
