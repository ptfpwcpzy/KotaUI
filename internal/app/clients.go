package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/store"
)

func (a *App) inbounds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.Snapshot().Inbounds)
	case http.MethodPost:
		var inbound config.Inbound
		if err := decodeJSON(r, &inbound); err != nil {
			badRequest(w, err)
			return
		}
		if err := a.prepareRealityInbound(&inbound); err != nil {
			serverError(w, err)
			return
		}
		if inbound.Name == "" {
			inbound.Name = protocolDisplayName(inbound.Type)
		}
		if err := validateInbound(&inbound); err != nil {
			badRequest(w, err)
			return
		}
		inbound.ID = config.NewID()
		inbound.Enabled = true
		if err := a.mutate(func(s *config.State) error {
			if err := validateReservedPort(inbound.Port, a.runtime); err != nil {
				return err
			}
			if err := validateInboundPort(s.Inbounds, inbound, ""); err != nil {
				return err
			}
			s.Inbounds = append(s.Inbounds, inbound)
			return nil
		}); err != nil {
			badRequest(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, inbound)
	default:
		methodNotAllowed(w)
	}
}

func (a *App) inboundAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/inbounds/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		badRequest(w, errors.New("入站 ID 不能为空"))
		return
	}
	id := parts[0]
	switch r.Method {
	case http.MethodDelete:
		err := a.mutate(func(s *config.State) error {
			for i, value := range s.Inbounds {
				if value.ID == id {
					s.Inbounds = append(s.Inbounds[:i], s.Inbounds[i+1:]...)
					s.Clients = detachInboundFromClients(s.Clients, id)
					return nil
				}
			}
			return errors.New("入站不存在")
		})
		if err != nil {
			badRequest(w, err)
			return
		}
	case http.MethodPatch:
		var incoming config.Inbound
		if err := decodeJSON(r, &incoming); err != nil {
			badRequest(w, err)
			return
		}
		for _, current := range a.store.Snapshot().Inbounds {
			if current.ID == id && incoming.Type == "reality" {
				if incoming.PrivateKey == "" {
					incoming.PrivateKey = current.PrivateKey
				}
				if incoming.PublicKey == "" {
					incoming.PublicKey = current.PublicKey
				}
				if incoming.ShortID == "" {
					incoming.ShortID = current.ShortID
				}
				break
			}
		}
		if err := a.prepareRealityInbound(&incoming); err != nil {
			serverError(w, err)
			return
		}
		if err := validateInbound(&incoming); err != nil {
			badRequest(w, err)
			return
		}
		if err := a.mutate(func(s *config.State) error {
			if err := validateReservedPort(incoming.Port, a.runtime); err != nil {
				return err
			}
			if err := validateInboundPort(s.Inbounds, incoming, id); err != nil {
				return err
			}
			for i := range s.Inbounds {
				if s.Inbounds[i].ID == id {
					incoming.ID = id
					incoming.Enabled = s.Inbounds[i].Enabled
					s.Inbounds[i] = incoming
					return nil
				}
			}
			return errors.New("入站不存在")
		}); err != nil {
			badRequest(w, err)
			return
		}
	case http.MethodPost:
		if len(parts) != 2 || parts[1] != "toggle" {
			methodNotAllowed(w)
			return
		}
		if err := a.mutate(func(s *config.State) error {
			for i := range s.Inbounds {
				if s.Inbounds[i].ID == id {
					s.Inbounds[i].Enabled = !s.Inbounds[i].Enabled
					return nil
				}
			}
			return errors.New("入站不存在")
		}); err != nil {
			badRequest(w, err)
			return
		}
	default:
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) clients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.resetMonth()
		a.syncTraffic()
		snapshot := a.store.Snapshot()
		clients := make([]config.Client, 0, len(snapshot.Clients)+1)
		clients = append(clients, snapshot.Clients...)
		if relay := snapshot.Settings.Relay; relay.Enabled && relay.Role == "landing" {
			traffic := snapshot.RelayTraffic
			clients = append(clients, config.Client{ID: "relay", Username: "中转主机A", Relay: true, InboundIDs: []string{}, UsedBytes: traffic.UsedBytes, MonthlyUsedBytes: traffic.MonthlyUsedBytes, DailyUsedBytes: traffic.DailyUsedBytes})
		}
		writeJSON(w, http.StatusOK, clients)
	case http.MethodPost:
		var request clientCreateRequest

		if err := decodeJSON(r, &request); err != nil {
			badRequest(w, err)
			return
		}
		client := request.Client
		client.SubscriptionSuffix = ""
		var err error
		client.ExpiresAt, err = resolveClientExpiry("", client.ExpiresAt, request.ExpiryUnit, request.ExpiryAmount, time.Now())
		if err != nil {
			badRequest(w, err)
			return
		}
		if err := validateClientInput(client); err != nil {
			badRequest(w, err)
			return
		}
		client.ID = config.NewID()
		client.CreatedAt = time.Now().UTC()
		client.Month = time.Now().Format("2006-01")
		client.DailyDate = time.Now().In(config.PanelLocation).Format("2006-01-02")
		client.Credentials = map[string]string{}
		client.TUICPasswords = map[string]string{}
		err = a.mutate(func(s *config.State) error {
			for _, existing := range s.Clients {
				if existing.Username == client.Username {
					return errors.New("用户名已存在")
				}
			}
			available := map[string]string{}
			for _, in := range s.Inbounds {
				available[in.ID] = in.Type
			}
			for _, id := range client.InboundIDs {
				protocol, exists := available[id]
				if !exists {
					return errors.New("选择的入站不存在")
				}
				credential, tuicPassword := newClientCredentials(protocol)
				client.Credentials[id] = credential
				if tuicPassword != "" {
					client.TUICPasswords[id] = tuicPassword
				}
			}
			if request.RandomSubscriptionSuffix == nil || *request.RandomSubscriptionSuffix {
				suffix, err := uniqueSubscriptionSuffix(s.Clients, client.Username)
				if err != nil {
					return err
				}
				client.SubscriptionSuffix = suffix
			}
			if err := validateUniqueSubscriptionID(s.Clients, client, ""); err != nil {
				return err
			}
			s.Clients = append(s.Clients, client)
			return nil
		})
		if err != nil {
			badRequest(w, err)
			return
		}
		a.syncSpeedLimits()
		writeJSON(w, http.StatusCreated, client)
	default:
		methodNotAllowed(w)
	}
}

type clientCreateRequest struct {
	clientExpiryRequest
	RandomSubscriptionSuffix *bool `json:"randomSubscriptionSuffix"`
}

type clientExpiryRequest struct {
	config.Client
	ExpiryUnit   string `json:"expiryUnit"`
	ExpiryAmount int    `json:"expiryAmount"`
}

func newClientCredentials(protocol string) (credential, tuicPassword string) {
	switch protocol {
	case "reality":
		return config.RandomUUID(), ""
	case "shadowsocks2022":
		return config.RandomBase64(32), ""
	case "tuic":
		return config.RandomUUID(), config.RandomToken(16)
	default:
		return config.RandomToken(16), ""
	}
}

func (a *App) clientAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/clients/"), "/")
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodPatch {
		var request clientCreateRequest
		if err := decodeJSON(r, &request); err != nil {
			badRequest(w, err)
			return
		}
		if err := a.updateClient(parts[0], request); err != nil {
			badRequest(w, err)
			return
		}
		// 保存后立即同步一次限速：开关拨动后不用等下一拍。
		a.syncSpeedLimits()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if len(parts) < 2 || r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	id, action := parts[0], parts[1]
	if action == "reset" {
		a.syncTraffic()
	}
	err := a.mutate(func(s *config.State) error {
		for i := range s.Clients {
			if s.Clients[i].ID != id {
				continue
			}
			c := &s.Clients[i]
			switch action {
			case "pause":
				c.Paused = true
			case "resume":
				c.Paused = false
			case "reset":
				c.UsedBytes = 0
				c.UploadBytes = 0
				c.DownloadBytes = 0
				c.MonthlyUsedBytes = 0
				c.MonthlyDownloadBytes = 0
			case "rotate":
				types := make(map[string]string, len(s.Inbounds))
				for _, inbound := range s.Inbounds {
					types[inbound.ID] = inbound.Type
				}
				if c.TUICPasswords == nil {
					c.TUICPasswords = map[string]string{}
				}
				for inboundID := range c.Credentials {
					credential, tuicPassword := newClientCredentials(types[inboundID])
					c.Credentials[inboundID] = credential
					if tuicPassword != "" {
						c.TUICPasswords[inboundID] = tuicPassword
					} else {
						delete(c.TUICPasswords, inboundID)
					}
				}
			case "delete":
				s.Clients = append(s.Clients[:i], s.Clients[i+1:]...)
			default:
				return errors.New("未知客户端操作")
			}
			return nil
		}
		return errors.New("客户端不存在")
	})
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) updateClient(id string, request clientCreateRequest) error {
	return a.mutate(func(s *config.State) error {
		var target *config.Client
		for i := range s.Clients {
			if s.Clients[i].ID == id {
				target = &s.Clients[i]
				break
			}
		}
		if target == nil {
			return errors.New("客户端不存在")
		}
		incoming := request.Client
		var err error
		incoming.ExpiresAt, err = resolveClientExpiry(target.ExpiresAt, incoming.ExpiresAt, request.ExpiryUnit, request.ExpiryAmount, time.Now())
		if err != nil {
			return err
		}
		if err := validateClientInput(incoming); err != nil {
			return err
		}
		for _, other := range s.Clients {
			if other.ID != id && other.Username == incoming.Username {
				return errors.New("用户名已存在")
			}
		}
		types := make(map[string]string, len(s.Inbounds))
		for _, inbound := range s.Inbounds {
			types[inbound.ID] = inbound.Type
		}
		credentials := map[string]string{}
		tuicPasswords := map[string]string{}
		for _, inboundID := range incoming.InboundIDs {
			protocol, exists := types[inboundID]
			if !exists {
				return errors.New("选择的入站不存在")
			}
			secret := target.Credentials[inboundID]
			if protocol == "tuic" {
				password := target.TUICPasswords[inboundID]
				if !config.ValidUUID(secret) || password == "" {
					secret, password = newClientCredentials(protocol)
				}
				tuicPasswords[inboundID] = password
			} else if secret == "" {
				secret, _ = newClientCredentials(protocol)
			}
			credentials[inboundID] = secret
		}
		target.Username = incoming.Username
		target.InboundIDs = incoming.InboundIDs
		target.Credentials = credentials
		target.TUICPasswords = tuicPasswords
		target.TotalLimitBytes = incoming.TotalLimitBytes
		target.MonthlyLimitBytes = incoming.MonthlyLimitBytes
		target.DailyLimitBytes = incoming.DailyLimitBytes
		target.ExpiresAt = incoming.ExpiresAt
		target.MaxOnlineIPs = incoming.MaxOnlineIPs
		target.SpeedLimitEnabled = incoming.SpeedLimitEnabled
		if request.RandomSubscriptionSuffix != nil {
			if *request.RandomSubscriptionSuffix {
				if target.SubscriptionSuffix == "" {
					suffix, err := uniqueSubscriptionSuffix(s.Clients, target.Username)
					if err != nil {
						return err
					}
					target.SubscriptionSuffix = suffix
				}
			} else {
				target.SubscriptionSuffix = ""
			}
		}
		if err := validateUniqueSubscriptionID(s.Clients, *target, id); err != nil {
			return err
		}
		return nil
	})
}

func resolveClientExpiry(current, legacyDate, unit string, amount int, now time.Time) (string, error) {
	if unit == "" {
		return legacyDate, nil
	}
	if unit == "keep" {
		return current, nil
	}
	if unit == "none" {
		return "", nil
	}
	if amount < 1 || amount > 9999 {
		return "", errors.New("有效期数量必须在 1–9999 之间")
	}
	localNow := now.In(config.PanelLocation)
	base := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, config.PanelLocation)
	switch unit {
	case "day":
		return base.AddDate(0, 0, amount).Format("2006-01-02"), nil
	case "month":
		return addCalendarMonths(base, amount).Format("2006-01-02"), nil
	case "year":
		return addCalendarMonths(base, amount*12).Format("2006-01-02"), nil
	default:
		return "", errors.New("有效期单位无效")
	}
}

func addCalendarMonths(base time.Time, months int) time.Time {
	monthIndex := int(base.Month()) - 1 + months
	year := base.Year() + monthIndex/12
	month := time.Month(monthIndex%12 + 1)
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, base.Location()).Day()
	day := base.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, 0, 0, 0, 0, base.Location())
}

func clientSubscriptionID(client config.Client) string {
	if client.SubscriptionSuffix == "" {
		return client.Username
	}
	return client.SubscriptionSuffix + "/" + client.Username
}

func validateUniqueSubscriptionID(clients []config.Client, candidate config.Client, excludeID string) error {
	identifier := clientSubscriptionID(candidate)
	if identifier == "" {
		return errors.New("订阅标识不能为空")
	}
	for _, client := range clients {
		if client.ID != excludeID && clientSubscriptionID(client) == identifier {
			return errors.New("订阅地址与已有客户端冲突，请修改用户名后重试")
		}
	}
	return nil
}

func uniqueSubscriptionSuffix(clients []config.Client, username string) (string, error) {
	for range 32 {
		suffix := config.RandomLetters(8)
		candidate := suffix + "/" + username
		used := false
		for _, client := range clients {
			if clientSubscriptionID(client) == candidate {
				used = true
				break
			}
		}
		if !used {
			return suffix, nil
		}
	}
	return "", errors.New("无法生成唯一订阅标识，请重试")
}

func (a *App) prepareRealityInbound(v *config.Inbound) error {
	if v.Type != "reality" {
		return nil
	}
	if v.HandshakePort == 0 {
		v.HandshakePort = 443
	}
	if v.SNI == "" {
		v.SNI = v.HandshakeServer
	}
	if v.ShortID == "" {
		v.ShortID = config.RandomHex(8)
	}
	if validRealityKeyPair(v.PrivateKey, v.PublicKey) {
		return nil
	}
	privateKey, publicKey, err := generateRealityKeypair(a.runtime.SingBoxBin)
	if err != nil {
		return err
	}
	v.PrivateKey = privateKey
	v.PublicKey = publicKey
	return nil
}

func validRealityKey(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	return err == nil && len(decoded) == 32
}

func validRealityKeyPair(privateKey, publicKey string) bool {
	return validRealityKey(privateKey) && validRealityKey(publicKey)
}

func generateRealityKeypair(binary string) (string, string, error) {
	if !filePresent(binary) {
		return "", "", errors.New("未找到 sing-box，无法自动生成 REALITY 密钥")
	}
	output, err := exec.Command(binary, "generate", "reality-keypair").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("生成 REALITY 密钥失败：%s", strings.TrimSpace(string(output)))
	}
	var privateKey, publicKey string
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "privatekey", "private key":
			privateKey = strings.TrimSpace(value)
		case "publickey", "public key":
			publicKey = strings.TrimSpace(value)
		}
	}
	if !validRealityKeyPair(privateKey, publicKey) {
		return "", "", errors.New("sing-box 未返回有效的 REALITY 密钥")
	}
	return privateKey, publicKey, nil
}

func validateInbound(v *config.Inbound) error {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		v.Name = protocolDisplayName(v.Type)
	}
	if v.Name == "" {
		return errors.New("入站名称不能为空")
	}
	if len([]rune(v.Name)) > 64 {
		return errors.New("入站名称不能超过 64 个字符")
	}
	if v.UseIPv6 {
		v.Listen = "::"
	} else if v.Listen == "" || v.Listen == "::" {
		v.Listen = "0.0.0.0"
	}
	if v.Port < 1024 || v.Port > 65535 {
		return errors.New("端口必须在 1024–65535 之间")
	}
	if v.UseIPv6 && !hasGlobalIPv6() {
		return errors.New("已开启 IPv6 分享，但服务器未检测到公网 IPv6 地址")
	}
	switch v.Type {
	case "reality":
		if v.HandshakeServer == "" || v.SNI == "" || v.PrivateKey == "" || v.PublicKey == "" {
			return errors.New("REALITY 目标、SNI 和密钥不能为空")
		}
		server, err := normalizeSNIHost(v.HandshakeServer)
		if err != nil {
			return fmt.Errorf("REALITY 目标无效：%w", err)
		}
		sni, err := normalizeSNIHost(v.SNI)
		if err != nil {
			return fmt.Errorf("REALITY SNI 无效：%w", err)
		}
		v.HandshakeServer, v.SNI = server, sni
		if v.HandshakePort == 0 {
			v.HandshakePort = 443
		}
		if v.ShortID == "" {
			v.ShortID = config.RandomHex(8)
		}
	case "hysteria2":
		// TLS server_name 固定使用安装域名（runtime.Domain），此处不再强制要求；
		// 若传入则做格式归一化，保持历史数据干净。
		if v.SNI != "" {
			sni, err := normalizeSNIHost(v.SNI)
			if err != nil {
				return fmt.Errorf("Hysteria 2 TLS server_name 无效：%w", err)
			}
			v.SNI = sni
		}
		if v.UpMbps == 0 {
			v.UpMbps = 500
		}
		if v.DownMbps == 0 {
			v.DownMbps = 500
		}
		if v.UpMbps < 1 || v.UpMbps > 100000 || v.DownMbps < 1 || v.DownMbps > 100000 {
			return errors.New("Hysteria 2 上下行带宽必须在 1–100000 Mbps 之间")
		}
	case "shadowsocks2022":
		if v.ServerPassword == "" {
			v.ServerPassword = config.RandomBase64(32)
		}
		if !validSS2022Key(v.ServerPassword) {
			return errors.New("Shadowsocks 2022 服务端密钥必须是 32 字节 Base64")
		}
	case "tuic":
	default:
		return errors.New("仅支持 REALITY、Hysteria 2、Shadowsocks 2022、TUIC")
	}
	return nil
}

func validSS2022Key(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func normalizeProtocolSecrets(s *store.Store, singBoxBin string) error {
	return s.Update(func(state *config.State) error {
		types := make(map[string]string, len(state.Inbounds))
		for _, inbound := range state.Inbounds {
			types[inbound.ID] = inbound.Type
		}
		for i := range state.Inbounds {
			inbound := &state.Inbounds[i]
			if inbound.Type == "reality" && !validRealityKeyPair(inbound.PrivateKey, inbound.PublicKey) {
				privateKey, publicKey, err := generateRealityKeypair(singBoxBin)
				if err != nil {
					return fmt.Errorf("修复 REALITY 入站 %q 的密钥失败：%w", inbound.Name, err)
				}
				inbound.PrivateKey = privateKey
				inbound.PublicKey = publicKey
			}
			if inbound.Type == "shadowsocks2022" && !validSS2022Key(inbound.ServerPassword) {
				inbound.ServerPassword = config.RandomBase64(32)
			}
		}
		for i := range state.Clients {
			if state.Clients[i].TUICPasswords == nil {
				state.Clients[i].TUICPasswords = map[string]string{}
			}
			for id := range state.Clients[i].Credentials {
				if types[id] == "reality" && !config.ValidUUID(state.Clients[i].Credentials[id]) {
					state.Clients[i].Credentials[id] = config.RandomUUID()
				}
				if types[id] == "shadowsocks2022" && !validSS2022Key(state.Clients[i].Credentials[id]) {
					state.Clients[i].Credentials[id] = config.RandomBase64(32)
				}
				if types[id] == "tuic" {
					if !config.ValidUUID(state.Clients[i].Credentials[id]) {
						state.Clients[i].Credentials[id] = config.RandomUUID()
					}
					if state.Clients[i].TUICPasswords[id] == "" {
						state.Clients[i].TUICPasswords[id] = config.RandomToken(16)
					}
				}
			}
		}
		return nil
	})
}

func hasGlobalIPv6() bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
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
				return true
			}
		}
	}
	return false
}
