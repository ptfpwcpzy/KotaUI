package app

import (
	"errors"

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
	a.settingsApplyErr = ""
	a.settingsApplyMu.Unlock()

	err := a.commitConfigMutation(fn)
	if err != nil {
		a.settingsApplyMu.Lock()
		a.settingsApplying = false
		a.settingsApplyMu.Unlock()
		return err
	}

	go func() {
		if err := a.restartManagedSingBox(); err != nil {
			a.settingsApplyMu.Lock()
			a.settingsApplyErr = "sing-box 核心重启失败：" + err.Error()
			a.settingsApplyMu.Unlock()
		}
		a.settingsApplyMu.Lock()
		a.settingsApplying = false
		a.settingsApplyMu.Unlock()
	}()
	return nil
}

func (a *App) settingsApplyInProgress() bool {
	a.settingsApplyMu.Lock()
	running := a.settingsApplying
	a.settingsApplyMu.Unlock()
	return running
}

// settingsApplyStatus 返回设置应用任务的状态与最近一次失败信息（供前端轮询）。
func (a *App) settingsApplyStatus() (applying bool, applyErr string) {
	a.settingsApplyMu.Lock()
	defer a.settingsApplyMu.Unlock()
	return a.settingsApplying, a.settingsApplyErr
}
