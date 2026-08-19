package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func TestWriteEnablesLocalUserTrafficStats(t *testing.T) {
	directory := t.TempDir()
	runtime := config.Runtime{SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	state := config.DefaultState("example.test")
	state.Clients = []config.Client{{Username: "alice"}, {Username: "bob"}}
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Experimental struct {
			V2RayAPI struct {
				Listen string `json:"listen"`
				Stats  struct {
					Enabled bool     `json:"enabled"`
					Users   []string `json:"users"`
				} `json:"stats"`
			} `json:"v2ray_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	stats := decoded.Experimental.V2RayAPI
	if stats.Listen != "127.0.0.1:19090" || !stats.Stats.Enabled || len(stats.Stats.Users) != 2 || stats.Stats.Users[0] != "alice" || stats.Stats.Users[1] != "bob" {
		t.Fatalf("unexpected stats config: %#v", stats)
	}
}

func TestHysteriaSubscriptionCarriesObfsParameters(t *testing.T) {
	state := config.DefaultState("example.test")
	state.Inbounds = []config.Inbound{{ID: "hy2", Name: "hy2", Type: "hysteria2", Enabled: true, Port: 24443, ObfsPassword: "obfs-secret"}}
	state.Clients = []config.Client{{Username: "alice", InboundIDs: []string{"hy2"}, Credentials: map[string]string{"hy2": "client-secret"}}}
	link, ok := Subscription(state, config.Runtime{Domain: "example.test"}, "alice")
	if !ok || !strings.Contains(link, "obfs=salamander") || !strings.Contains(link, "obfs-password=obfs-secret") {
		t.Fatalf("unexpected Hysteria subscription: %q", link)
	}
}

func TestWriteAppliesOutboundAddressStrategy(t *testing.T) {
	for _, test := range []struct {
		name     string
		strategy string
		want     string
	}{
		{name: "automatic", strategy: "auto", want: ""},
		{name: "prefer ipv4", strategy: "prefer_ipv4", want: "prefer_ipv4"},
		{name: "ipv6 only", strategy: "ipv6_only", want: "ipv6_only"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			state := config.DefaultState("example.test")
			state.Settings.OutboundStrategy = test.strategy
			runtime := config.Runtime{SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
			if err := Write(state, runtime); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(runtime.SingBoxConfig)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Outbounds []struct {
					DomainStrategy string `json:"domain_strategy"`
				} `json:"outbounds"`
			}
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.Outbounds) != 1 || decoded.Outbounds[0].DomainStrategy != test.want {
				t.Fatalf("strategy = %q, want %q", decoded.Outbounds[0].DomainStrategy, test.want)
			}
		})
	}
}
