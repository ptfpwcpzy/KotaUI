package app

import (
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func TestReservedServicePortsIncludesClash(t *testing.T) {
	runtime := config.Runtime{SubscriptionPort: 1109, StatsPort: 19090, Listen: "0.0.0.0:1989"}
	ports := reservedServicePorts(runtime)
	if ports[19090] != "流量统计" {
		t.Fatalf("stats port = %q", ports[19090])
	}
	if ports[19091] != "实时流量预览" {
		t.Fatalf("clash port = %q, want 实时流量预览", ports[19091])
	}
	if ports[1109] != "订阅服务" || ports[1989] != "面板" {
		t.Fatalf("reserved ports = %v", ports)
	}
	if err := validateReservedPort(19091, runtime); err == nil {
		t.Fatal("inbound must not use the clash port")
	}
}
