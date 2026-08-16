package app

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (a *App) logs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/logs/"), "/")
	if name != "panel" && name != "singbox" && name != "certificate" {
		badRequest(w, errors.New("无效日志类型"))
		return
	}
	content, err := a.readLogs(name)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "content": content})
}

func (a *App) readLogs(name string) (string, error) {
	if name == "certificate" {
		return tailFile(filepath.Join(a.runtime.DataDir, "cert-renew.log"), 100)
	}
	unit := "kotaui"
	if name == "singbox" {
		unit = "kotaui-singbox"
	}
	if hasSystemd() {
		output, err := exec.Command("journalctl", "-u", unit, "-n", "100", "--no-pager", "-o", "short-iso").CombinedOutput()
		if err != nil && len(output) == 0 {
			return "", err
		}
		return strings.TrimSpace(string(output)), nil
	}
	return tailFile(filepath.Join("/var/log", unit+".log"), 100)
}

func tailFile(file string, lines int) (string, error) {
	body, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return "暂无日志。", nil
	}
	if err != nil {
		return "", err
	}
	parts := bytes.Split(body, []byte("\n"))
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.TrimSpace(string(bytes.Join(parts, []byte("\n")))), nil
}
