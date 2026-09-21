package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

const (
	pingInterval   = time.Minute
	pingTimeout    = 2 * time.Second
	pingSamples    = 3
	pingHistoryTTL = 24 * time.Hour
	maxPingTargets = 32
)

type pingSample struct {
	TargetID      string    `json:"targetId"`
	CheckedAt     time.Time `json:"checkedAt"`
	Address       string    `json:"address,omitempty"`
	AddressFamily string    `json:"addressFamily,omitempty"`
	MinMs         float64   `json:"minMs"`
	AvgMs         float64   `json:"avgMs"`
	MaxMs         float64   `json:"maxMs"`
	Sent          int       `json:"sent"`
	Received      int       `json:"received"`
	Loss          float64   `json:"loss"`
	Error         string    `json:"error,omitempty"`
}

type pingHistoryFile struct {
	Samples []pingSample `json:"samples"`
}

type pingManager struct {
	mu      sync.RWMutex
	samples []pingSample
	path    string
}

var pingTimePattern = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)

func newPingManager(dataDir string) *pingManager {
	manager := &pingManager{path: filepath.Join(dataDir, "ping-history.json")}
	body, err := osReadFile(manager.path)
	if err == nil {
		var saved pingHistoryFile
		if json.Unmarshal(body, &saved) == nil {
			manager.samples = saved.Samples
		}
	}
	manager.pruneLocked(time.Now())
	return manager
}

// osReadFile is a small indirection that keeps file loading easy to replace in tests.
var osReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }

func (m *pingManager) pruneLocked(now time.Time) {
	cutoff := now.Add(-pingHistoryTTL)
	kept := m.samples[:0]
	for _, sample := range m.samples {
		if sample.CheckedAt.After(cutoff) {
			kept = append(kept, sample)
		}
	}
	m.samples = kept
}

func (m *pingManager) add(sample pingSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, sample)
	m.pruneLocked(time.Now())
	body, err := json.MarshalIndent(pingHistoryFile{Samples: m.samples}, "", "  ")
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err == nil {
		_ = os.Rename(tmp, m.path)
	}
}

func (m *pingManager) snapshot(targetIDs map[string]bool, now time.Time) []pingSample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cutoff := now.Add(-pingHistoryTTL)
	out := make([]pingSample, 0, len(m.samples))
	for _, sample := range m.samples {
		if sample.CheckedAt.After(cutoff) && (len(targetIDs) == 0 || targetIDs[sample.TargetID]) {
			out = append(out, sample)
		}
	}
	return out
}

func (m *pingManager) removeTarget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.samples[:0]
	for _, sample := range m.samples {
		if sample.TargetID != id {
			kept = append(kept, sample)
		}
	}
	m.samples = kept
	body, err := json.MarshalIndent(pingHistoryFile{Samples: m.samples}, "", "  ")
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err == nil {
		_ = os.Rename(tmp, m.path)
	}
}

func (a *App) networkQualityLoop() {
	a.runPingChecks()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.runPingChecks()
	}
}

func (a *App) runPingChecks() {
	state := a.store.Snapshot()
	var group sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	for _, target := range state.Settings.PingTargets {
		if strings.TrimSpace(target.Address) == "" || strings.TrimSpace(target.Name) == "" {
			continue
		}
		group.Add(1)
		go func(target config.PingTarget) {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			a.pingTarget(target)
		}(target)
	}
	group.Wait()
}

func (a *App) pingTarget(target config.PingTarget) {
	values := make([]float64, 0, pingSamples)
	var lastError string
	var resolvedAddress, family string
	for i := 0; i < pingSamples; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout+500*time.Millisecond)
		value, address, err := systemPing(ctx, target.Address)
		cancel()
		if err != nil {
			lastError = err.Error()
		} else {
			values = append(values, value)
			resolvedAddress = address
			if ip := net.ParseIP(address); ip != nil {
				family = "IPv4"
				if ip.To4() == nil {
					family = "IPv6"
				}
			}
		}
		if i+1 < pingSamples {
			time.Sleep(250 * time.Millisecond)
		}
	}
	sample := pingSample{TargetID: target.ID, CheckedAt: time.Now().UTC(), Address: resolvedAddress, AddressFamily: family, Sent: pingSamples, Received: len(values), Loss: float64(pingSamples-len(values)) * 100 / float64(pingSamples), Error: lastError}
	if len(values) > 0 {
		sample.MinMs, sample.MaxMs = values[0], values[0]
		var sum float64
		for _, value := range values {
			if value < sample.MinMs {
				sample.MinMs = value
			}
			if value > sample.MaxMs {
				sample.MaxMs = value
			}
			sum += value
		}
		sample.AvgMs = sum / float64(len(values))
	}
	a.pings.add(sample)
}

func systemPing(ctx context.Context, address string) (float64, string, error) {
	cmd := exec.CommandContext(ctx, "ping", "-n", "-c", "1", "-W", "2", address)
	output, err := cmd.CombinedOutput()
	text := string(output)
	match := pingTimePattern.FindStringSubmatch(text)
	if err != nil || len(match) != 2 {
		return 0, "", errors.New("目标不可达或 ping 命令不可用")
	}
	value, parseErr := strconv.ParseFloat(match[1], 64)
	if parseErr != nil {
		return 0, "", fmt.Errorf("无法解析 ping 延迟: %w", parseErr)
	}
	resolved := address
	if host, _, splitErr := net.SplitHostPort(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "PING "), "["))); splitErr == nil {
		resolved = host
	}
	if ips, resolveErr := net.LookupIP(address); resolveErr == nil && len(ips) > 0 {
		resolved = ips[0].String()
	}
	return value, resolved, nil
}

func validatePingTarget(name, address string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 40 {
		return errors.New("名称不能为空且不能超过 40 个字符")
	}
	address = strings.TrimSpace(address)
	if address == "" || len(address) > 253 || strings.ContainsAny(address, " \t\r\n") {
		return errors.New("IP 或域名格式无效")
	}
	if ip := net.ParseIP(strings.Trim(address, "[]")); ip != nil {
		return nil
	}
	if strings.Contains(address, "/") || strings.Contains(address, ":") {
		return errors.New("IP 或域名格式无效")
	}
	for _, label := range strings.Split(address, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("IP 或域名格式无效")
		}
	}
	return nil
}

func (a *App) networkQuality(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	state := a.store.Snapshot()
	targetIDs := make(map[string]bool, len(state.Settings.PingTargets))
	for _, target := range state.Settings.PingTargets {
		targetIDs[target.ID] = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": state.Settings.PingTargets, "samples": a.pings.snapshot(targetIDs, time.Now())})
}

func (a *App) pingTargetAction(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/network-quality/targets/")
	if r.Method != http.MethodDelete || id == "" {
		methodNotAllowed(w)
		return
	}
	if err := a.store.Update(func(state *config.State) error {
		found := false
		targets := state.Settings.PingTargets[:0]
		for _, target := range state.Settings.PingTargets {
			if target.ID == id {
				found = true
				continue
			}
			targets = append(targets, target)
		}
		if !found {
			return errors.New("监测目标不存在")
		}
		state.Settings.PingTargets = targets
		return nil
	}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.pings.removeTarget(id)
	writeJSON(w, http.StatusOK, map[string]string{"message": "监测目标已删除"})
}

func (a *App) addPingTarget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	if err := decodeJSON(r, &input); err != nil {
		badRequest(w, err)
		return
	}
	input.Name, input.Address = strings.TrimSpace(input.Name), strings.TrimSpace(input.Address)
	if err := validatePingTarget(input.Name, input.Address); err != nil {
		badRequest(w, err)
		return
	}
	var created config.PingTarget
	err := a.store.Update(func(state *config.State) error {
		if len(state.Settings.PingTargets) >= maxPingTargets {
			return fmt.Errorf("最多添加 %d 个监测目标", maxPingTargets)
		}
		for _, target := range state.Settings.PingTargets {
			if strings.EqualFold(target.Address, input.Address) {
				return errors.New("该地址已经添加")
			}
		}
		created = config.PingTarget{ID: config.NewID(), Name: input.Name, Address: input.Address, CreatedAt: time.Now().UTC()}
		state.Settings.PingTargets = append(state.Settings.PingTargets, created)
		return nil
	})
	if err != nil {
		badRequest(w, err)
		return
	}
	go a.pingTarget(created)
	writeJSON(w, http.StatusCreated, created)
}
