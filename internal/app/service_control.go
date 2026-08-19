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

func (a *App) restartManagedSingBox() error {
	if !a.runtime.ManageSingBox || !filePresent(a.runtime.SingBoxBin) {
		return nil
	}
	if err := serviceCommand("kotaui-singbox", "restart").Run(); err != nil {
		return fmt.Errorf("sing-box 核心重启失败：%w", err)
	}
	return waitForServiceRunning("kotaui-singbox", serviceRecoveryTimeout)
}
