package app

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type certificateInfo struct {
	Present bool   `json:"present"`
	Valid   bool   `json:"valid"`
	Days    int    `json:"days"`
	Message string `json:"message"`
}

func certificateStatus(certPath string) certificateInfo {
	body, err := os.ReadFile(certPath)
	if err != nil {
		return certificateInfo{Message: "未找到证书"}
	}
	block, _ := pem.Decode(body)
	if block == nil {
		return certificateInfo{Present: true, Message: "证书文件格式无效"}
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return certificateInfo{Present: true, Message: "无法读取证书"}
	}
	days := int(time.Until(certificate.NotAfter).Hours() / 24)
	if days < 0 {
		return certificateInfo{Present: true, Days: days, Message: "证书已过期"}
	}
	return certificateInfo{Present: true, Valid: true, Days: days, Message: "证书有效"}
}

func serviceRunning(name string) bool {
	if hasSystemd() {
		return exec.Command("systemctl", "is-active", "--quiet", name).Run() == nil
	}
	return exec.Command("rc-service", name, "status").Run() == nil
}

func (a *App) serviceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/"), "/")
	if len(parts) != 2 || (parts[0] != "panel" && parts[0] != "singbox") {
		badRequest(w, errors.New("无效服务操作"))
		return
	}
	if parts[1] != "restart" && parts[1] != "start" && parts[1] != "stop" {
		badRequest(w, errors.New("无效操作"))
		return
	}
	unit := "kotaui"
	if parts[0] == "singbox" {
		unit = "kotaui-singbox"
	}
	var command *exec.Cmd
	if hasSystemd() {
		command = exec.Command("systemctl", parts[1], unit)
	} else {
		command = exec.Command("rc-service", unit, parts[1])
	}
	if output, err := command.CombinedOutput(); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": strings.TrimSpace(string(output))})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "服务操作已执行"})
}

func hasSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	return true
}
