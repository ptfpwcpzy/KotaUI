package app

import (
	"errors"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func (a *App) mutate(fn func(*config.State) error) error {
	if a.updateIsRunning() {
		return errors.New("面板更新正在执行，暂不能修改配置")
	}
	if a.settingsApplyInProgress() {
		return errors.New("设置正在应用，暂不能修改配置")
	}
	if err := a.commitConfigMutation(fn); err != nil {
		return err
	}
	return a.restartManagedSingBox()
}

// persistStateOnly persists a state mutation (regenerating the sing-box
// config file) without restarting the core. Use it for bookkeeping that
// never affects the generated core config, e.g. the daily usage rollover,
// so routine accounting doesn't drop active connections.

// persistStateOnly persists a state mutation (regenerating the sing-box
// config file) without restarting the core. Use it for bookkeeping that
// never affects the generated core config, e.g. the daily usage rollover,
// so routine accounting doesn't drop active connections.
func (a *App) persistStateOnly(fn func(*config.State) error) error {
	if a.updateIsRunning() {
		return errors.New("面板更新正在执行，暂不能修改配置")
	}
	if a.settingsApplyInProgress() {
		return errors.New("设置正在应用，暂不能修改配置")
	}
	return a.commitConfigMutation(fn)
}
