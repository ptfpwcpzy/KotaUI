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
	a.settingsApplyMu.Unlock()

	err := a.commitConfigMutation(fn)
	if err != nil {
		a.settingsApplyMu.Lock()
		a.settingsApplying = false
		a.settingsApplyMu.Unlock()
		return err
	}

	go func() {
		_ = a.restartManagedSingBox()
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
