package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.store.Snapshot().Settings)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var incoming settingsUpdateRequest
	if err := decodeJSON(r, &incoming); err != nil {
		badRequest(w, err)
		return
	}
	if incoming.RealityCandidates != nil {
		candidates, err := normalizeRealityCandidates(incoming.RealityCandidates)
		if err != nil {
			badRequest(w, err)
			return
		}
		incoming.RealityCandidates = candidates
	}
	if incoming.OutboundStrategy != "" {
		strategy, err := normalizeOutboundStrategy(incoming.OutboundStrategy)
		if err != nil {
			badRequest(w, err)
			return
		}
		incoming.OutboundStrategy = strategy
	}
	if incoming.BlockedDomains != nil {
		domains, err := normalizeBlockedDomains(incoming.BlockedDomains)
		if err != nil {
			badRequest(w, err)
			return
		}
		incoming.BlockedDomains = domains
	}
	if incoming.Relay != nil {
		relay, err := a.normalizeRelay(*incoming.Relay)
		if err != nil {
			badRequest(w, err)
			return
		}
		incoming.Relay = &relay
	}
	current := a.store.Snapshot().Settings
	dashboardChanged := dashboardSettingsChanged(current, incoming)
	outboundChanged := incoming.OutboundStrategy != "" && incoming.OutboundStrategy != current.OutboundStrategy
	candidatesChanged := incoming.RealityCandidates != nil && !reflect.DeepEqual(incoming.RealityCandidates, current.RealityCandidates)
	blockedDomainsChanged := incoming.BlockedDomains != nil && !reflect.DeepEqual(incoming.BlockedDomains, current.BlockedDomains)
	blockBitTorrentChanged := incoming.BlockBitTorrent != nil && *incoming.BlockBitTorrent != current.BlockBitTorrent
	relayChanged := incoming.Relay != nil && !reflect.DeepEqual(*incoming.Relay, current.Relay)
	siteDetailChanged := incoming.SiteDetailEnabled != nil && *incoming.SiteDetailEnabled != current.SiteDetailEnabled
	requiresCoreApply := outboundChanged || blockedDomainsChanged || blockBitTorrentChanged || relayChanged
	if !requiresCoreApply {
		if !candidatesChanged && !dashboardChanged && !siteDetailChanged {
			writeJSON(w, http.StatusOK, map[string]any{"settings": current, "applied": true, "message": "设置没有变化，当前配置已生效。"})
			return
		}
		if a.updateIsRunning() || a.settingsApplyInProgress() {
			serverError(w, errors.New("维护任务正在执行，暂不能保存设置"))
			return
		}
		a.mu.Lock()
		err := a.store.Update(func(s *config.State) error {
			if incoming.RealityCandidates != nil {
				s.Settings.RealityCandidates = incoming.RealityCandidates
			}
			if incoming.SiteDetailEnabled != nil {
				s.Settings.SiteDetailEnabled = *incoming.SiteDetailEnabled
			}
			applyDashboardSettings(&s.Settings, incoming)
			return nil
		})
		a.mu.Unlock()
		if err != nil {
			serverError(w, err)
			return
		}
		message := "仪表盘设置已保存。"
		if candidatesChanged {
			message = "设置已保存，不需要重启 sing-box 核心。"
		}
		writeJSON(w, http.StatusOK, map[string]any{"settings": a.store.Snapshot().Settings, "applied": true, "message": message})
		return
	}
	err := a.startSettingsApply(func(s *config.State) error {
		if incoming.RealityCandidates != nil {
			s.Settings.RealityCandidates = incoming.RealityCandidates
		}
		if incoming.OutboundStrategy != "" {
			s.Settings.OutboundStrategy = incoming.OutboundStrategy
		}
		if incoming.BlockedDomains != nil {
			s.Settings.BlockedDomains = incoming.BlockedDomains
		}
		if incoming.BlockBitTorrent != nil {
			s.Settings.BlockBitTorrent = *incoming.BlockBitTorrent
		}
		if incoming.Relay != nil {
			s.Settings.Relay = *incoming.Relay
		}
		applyDashboardSettings(&s.Settings, incoming)
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	settings := a.store.Snapshot().Settings
	writeJSON(w, http.StatusAccepted, map[string]any{"settings": settings, "applied": false, "message": "访问控制设置已保存，正在重启 sing-box 核心并检查恢复。"})
}

// settingsApplyStatusHandler 供前端轮询设置应用任务：是否还在执行、最近一次是否失败。

// settingsApplyStatusHandler 供前端轮询设置应用任务：是否还在执行、最近一次是否失败。
func (a *App) settingsApplyStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	applying, applyErr := a.settingsApplyStatus()
	writeJSON(w, http.StatusOK, map[string]any{"applying": applying, "error": applyErr})
}

type settingsUpdateRequest struct {
	RealityCandidates []config.RealityCandidate `json:"realityCandidates"`
	OutboundStrategy  string                    `json:"outboundStrategy"`
	BlockedDomains    []string                  `json:"blockedDomains"`
	BlockBitTorrent   *bool                     `json:"blockBitTorrent"`
	SiteDetailEnabled *bool                     `json:"siteDetailEnabled"`
	MonthlyQuotaGB    *int                      `json:"monthlyQuotaGB"`
	BandwidthMbps     *int                      `json:"bandwidthMbps"`
	TrafficDirection  *string                   `json:"trafficDirection"`
	Relay             *config.RelaySettings     `json:"relay"`
}

func dashboardQuota(v int) int {
	if v < 1 {
		return 1000
	}
	if v > 1000000 {
		return 1000000
	}
	return v
}

func dashboardBandwidth(v int) int {
	if v < 1 {
		return 500
	}
	if v > 100000 {
		return 100000
	}
	return v
}

func dashboardDirection(v string) string {
	if strings.TrimSpace(v) == "down" {
		return "down"
	}
	return "both"
}

func applyDashboardSettings(s *config.Settings, incoming settingsUpdateRequest) {
	if incoming.MonthlyQuotaGB != nil {
		s.MonthlyQuotaGB = dashboardQuota(*incoming.MonthlyQuotaGB)
	}
	if incoming.BandwidthMbps != nil {
		s.BandwidthMbps = dashboardBandwidth(*incoming.BandwidthMbps)
	}
	if incoming.TrafficDirection != nil {
		s.TrafficDirection = dashboardDirection(*incoming.TrafficDirection)
	}
}

func dashboardSettingsChanged(current config.Settings, incoming settingsUpdateRequest) bool {
	if incoming.MonthlyQuotaGB != nil && dashboardQuota(*incoming.MonthlyQuotaGB) != dashboardQuota(current.MonthlyQuotaGB) {
		return true
	}
	if incoming.BandwidthMbps != nil && dashboardBandwidth(*incoming.BandwidthMbps) != dashboardBandwidth(current.BandwidthMbps) {
		return true
	}
	if incoming.TrafficDirection != nil && dashboardDirection(*incoming.TrafficDirection) != dashboardDirection(current.TrafficDirection) {
		return true
	}
	return false
}

func normalizeBlockedDomains(values []string) ([]string, error) {
	if len(values) > 256 {
		return nil, errors.New("黑名单域名最多 256 个")
	}
	seen := make(map[string]bool, len(values))
	domains := make([]string, 0, len(values))
	for _, value := range values {
		host, err := normalizeSNIHost(value)
		if err != nil {
			return nil, fmt.Errorf("黑名单域名无效：%w", err)
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		domains = append(domains, host)
	}
	sort.Strings(domains)
	return domains, nil
}

func normalizeRealityCandidates(values []config.RealityCandidate) ([]config.RealityCandidate, error) {
	if len(values) > 64 {
		return nil, errors.New("候选伪装域名最多 64 个")
	}
	seen := make(map[string]bool, len(values))
	out := make([]config.RealityCandidate, 0, len(values))
	for _, value := range values {
		host, err := normalizeSNIHost(value.Host)
		if err != nil {
			return nil, err
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, config.RealityCandidate{Host: host, Port: 443})
	}
	return out, nil
}

func normalizeOutboundStrategy(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "auto", "prefer_ipv4", "prefer_ipv6", "ipv4_only", "ipv6_only":
		return value, nil
	default:
		return "", errors.New("出站网络策略无效")
	}
}

// normalizeRelay 校验中转配置：关闭时直接清空；落地机首次开启时随机生成端口和账号并固定下来。

// normalizeRelay 校验中转配置：关闭时直接清空；落地机首次开启时随机生成端口和账号并固定下来。
func (a *App) normalizeRelay(incoming config.RelaySettings) (config.RelaySettings, error) {
	if !incoming.Enabled {
		return config.RelaySettings{}, nil
	}
	relay := incoming
	relay.Host = strings.TrimSpace(relay.Host)
	relay.Username = strings.TrimSpace(relay.Username)
	relay.Password = strings.TrimSpace(relay.Password)
	switch relay.Role {
	case "":
		relay.Role = "transit"
	case "transit", "landing":
	default:
		return config.RelaySettings{}, errors.New("中转角色无效")
	}
	switch relay.Mode {
	case "":
		relay.Mode = "all"
	case "all", "categories":
	default:
		return config.RelaySettings{}, errors.New("中转流量模式无效")
	}
	categories := make([]string, 0, len(relay.Categories))
	for _, id := range relay.Categories {
		if proxy.RelayCategoryKnown(id) {
			categories = append(categories, id)
		}
	}
	relay.Categories = categories
	if relay.Role == "transit" {
		if relay.Host == "" {
			return config.RelaySettings{}, errors.New("中转机需要填写落地机地址")
		}
		if relay.Port < 1 || relay.Port > 65535 {
			return config.RelaySettings{}, errors.New("中转端口无效")
		}
		return relay, nil
	}
	snapshot := a.store.Snapshot()
	previous := snapshot.Settings.Relay
	used := map[int]bool{}
	for _, inbound := range snapshot.Inbounds {
		used[inbound.Port] = true
	}
	if _, port, err := net.SplitHostPort(a.runtime.Listen); err == nil {
		if value, err := strconv.Atoi(port); err == nil {
			used[value] = true
		}
	}
	if !previous.Enabled || previous.Role != "landing" {
		port, err := randomRelayPort(used)
		if err != nil {
			return config.RelaySettings{}, err
		}
		relay.Port = port
		relay.Username = "relay-" + config.RandomLetters(6)
		relay.Password = config.RandomHex(7)
		return relay, nil
	}
	if relay.Port < 1 || relay.Port > 65535 {
		port, err := randomRelayPort(used)
		if err != nil {
			return config.RelaySettings{}, err
		}
		relay.Port = port
	}
	if relay.Username == "" {
		relay.Username = "relay-" + config.RandomLetters(6)
	}
	if relay.Password == "" {
		relay.Password = config.RandomHex(7)
	}
	return relay, nil
}

func randomRelayPort(used map[int]bool) (int, error) {
	for range 100 {
		value, err := rand.Int(rand.Reader, big.NewInt(55536))
		if err != nil {
			return 0, errors.New("无法生成中转端口")
		}
		port := 10000 + int(value.Int64())
		if !used[port] {
			return port, nil
		}
	}
	return 0, errors.New("无法生成中转端口")
}
