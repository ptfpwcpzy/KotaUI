package app

import (
	"fmt"
	"time"
)

const serviceRecoveryTimeout = 12 * time.Second

func waitForServiceRunning(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if serviceRunning(name) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s 重启后未在 %s 内恢复运行", name, timeout.Round(time.Second))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (a *App) controlManagedSingBox(action string) error {
	if !a.runtime.ManageSingBox || !filePresent(a.runtime.SingBoxBin) {
		return nil
	}
	a.coreReloadMu.Lock()
	defer a.coreReloadMu.Unlock()
	if err := serviceCommand("kotaui-singbox", action).Run(); err != nil {
		return fmt.Errorf("sing-box 核心%s失败：%w", action, err)
	}
	return waitForServiceRunning("kotaui-singbox", serviceRecoveryTimeout)
}

func (a *App) restartManagedSingBox() error {
	return a.controlManagedSingBox("restart")
}
