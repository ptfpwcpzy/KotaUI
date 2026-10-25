package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func Write(state config.State, runtime config.Runtime) error {
	if err := os.MkdirAll(filepath.Dir(runtime.SingBoxConfig), 0700); err != nil {
		return err
	}
	inbounds := make([]map[string]any, 0, len(state.Inbounds))
	active := activeClients(state.Clients)
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled {
			continue
		}
		switch inbound.Type {
		case "reality":
			users := []map[string]any{}
			for _, client := range active {
				if secret := client.Credentials[inbound.ID]; secret != "" {
					users = append(users, map[string]any{"name": client.Username, "uuid": secret, "flow": "xtls-rprx-vision"})
				}
			}
			inbounds = append(inbounds, map[string]any{"type": "vless", "tag": inbound.ID, "listen": inbound.Listen, "listen_port": inbound.Port, "users": users, "tls": map[string]any{"enabled": true, "server_name": inbound.SNI, "reality": map[string]any{"enabled": true, "handshake": map[string]any{"server": inbound.HandshakeServer, "server_port": inbound.HandshakePort}, "private_key": inbound.PrivateKey, "short_id": []string{inbound.ShortID}}}})
		case "hysteria2":
			users := []map[string]any{}
			for _, client := range active {
				if secret := client.Credentials[inbound.ID]; secret != "" {
					users = append(users, map[string]any{"name": client.Username, "password": secret})
				}
			}
			tls := map[string]any{"enabled": true, "certificate_path": runtime.TLSCert, "key_path": runtime.TLSKey, "server_name": runtime.Domain}

			entry := map[string]any{"type": "hysteria2", "tag": inbound.ID, "listen": inbound.Listen, "listen_port": inbound.Port, "users": users, "tls": tls, "up_mbps": inbound.UpMbps, "down_mbps": inbound.DownMbps}
			if inbound.ObfsPassword != "" {
				entry["obfs"] = map[string]any{"type": "salamander", "password": inbound.ObfsPassword}
			}
			inbounds = append(inbounds, entry)
		case "tuic":
			users := []map[string]any{}
			for _, client := range active {
				if uuid, password := client.Credentials[inbound.ID], client.TUICPasswords[inbound.ID]; uuid != "" && password != "" {
					users = append(users, map[string]any{"name": client.Username, "uuid": uuid, "password": password})
				}
			}
			tls := map[string]any{"enabled": true, "certificate_path": runtime.TLSCert, "key_path": runtime.TLSKey, "server_name": runtime.Domain, "alpn": []string{"h3"}}
			inbounds = append(inbounds, map[string]any{"type": "tuic", "tag": inbound.ID, "listen": inbound.Listen, "listen_port": inbound.Port, "users": users, "congestion_control": "cubic", "auth_timeout": "3s", "zero_rtt_handshake": false, "heartbeat": "10s", "tls": tls})
		case "shadowsocks2022":
			users := []map[string]any{}
			for _, client := range active {
				if secret := client.Credentials[inbound.ID]; secret != "" {
					users = append(users, map[string]any{"name": client.Username, "password": secret})
				}
			}
			inbounds = append(inbounds, map[string]any{"type": "shadowsocks", "tag": inbound.ID, "listen": inbound.Listen, "listen_port": inbound.Port, "method": "2022-blake3-aes-256-gcm", "password": inbound.ServerPassword, "users": users})
		}
	}
	statsUsers := make([]string, 0, len(state.Clients))
	for _, client := range state.Clients {
		if client.Username != "" {
			statsUsers = append(statsUsers, client.Username)
		}
	}
	sort.Strings(statsUsers)
	relay := state.Settings.Relay
	if relay.Enabled && relay.Role == "landing" {
		inbounds = append(inbounds, map[string]any{"type": "socks", "tag": "relay-in", "listen": "0.0.0.0", "listen_port": relay.Port, "version": "5", "users": []map[string]any{{"username": relay.Username, "password": relay.Password}}})
	}
	directOutbound := map[string]any{"type": "direct", "tag": "direct"}
	if strategy := state.Settings.OutboundStrategy; strategy != "" && strategy != "auto" {
		directOutbound["domain_strategy"] = strategy
	}
	outbounds := []map[string]any{directOutbound}
	if relay.Enabled && relay.Role == "transit" {
		relayOutbound := map[string]any{"type": "socks", "tag": "relay", "server": relay.Host, "server_port": relay.Port, "version": "5"}
		if relay.Username != "" {
			relayOutbound["username"] = relay.Username
		}
		if relay.Password != "" {
			relayOutbound["password"] = relay.Password
		}
		if relay.Mode == "all" {
			outbounds = []map[string]any{relayOutbound, directOutbound}
		} else {
			outbounds = []map[string]any{directOutbound, relayOutbound}
		}
	}
	root := map[string]any{
		// 只记录 error 级别且输出到 stderr：由 systemd-journald / OpenRC 统一收集并轮转，
		// 避免在小磁盘 VPS 上因 info 级 access 日志无限增长写满磁盘。面板日志页读取的
		// 正是服务 stdout/stderr（journalctl / /var/log/kotaui-singbox.log），不受影响。
		"log":       map[string]any{"level": "error", "timestamp": true},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"experimental": map[string]any{
			"v2ray_api": map[string]any{
				"listen": fmt.Sprintf("127.0.0.1:%d", runtime.StatsPort),
				"stats":  map[string]any{"enabled": true, "users": statsUsers},
			},
		},
	}
	categoryRules := relayCategoryRules(relay)
	route := map[string]any{}
	if relay.Enabled && relay.Role == "transit" {
		if relay.Mode == "all" {
			route["final"] = "relay"
		} else {
			route["final"] = "direct"
		}
	}
	if len(state.Settings.BlockedDomains) > 0 || state.Settings.BlockBitTorrent || len(categoryRules) > 0 {
		rules := []map[string]any{{"action": "sniff", "timeout": "300ms"}}
		if len(state.Settings.BlockedDomains) > 0 {
			rules = append(rules, map[string]any{"domain_suffix": state.Settings.BlockedDomains, "action": "reject"})
		}
		if state.Settings.BlockBitTorrent {
			rules = append(rules, map[string]any{"protocol": []string{"bittorrent"}, "action": "reject"})
		}
		rules = append(rules, categoryRules...)
		route["rules"] = rules
	}
	if len(route) > 0 {
		root["route"] = route
	}
	body, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	tmp := runtime.SingBoxConfig + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, runtime.SingBoxConfig)
}

// relayCategories 是中转“指定分类”里每个分类对应的主域名（后缀匹配）。
type relayCategory struct {
	id      string
	domains []string
}

var relayCategories = []relayCategory{
	{"netflix", []string{"netflix.com", "nflxext.com", "nflximg.net", "nflxvideo.net"}},
	{"disney", []string{"disneyplus.com", "disney-plus.net"}},
	{"youtube", []string{"youtube.com", "googlevideo.com", "ytimg.com"}},
	{"ai", []string{"openai.com", "chatgpt.com", "claude.ai", "anthropic.com", "deepseek.com", "grok.com"}},
	{"game", []string{"steampowered.com", "steamcommunity.com", "epicgames.com", "playstation.com", "xbox.com"}},
	{"social", []string{"x.com", "twitter.com", "instagram.com", "facebook.com", "fbcdn.net"}},
	{"hbo", []string{"max.com", "hbomax.com"}},
	{"prime", []string{"primevideo.com", "amazonvideo.com"}},
	{"appletv", []string{"tv.apple.com"}},
	{"tiktok", []string{"tiktok.com", "tiktokcdn.com"}},
	{"twitch", []string{"twitch.tv", "twitchcdn.net"}},
	{"spotify", []string{"spotify.com", "scdn.co"}},
	{"telegram", []string{"telegram.org", "t.me"}},
	{"cloud", []string{"dropbox.com", "drive.google.com", "onedrive.live.com", "box.com"}},
	{"microsoft", []string{"microsoft.com", "office.com", "outlook.com", "live.com"}},
	{"apple", []string{"apple.com", "icloud.com"}},
	{"google", []string{"google.com", "gmail.com", "googleapis.com", "gstatic.com"}},
}

// RelayCategoryKnown 报告分类 id 是否在中转分类表里。
func RelayCategoryKnown(id string) bool {
	for _, category := range relayCategories {
		if category.id == id {
			return true
		}
	}
	return false
}

func relayCategoryRules(relay config.RelaySettings) []map[string]any {
	if !relay.Enabled || relay.Role != "transit" || relay.Mode != "categories" {
		return nil
	}
	rules := []map[string]any{}
	for _, id := range relay.Categories {
		for _, category := range relayCategories {
			if category.id == id {
				rules = append(rules, map[string]any{"domain_suffix": category.domains, "outbound": "relay"})
			}
		}
	}
	return rules
}

func activeClients(clients []config.Client) []config.Client {
	now := time.Now()
	currentMonth := now.Format("2006-01")
	out := []config.Client{}
	for _, client := range clients {
		if client.Month != currentMonth {
			client.MonthlyUsedBytes = 0
		}
		if client.Active(now) {
			out = append(out, client)
		}
	}
	return out
}

func Subscription(state config.State, runtime config.Runtime, username string) (string, bool) {
	var client *config.Client
	for i := range state.Clients {
		if state.Clients[i].Username == username {
			client = &state.Clients[i]
			break
		}
	}
	if client == nil || !client.Active(time.Now()) {
		return "", false
	}
	links := []string{}
	for _, id := range client.InboundIDs {
		for _, inbound := range state.Inbounds {
			if inbound.ID != id || !inbound.Enabled {
				continue
			}
			secret := client.Credentials[id]
			if secret == "" {
				continue
			}
			label := urlLabel(inbound.Name, client.Username)
			switch inbound.Type {
			case "reality":
				host := shareHost(inbound, runtime)
				query := url.Values{}

				query.Set("encryption", "none")
				query.Set("security", "reality")
				query.Set("flow", "xtls-rprx-vision")
				query.Set("sni", inbound.SNI)
				query.Set("fp", "chrome")
				query.Set("pbk", inbound.PublicKey)
				query.Set("sid", inbound.ShortID)
				query.Set("type", "tcp")
				query.Set("headerType", "none")
				links = append(links, fmt.Sprintf("vless://%s@%s:%d?%s#%s", url.PathEscape(secret), host, inbound.Port, query.Encode(), url.QueryEscape(label)))
			case "hysteria2":
				query := url.Values{"sni": []string{runtime.Domain}, "insecure": []string{"0"}}
				if inbound.ObfsPassword != "" {
					query.Set("obfs", "salamander")
					query.Set("obfs-password", inbound.ObfsPassword)
				}
				// NekoBox recognizes the official short Hysteria2 scheme, hy2://.
				links = append(links, fmt.Sprintf("hy2://%s@%s:%d/?%s#%s", url.PathEscape(secret), shareHost(inbound, runtime), inbound.Port, query.Encode(), url.QueryEscape(label)))
			case "tuic":
				password := client.TUICPasswords[id]
				if password == "" {
					continue
				}
				query := url.Values{"alpn": []string{"h3"}, "congestion_control": []string{"cubic"}, "udp_relay_mode": []string{"native"}, "sni": []string{runtime.Domain}, "allow_insecure": []string{"0"}}
				links = append(links, fmt.Sprintf("tuic://%s:%s@%s:%d?%s#%s", url.PathEscape(secret), url.PathEscape(password), shareHost(inbound, runtime), inbound.Port, query.Encode(), url.QueryEscape(label)))
			case "shadowsocks2022":
				// SIP022 / SS2022 userinfo is literal method:server-key:user-key, not legacy Base64 userinfo.
				userInfo := strings.Join([]string{
					url.QueryEscape("2022-blake3-aes-256-gcm"),
					url.QueryEscape(inbound.ServerPassword),
					url.QueryEscape(secret),
				}, ":")
				links = append(links, fmt.Sprintf("ss://%s@%s:%d#%s", userInfo, shareHost(inbound, runtime), inbound.Port, url.QueryEscape(label)))

			}
		}
	}
	return strings.Join(links, "\n"), true
}

func shareHost(inbound config.Inbound, runtime config.Runtime) string {
	if !inbound.UseIPv6 {
		return runtime.Domain
	}
	if ip := publicIPv6(); ip != "" {
		return "[" + ip + "]"
	}
	return runtime.Domain
}

func publicIPv6() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && ip.To4() == nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
				return ip.String()
			}
		}
	}
	return ""
}

func urlLabel(parts ...string) string {
	return strings.NewReplacer(" ", "-", "#", "", "?", "").Replace(strings.Join(parts, "-"))
}
