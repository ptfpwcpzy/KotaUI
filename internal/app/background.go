package app

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) startBackgroundTasks() {
	a.backgroundOnce.Do(func() {
		go a.backgroundSchedulerLoop()
		go a.removeUnusedDistroSingBox()
	})
}

// nextRelayRuleSetMarker 推进规则集更新标记状态：空标记不推进；第一次见到标记
// 只记基线不触发重启；标记变化才触发重启。

// nextRelayRuleSetMarker 推进规则集更新标记状态：空标记不推进；第一次见到标记
// 只记基线不触发重启；标记变化才触发重启。
func nextRelayRuleSetMarker(applied, marker string) (string, bool) {
	if marker == "" {
		return applied, false
	}
	if applied == "" {
		return marker, false
	}
	if marker == applied {
		return applied, false
	}
	return marker, true
}

// backgroundSchedulerLoop 是面板唯一的常驻定时循环：15 秒一拍，每拍同步流量；
// 每 4 拍（60 秒）盯一次中转规则集标记，每 8 拍（120 秒）测一次速；两者的拍次
// 错开，互不撞到同一拍。测速单独起 goroutine 跑（带重叠保护），不阻塞流量同步。

// backgroundSchedulerLoop 是面板唯一的常驻定时循环：15 秒一拍，每拍同步流量；
// 每 4 拍（60 秒）盯一次中转规则集标记，每 8 拍（120 秒）测一次速；两者的拍次
// 错开，互不撞到同一拍。测速单独起 goroutine 跑（带重叠保护），不阻塞流量同步。
func (a *App) backgroundSchedulerLoop() {
	a.syncTrafficStep()
	a.dispatchPingChecks()
	relayApplied := ""
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for tick := 1; ; tick++ {
		<-ticker.C
		a.syncTrafficStep()
		a.syncSpeedLimits()
		if tick%4 == 2 {
			relayApplied = a.relayRuleSetWatchStep(relayApplied)
		}
		if tick%8 == 4 {
			a.dispatchPingChecks()
		}
	}
}

func (a *App) syncTrafficStep() {
	a.syncTraffic()
	a.recordDailyUsage(totalClientUsage(a.store.Snapshot()), time.Now())
}

// relayRuleSetWatchStep 是单拍的中转规则集盯梢：只在中转分类模式下盯
// kota-autoupdate 写的更新标记，标记变化时重启核心；第一次见到标记只记基线不重启
// （刚启动的核心本来就读到了最新的规则集文件）。

// relayRuleSetWatchStep 是单拍的中转规则集盯梢：只在中转分类模式下盯
// kota-autoupdate 写的更新标记，标记变化时重启核心；第一次见到标记只记基线不重启
// （刚启动的核心本来就读到了最新的规则集文件）。
func (a *App) relayRuleSetWatchStep(applied string) string {
	state := a.store.Snapshot()
	relay := state.Settings.Relay
	if !relay.Enabled || relay.Role != "transit" || relay.Mode != "categories" {
		return applied
	}
	body, err := os.ReadFile(filepath.Join(a.runtime.DataDir, "rule-sets", ".updated"))
	if err != nil {
		return applied
	}
	marker := strings.TrimSpace(string(body))
	next, changed := nextRelayRuleSetMarker(applied, marker)
	if !changed {
		return next
	}
	if err := a.restartManagedSingBox(); err != nil {
		log.Printf("中转规则集更新后核心重启失败：%v", err)
	}
	return next
}

// dispatchPingChecks 在后台测速：测速可能耗时（多目标 × 超时），不能阻塞流量
// 同步；重叠保护避免上一轮还没跑完又起一轮。

// dispatchPingChecks 在后台测速：测速可能耗时（多目标 × 超时），不能阻塞流量
// 同步；重叠保护避免上一轮还没跑完又起一轮。
func (a *App) dispatchPingChecks() {
	if !a.pingRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.pingRunning.Store(false)
		a.runPingChecks()
	}()
}
