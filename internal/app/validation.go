package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func validateClientInput(client config.Client) error {
	if !usernamePattern.MatchString(client.Username) {
		return errors.New("用户名仅允许 3–32 位字母、数字、下划线或连字符")
	}
	if len(client.InboundIDs) == 0 {
		return errors.New("至少选择一个入站")
	}
	if client.TotalLimitBytes < 0 || client.MonthlyLimitBytes < 0 || client.MaxOnlineIPs < 0 {
		return errors.New("流量配额和在线 IP 限制不能小于 0")
	}
	if client.ExpiresAt != "" {
		if _, err := time.ParseInLocation("2006-01-02", client.ExpiresAt, time.Local); err != nil {
			return errors.New("有效期格式无效")
		}
	}
	seen := make(map[string]struct{}, len(client.InboundIDs))
	for _, id := range client.InboundIDs {
		if id == "" {
			return errors.New("入站绑定无效")
		}
		if _, exists := seen[id]; exists {
			return errors.New("入站不能重复绑定")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateInboundPort(inbounds []config.Inbound, candidate config.Inbound, excludeID string) error {
	for _, inbound := range inbounds {
		if inbound.ID == excludeID {
			continue
		}
		if inbound.Port == candidate.Port {
			return fmt.Errorf("端口 %d 已被入站 %q 使用", candidate.Port, inbound.Name)
		}
	}
	return nil
}
