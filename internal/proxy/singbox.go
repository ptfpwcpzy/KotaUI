package proxy

import (
	"encoding/base64"
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
	directOutbound := map[string]any{"type": "direct", "tag": "direct"}
	if strategy := state.Settings.OutboundStrategy; strategy != "" && strategy != "auto" {
		directOutbound["domain_strategy"] = strategy
	}
	root := map[string]any{
		"log":       map[string]any{"level": "warn", "timestamp": true},
		"inbounds":  inbounds,
		"outbounds": []map[string]any{directOutbound},
		"experimental": map[string]any{
			"v2ray_api": map[string]any{
				"listen": fmt.Sprintf("127.0.0.1:%d", runtime.StatsPort),
				"stats":  map[string]any{"enabled": true, "users": statsUsers},
			},
		},
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
				} else {
					query.Set("obfs", "none")
				}
				links = append(links, fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", url.PathEscape(secret), shareHost(inbound, runtime), inbound.Port, query.Encode(), url.QueryEscape(label)))
			case "shadowsocks2022":
				// SS2022 multi-user mode requires the server EIH key followed by the user key.
				encoded := base64.RawStdEncoding.EncodeToString([]byte("2022-blake3-aes-256-gcm:" + inbound.ServerPassword + ":" + secret))
				links = append(links, fmt.Sprintf("ss://%s@%s:%d#%s", encoded, shareHost(inbound, runtime), inbound.Port, url.QueryEscape(label)))

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
