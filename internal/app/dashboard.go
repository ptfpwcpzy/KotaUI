package app

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

type certificateInfo struct {
	Present bool   `json:"present"`
	Valid   bool   `json:"valid"`
	Days    int    `json:"days"`
	Message string `json:"message"`
}

type healthHint struct {
	Level  string `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Target string `json:"target"`
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

func dashboardHints(state config.State, certificate certificateInfo, services []map[string]any) []healthHint {
	hints := make([]healthHint, 0, 4)
	for _, service := range services {
		running, _ := service["running"].(bool)
		if !running {
			hints = append(hints, healthHint{Level: "danger", Title: service["name"].(string) + " 未运行", Detail: "请检查服务状态后重新启动。", Target: "settings"})
		}
	}
	if !certificate.Valid {
		hints = append(hints, healthHint{Level: "danger", Title: "证书不可用", Detail: certificate.Message, Target: "settings"})
	} else if certificate.Days <= 7 {
		hints = append(hints, healthHint{Level: "warning", Title: "证书即将到期", Detail: fmt.Sprintf("剩余 %d 天，自动续签会定期执行。", certificate.Days), Target: "settings"})
	}
	now := time.Now()
	for _, client := range state.Clients {
		if len(hints) >= 5 {
			break
		}
		if client.TotalLimitBytes > 0 && client.UsedBytes*100 >= client.TotalLimitBytes*80 {
			hints = append(hints, healthHint{Level: "warning", Title: client.Username + " 总流量接近上限", Detail: "已使用 " + formatBytes(client.UsedBytes) + "。", Target: "clients"})
			continue
		}
		if client.MonthlyLimitBytes > 0 && client.MonthlyUsedBytes*100 >= client.MonthlyLimitBytes*80 {
			hints = append(hints, healthHint{Level: "warning", Title: client.Username + " 本月流量接近上限", Detail: "已使用 " + formatBytes(client.MonthlyUsedBytes) + "。", Target: "clients"})
			continue
		}
		if client.ExpiresAt != "" {
			if expires, err := time.Parse("2006-01-02", client.ExpiresAt); err == nil {
				days := int(expires.Sub(now).Hours() / 24)
				if days >= 0 && days <= 3 {
					hints = append(hints, healthHint{Level: "warning", Title: client.Username + " 即将到期", Detail: fmt.Sprintf("剩余 %d 天。", days), Target: "clients"})
				}
			}
		}
	}
	return hints
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(value)/1024)
	}
	if value < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(value)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(value)/(1024*1024*1024))
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
