package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func (a *App) startSettingsApply(fn func(*config.State) error) error {
	if a.updateIsRunning() {
		return errors.New("面板更新正在执行，暂不能修改配置")
	}
	a.settingsApplyMu.Lock()
	if a.settingsApplying {
		a.settingsApplyMu.Unlock()
		return errors.New("设置正在应用，正在等待 sing-box 核心恢复")
	}
	a.settingsApplying = true
	a.settingsApplyMu.Unlock()

	finish := func(state, message string) {
		_ = a.writeSettingsApplyProgress(state, message)
		a.settingsApplyMu.Lock()
		a.settingsApplying = false
		a.settingsApplyMu.Unlock()
	}
	if err := a.writeSettingsApplyProgress("running", "正在保存设置并生成 sing-box 配置…"); err != nil {
		finish("failed", "无法记录设置应用状态。")
		return err
	}

	a.mu.Lock()
	err := a.store.UpdateWith(fn, func(candidate config.State) error {
		return writeAndValidateConfig(candidate, a.runtime)
	})
	a.mu.Unlock()
	if err != nil {
		finish("failed", "设置保存失败："+err.Error())
		return err
	}

	go func() {
		_ = a.writeSettingsApplyProgress("running", "设置已保存，正在重启 sing-box 核心并检查恢复…")
		if err := a.restartManagedSingBox(); err != nil {
			finish("failed", "设置已保存，但 sing-box 核心未恢复："+err.Error())
			return
		}
		finish("success", "设置已保存，sing-box 核心已恢复，出站网络策略已生效。")
	}()
	return nil
}

func (a *App) settingsApplyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		methodNotAllowed(w)
		return
	}
	state, message := a.readSettingsApplyProgress()
	a.settingsApplyMu.Lock()
	running := a.settingsApplying
	a.settingsApplyMu.Unlock()
	if state == "running" && !running {
		state = "failed"
		message = "设置应用任务已停止，请重新保存设置。"
		_ = a.writeSettingsApplyProgress(state, message)
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": state == "running" && running, "state": state, "message": message})
}

func (a *App) settingsApplyStatePath() string {
	return filepath.Join(a.runtime.DataDir, "settings-apply.status")
}

func (a *App) writeSettingsApplyProgress(state, message string) error {
	path := a.settingsApplyStatePath()
	temporary := path + ".tmp"
	body := strings.TrimSpace(state) + "\n" + strings.TrimSpace(message) + "\n"
	if err := os.WriteFile(temporary, []byte(body), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (a *App) readSettingsApplyProgress() (string, string) {
	body, err := os.ReadFile(a.settingsApplyStatePath())
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(body)), "\n", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (a *App) settingsApplyInProgress() bool {
	a.settingsApplyMu.Lock()
	running := a.settingsApplying
	a.settingsApplyMu.Unlock()
	return running
}
