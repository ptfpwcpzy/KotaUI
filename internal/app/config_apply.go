package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

// writeAndValidateConfig stages the generated sing-box config, validates it when
// the managed core is available, then atomically replaces the live config.
func writeAndValidateConfig(state config.State, runtime config.Runtime) error {
	if runtime.SingBoxConfig == "" {
		return fmt.Errorf("sing-box 配置路径不能为空")
	}
	if err := os.MkdirAll(filepath.Dir(runtime.SingBoxConfig), 0700); err != nil {
		return err
	}
	staging := filepath.Join(filepath.Dir(runtime.SingBoxConfig), ".config-"+config.NewID()+".json")
	defer os.Remove(staging)
	stagedRuntime := runtime
	stagedRuntime.SingBoxConfig = staging
	if err := proxy.Write(state, stagedRuntime); err != nil {
		return err
	}
	if filePresent(runtime.SingBoxBin) {
		output, err := exec.Command(runtime.SingBoxBin, "check", "-c", staging).CombinedOutput()
		if err != nil {
			return fmt.Errorf("sing-box 配置校验失败：%s", strings.TrimSpace(string(output)))
		}
	}
	return os.Rename(staging, runtime.SingBoxConfig)
}

// commitConfigMutation serializes state persistence with generated-config
// replacement. Callers choose whether the subsequent core restart is
// synchronous (interactive resource changes) or background (maintenance).
func (a *App) commitConfigMutation(fn func(*config.State) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.store.UpdateWith(fn, func(candidate config.State) error {
		return writeAndValidateConfig(candidate, a.runtime)
	})
}
