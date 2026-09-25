package app

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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

func (a *App) removeUnusedDistroSingBox() {
	bin := a.runtime.SingBoxBin
	if filepath.Base(bin) != "sing-box-v2ray" || !filePresent(bin) {
		return
	}
	if exec.Command("apk", "info", "-e", "sing-box").Run() == nil {
		if err := exec.Command("apk", "del", "--no-cache", "sing-box").Run(); err != nil {
			log.Printf("未能移除系统 sing-box 包：%v", err)
		}
		return
	}
	if exec.Command("dpkg", "-s", "sing-box").Run() == nil {
		cmd := exec.Command("apt-get", "remove", "-y", "sing-box")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		if err := cmd.Run(); err != nil {
			log.Printf("未能移除系统 sing-box 包：%v", err)
		}
	}
}
