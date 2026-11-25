package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

// syncSpeedLimits 按当前状态同步 tc 限速规则：被限速用户的在线 IP
// （从核心 /connections 里按用户归属）按各自的速度限速。
// 没人限速且无规则时直接返回，不碰 tc、不查连接。
// live 是查到的各用户在线 IP，desired 是期望的 ip->KB/s；
// err 非空表示本次同步没做成（核心没起、tc 不可用等），旧规则保持不动。
func (a *App) syncSpeedLimits() (live map[string][]string, desired map[string]int, err error) {
	snapshot := a.store.Snapshot()
	limited := map[string]int{}
	for _, client := range snapshot.Clients {
		if kbps := client.EffectiveSpeedLimitKBps(); kbps > 0 {
			limited[client.Username] = kbps
		}
	}
	a.shaperMu.Lock()
	hasApplied := a.shaper.HasApplied()
	a.shaperMu.Unlock()
	if len(limited) == 0 && !hasApplied {
		return map[string][]string{}, map[string]int{}, nil
	}
	live, err = a.liveUserIPs()
	if err != nil {
		// 核心没起或查不到连接：保持旧规则，下次再试。
		return nil, nil, err
	}
	desired = map[string]int{}
	for username, kbps := range limited {
		for _, ip := range live[username] {
			if current, ok := desired[ip]; !ok || kbps < current {
				desired[ip] = kbps
			}
		}
	}
	a.shaperMu.Lock()
	syncErr := a.shaper.Sync(desired)
	if syncErr != nil {
		if syncErr.Error() != a.shaperLastErr {
			log.Printf("限速同步失败：%v", syncErr)
			a.shaperLastErr = syncErr.Error()
		}
	} else {
		a.shaperLastErr = ""
	}
	a.shaperMu.Unlock()
	if syncErr != nil {
		return live, desired, syncErr
	}
	return live, desired, nil
}

// liveUserIPs 查核心 clash_api 的活动连接，按用户归出各用户的来源 IP。
// 需要核心补丁 002（/connections 输出 user 字段）；旧核心没有 user 字段，
// 返回的 map 为空，限速保持关闭，等核心更新后自动生效。
func (a *App) liveUserIPs() (map[string][]string, error) {
	secret := proxy.ReadClashSecret(a.runtime.DataDir)
	if secret == "" {
		return nil, errors.New("clash 密钥未就绪")
	}
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/connections", proxy.ClashPort(a.runtime.StatsPort)), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询活动连接失败：%s", response.Status)
	}
	var snapshot struct {
		Connections []liveConnection `json:"connections"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		return nil, err
	}
	return parseLiveConnections(snapshot.Connections), nil
}

// liveConnection 是 /connections 返回的一条活动连接（只要归属用户需要的字段）。
type liveConnection struct {
	Metadata struct {
		User     string `json:"user"`
		SourceIP string `json:"sourceIP"`
	} `json:"metadata"`
}

// parseLiveConnections 把 /connections 的连接列表按用户归出各用户的来源 IP
// （去重、跳过空用户与非法 IP）。旧核心没有 user 字段时返回空 map。
func parseLiveConnections(connections []liveConnection) map[string][]string {
	ips := map[string][]string{}
	seen := map[string]map[string]bool{}
	for _, connection := range connections {
		user := strings.TrimSpace(connection.Metadata.User)
		ip := strings.TrimSpace(connection.Metadata.SourceIP)
		if user == "" || ip == "" || net.ParseIP(ip) == nil {
			continue
		}
		if seen[user] == nil {
			seen[user] = map[string]bool{}
		}
		if seen[user][ip] {
			continue
		}
		seen[user][ip] = true
		ips[user] = append(ips[user], ip)
	}
	return ips
}
