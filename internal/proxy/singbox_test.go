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
	if !ok || !strings.HasPrefix(link, "hy2://client-secret@example.test:24443/") || !strings.Contains(link, "obfs=salamander") || !strings.Contains(link, "obfs-password=obfs-secret") {
		t.Fatalf("unexpected Hysteria subscription: %q", link)
	}
}

func TestHysteriaSubscriptionOmitsDisabledObfs(t *testing.T) {
	state := config.DefaultState("example.test")
	state.Inbounds = []config.Inbound{{ID: "hy2", Name: "hy2", Type: "hysteria2", Enabled: true, Port: 24443}}
	state.Clients = []config.Client{{Username: "alice", InboundIDs: []string{"hy2"}, Credentials: map[string]string{"hy2": "client-secret"}}}
	link, ok := Subscription(state, config.Runtime{Domain: "example.test"}, "alice")
	if !ok || strings.Contains(link, "obfs=") || strings.Contains(link, "obfs-password=") {
		t.Fatalf("disabled obfuscation must be omitted: %q", link)
	}
}

func TestSS2022SubscriptionUsesLiteralSIP022Userinfo(t *testing.T) {
	state := config.DefaultState("example.test")
	state.Inbounds = []config.Inbound{{ID: "ss", Name: "ss", Type: "shadowsocks2022", Enabled: true, Port: 24443, ServerPassword: "server/key+"}}
	state.Clients = []config.Client{{Username: "alice", InboundIDs: []string{"ss"}, Credentials: map[string]string{"ss": "user/key+"}}}

	link, ok := Subscription(state, config.Runtime{Domain: "example.test"}, "alice")
	wantPrefix := "ss://2022-blake3-aes-256-gcm:server%2Fkey%2B:user%2Fkey%2B@example.test:24443"
	if !ok || !strings.HasPrefix(link, wantPrefix) {
		t.Fatalf("SS2022 subscription = %q, want prefix %q", link, wantPrefix)
	}
}

func TestWriteAndSubscribeTUIC(t *testing.T) {
	directory := t.TempDir()
	runtime := config.Runtime{Domain: "example.test", TLSCert: "/tmp/fullchain.pem", TLSKey: "/tmp/privkey.pem", SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	uuid := "2dd61d93-75d8-4da4-ac0e-6aece7eac365"
	state := config.DefaultState("example.test")
	state.Inbounds = []config.Inbound{{ID: "tuic", Name: "tuic", Type: "tuic", Enabled: true, Listen: "0.0.0.0", Port: 24443}}
	state.Clients = []config.Client{{Username: "alice", InboundIDs: []string{"tuic"}, Credentials: map[string]string{"tuic": uuid}, TUICPasswords: map[string]string{"tuic": "password-secret"}}}
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Inbounds []struct {
			Type              string `json:"type"`
			CongestionControl string `json:"congestion_control"`
			ZeroRTTHandshake  bool   `json:"zero_rtt_handshake"`
			Users             []struct {
				UUID     string `json:"uuid"`
				Password string `json:"password"`
			} `json:"users"`
			TLS struct {
				ALPN []string `json:"alpn"`
			} `json:"tls"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Inbounds) != 1 || decoded.Inbounds[0].Type != "tuic" || decoded.Inbounds[0].CongestionControl != "cubic" || decoded.Inbounds[0].ZeroRTTHandshake || len(decoded.Inbounds[0].Users) != 1 || decoded.Inbounds[0].Users[0].UUID != uuid || decoded.Inbounds[0].Users[0].Password != "password-secret" || len(decoded.Inbounds[0].TLS.ALPN) != 1 || decoded.Inbounds[0].TLS.ALPN[0] != "h3" {
		t.Fatalf("unexpected TUIC config: %#v", decoded.Inbounds)
	}
	link, ok := Subscription(state, runtime, "alice")
	for _, field := range []string{"tuic://" + uuid + ":password-secret@example.test:24443", "alpn=h3", "congestion_control=cubic", "udp_relay_mode=native", "sni=example.test", "allow_insecure=0"} {
		if !ok || !strings.Contains(link, field) {
			t.Fatalf("TUIC subscription missing %q: %q", field, link)
		}
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

func TestWriteRelayLandingAddsSocksInbound(t *testing.T) {
	directory := t.TempDir()
	state := config.DefaultState("example.test")
	state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "landing", Port: 41234, Username: "relay-abc", Password: "secret"}
	runtime := config.Runtime{SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Inbounds []struct {
			Type       string `json:"type"`
			Tag        string `json:"tag"`
			ListenPort int    `json:"listen_port"`
			Version    string `json:"version"`
			Users      []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Inbounds) != 1 || decoded.Inbounds[0].Type != "socks" || decoded.Inbounds[0].Tag != "relay-in" || decoded.Inbounds[0].ListenPort != 41234 || decoded.Inbounds[0].Version != "5" || len(decoded.Inbounds[0].Users) != 1 || decoded.Inbounds[0].Users[0].Username != "relay-abc" || decoded.Inbounds[0].Users[0].Password != "secret" {
		t.Fatalf("unexpected relay inbound: %#v", decoded.Inbounds)
	}
}

func TestWriteRelayTransitAllRoutesEverything(t *testing.T) {
	directory := t.TempDir()
	state := config.DefaultState("example.test")
	state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "transit", Host: "203.0.113.10", Port: 41234, Username: "relay-abc", Password: "secret", Mode: "all"}
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
			Type       string `json:"type"`
			Tag        string `json:"tag"`
			Server     string `json:"server"`
			ServerPort int    `json:"server_port"`
		} `json:"outbounds"`
		Route struct {
			Final string `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Outbounds) != 2 || decoded.Outbounds[0].Type != "socks" || decoded.Outbounds[0].Tag != "relay" || decoded.Outbounds[0].Server != "203.0.113.10" || decoded.Outbounds[0].ServerPort != 41234 || decoded.Outbounds[1].Tag != "direct" || decoded.Route.Final != "relay" {
		t.Fatalf("unexpected relay-all config: %#v %#v", decoded.Outbounds, decoded.Route)
	}
}

func TestWriteRelayTransitCategoriesRoutesOnlySelected(t *testing.T) {
	directory := t.TempDir()
	state := config.DefaultState("example.test")
	state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "transit", Host: "203.0.113.10", Port: 41234, Mode: "categories", Categories: []string{"netflix", "bogus", "youtube"}}
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
			Tag string `json:"tag"`
		} `json:"outbounds"`
		Route struct {
			Final string           `json:"final"`
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Outbounds) != 2 || decoded.Outbounds[0].Tag != "direct" || decoded.Outbounds[1].Tag != "relay" || decoded.Route.Final != "direct" {
		t.Fatalf("unexpected relay-categories outbounds: %#v %#v", decoded.Outbounds, decoded.Route)
	}
	relayed := []string{}
	for _, rule := range decoded.Route.Rules {
		if rule["outbound"] == "relay" {
			domains, _ := rule["domain_suffix"].([]any)
			for _, domain := range domains {
				relayed = append(relayed, domain.(string))
			}
		}
	}
	if len(relayed) != 7 || relayed[0] != "netflix.com" || relayed[4] != "youtube.com" {
		t.Fatalf("unexpected relayed domains: %#v", relayed)
	}
}

func TestWriteRelayCategoriesUsesLocalRuleSetsWhenPresent(t *testing.T) {
	directory := t.TempDir()
	ruleSets := filepath.Join(directory, "rule-sets")
	if err := os.MkdirAll(ruleSets, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ruleSets, "geosite-netflix.srs"), []byte("fake-srs"), 0600); err != nil {
		t.Fatal(err)
	}
	state := config.DefaultState("example.test")
	state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "transit", Host: "203.0.113.10", Port: 41234, Mode: "categories", Categories: []string{"netflix", "youtube"}}
	runtime := config.Runtime{DataDir: directory, SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Route struct {
			RuleSets []struct {
				Type   string `json:"type"`
				Tag    string `json:"tag"`
				Format string `json:"format"`
				Path   string `json:"path"`
			} `json:"rule_set"`
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Route.RuleSets) != 1 || decoded.Route.RuleSets[0].Type != "local" || decoded.Route.RuleSets[0].Tag != "geosite-netflix" || decoded.Route.RuleSets[0].Format != "binary" || decoded.Route.RuleSets[0].Path != filepath.Join(ruleSets, "geosite-netflix.srs") {
		t.Fatalf("unexpected rule sets: %#v", decoded.Route.RuleSets)
	}
	ruleSetRules, domainRules := 0, 0
	for _, rule := range decoded.Route.Rules {
		if _, ok := rule["rule_set"]; ok {
			ruleSetRules++
		}
		if _, ok := rule["domain_suffix"]; ok {
			domainRules++
		}
	}
	if ruleSetRules != 1 || domainRules != 2 {
		t.Fatalf("rule_set rules = %d, domain rules = %d, want 1 and 2", ruleSetRules, domainRules)
	}
}

func TestWriteRelayCategoriesSkipsMissingRuleSets(t *testing.T) {
	directory := t.TempDir()
	state := config.DefaultState("example.test")
	state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "transit", Host: "203.0.113.10", Port: 41234, Mode: "categories", Categories: []string{"netflix"}}
	runtime := config.Runtime{DataDir: directory, SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "rule_set") {
		t.Fatalf("missing rule set files must not be referenced: %s", body)
	}
}

func TestWriteRelayLandingUserCountedInStats(t *testing.T) {
	directory := t.TempDir()
	state := config.DefaultState("example.test")
	state.Settings.Relay = config.RelaySettings{Enabled: true, Role: "landing", Port: 41234, Username: "relay-abc", Password: "secret"}
	runtime := config.Runtime{DataDir: directory, SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
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
				Stats struct {
					Users []string `json:"users"`
				} `json:"stats"`
			} `json:"v2ray_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Experimental.V2RayAPI.Stats.Users) != 1 || decoded.Experimental.V2RayAPI.Stats.Users[0] != "relay-abc" {
		t.Fatalf("unexpected stats users: %#v", decoded.Experimental.V2RayAPI.Stats.Users)
	}
}

func TestWriteAddsDomainAndBitTorrentBlockRules(t *testing.T) {
	directory := t.TempDir()
	state := config.DefaultState("example.test")
	state.Settings.BlockedDomains = []string{"example.com", "tracker.example"}
	state.Settings.BlockBitTorrent = true
	runtime := config.Runtime{SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Route.Rules) != 3 {
		t.Fatalf("route rules = %#v", decoded.Route.Rules)
	}
	if decoded.Route.Rules[0]["action"] != "sniff" || decoded.Route.Rules[1]["action"] != "reject" || decoded.Route.Rules[2]["action"] != "reject" {
		t.Fatalf("unexpected route actions: %#v", decoded.Route.Rules)
	}
	domains, ok := decoded.Route.Rules[1]["domain_suffix"].([]any)
	if !ok || len(domains) != 2 || domains[0] != "example.com" || domains[1] != "tracker.example" {
		t.Fatalf("domain block rule = %#v", decoded.Route.Rules[1])
	}
	protocols, ok := decoded.Route.Rules[2]["protocol"].([]any)
	if !ok || len(protocols) != 1 || protocols[0] != "bittorrent" {
		t.Fatalf("BT block rule = %#v", decoded.Route.Rules[2])
	}
}
