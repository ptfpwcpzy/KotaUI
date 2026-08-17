package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
	"github.com/ptfpwcpzy/KotaUI/internal/store"
	"github.com/ptfpwcpzy/KotaUI/internal/system"
)

//go:embed web/*
var files embed.FS

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

type App struct {
	runtime             config.Runtime
	store               *store.Store
	key                 []byte
	mu                  sync.Mutex
	trafficMu           sync.Mutex
	lastTrafficSync     time.Time
	trafficSyncInterval time.Duration
	updateMu            sync.Mutex
	updateRunning       bool
	sniProbe            sniProbeFunc
}

func New(runtime config.Runtime) (*App, error) {
	if runtime.PanelPath == "" {
		runtime.PanelPath = "/ptf"
	}
	runtime.PanelPath = config.NormalizePath(runtime.PanelPath)
	if runtime.SubscriptionPort == 0 {
		runtime.SubscriptionPort = 1109
	}
	if runtime.StatsPort == 0 {
		runtime.StatsPort = 9090
	}
	if runtime.SingBoxBin == "" {
		runtime.SingBoxBin = "/usr/local/bin/sing-box"
	}
	if runtime.SingBoxConfig == "" {
		runtime.SingBoxConfig = path.Join(runtime.DataDir, "sing-box", "config.json")
	}
	s, err := store.Open(runtime.DataDir, runtime.Domain)
	if err != nil {
		return nil, err
	}
	if err := normalizeProtocolSecrets(s, runtime.SingBoxBin); err != nil {
		return nil, err
	}
	if err := proxy.Write(s.Snapshot(), runtime); err != nil {
		return nil, err
	}
	keyHash := sha256.Sum256([]byte(runtime.AdminPassword + "|" + runtime.DataDir))
	return &App{runtime: runtime, store: s, key: keyHash[:], trafficSyncInterval: 3 * time.Second, sniProbe: probeSNI}, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/kota-sub/", a.subscription)
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/logout", a.logout)
	mux.HandleFunc("/api/state", a.auth(a.state))
	mux.HandleFunc("/api/overview", a.auth(a.dashboard))
	mux.HandleFunc("/api/dashboard", a.auth(a.dashboard))
	mux.HandleFunc("/api/inbounds", a.auth(a.inbounds))
	mux.HandleFunc("/api/inbounds/", a.auth(a.inboundAction))
	mux.HandleFunc("/api/clients", a.auth(a.clients))
	mux.HandleFunc("/api/clients/", a.auth(a.clientAction))
	mux.HandleFunc("/api/settings", a.auth(a.settings))
	mux.HandleFunc("/api/config/validate", a.auth(a.validateConfig))
	mux.HandleFunc("/api/reality/test", a.auth(a.sniTest))
	mux.HandleFunc("/api/reality/test-all", a.auth(a.sniTestAll))
	mux.HandleFunc("/api/services/", a.auth(a.serviceAction))
	mux.HandleFunc("/api/update", a.auth(a.updatePanel))
	mux.HandleFunc("/api/certificate/renew", a.auth(a.renewCertificate))
	mux.HandleFunc("/api/logs/", a.auth(a.logs))
	mux.HandleFunc(a.runtime.PanelPath, a.panel)
	mux.HandleFunc(a.runtime.PanelPath+"/", a.panel)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, a.runtime.PanelPath, http.StatusFound)
	})
	return withSecurityHeaders(mux)
}

func (a *App) Serve() error {
	server := &http.Server{Addr: a.runtime.Listen, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	if a.runtime.TLSCert != "" && a.runtime.TLSKey != "" {
		if _, err := os.Stat(a.runtime.TLSCert); err == nil {
			return server.ListenAndServeTLS(a.runtime.TLSCert, a.runtime.TLSKey)
		}
	}
	return server.ListenAndServe()
}

func (a *App) ServeSubscription() error {
	mux := http.NewServeMux()
	mux.HandleFunc(a.store.Snapshot().Settings.SubscriptionPath+"/", a.subscription)
	server := &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", a.runtime.SubscriptionPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	if a.runtime.TLSCert != "" && a.runtime.TLSKey != "" {
		if _, err := os.Stat(a.runtime.TLSCert); err == nil {
			return server.ListenAndServeTLS(a.runtime.TLSCert, a.runtime.TLSKey)
		}
	}
	return server.ListenAndServe()
}

func (a *App) panel(w http.ResponseWriter, _ *http.Request) {
	body, _ := files.ReadFile("web/index.html")
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}
func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": config.Version})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		badRequest(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Username), []byte(a.runtime.AdminUser)) != 1 || subtle.ConstantTimeCompare([]byte(body.Password), []byte(a.runtime.AdminPassword)) != 1 {
		http.Error(w, "账号或密码错误", http.StatusUnauthorized)
		return
	}
	value := a.signSession(time.Now().Add(12 * time.Hour))
	http.SetCookie(w, &http.Cookie{Name: "kotaui_session", Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 43200})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (a *App) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "kotaui_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("kotaui_session")
		if e != nil || !a.verifySession(c.Value) {
			http.Error(w, "请先登录", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
func (a *App) signSession(expiry time.Time) string {
	payload := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, a.key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))))
}
func (a *App) verifySession(value string) bool {
	raw, e := base64.RawURLEncoding.DecodeString(value)
	if e != nil {
		return false
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 2 {
		return false
	}
	stamp, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || time.Now().Unix() > stamp {
		return false
	}
	expected := a.signSession(time.Unix(stamp, 0))
	return hmac.Equal([]byte(value), []byte(expected))
}

func (a *App) state(w http.ResponseWriter, _ *http.Request) {
	a.resetMonth()
	a.syncTraffic()
	writeJSON(w, http.StatusOK, a.store.Snapshot())
}
func (a *App) dashboard(w http.ResponseWriter, _ *http.Request) {
	a.resetMonth()
	a.syncTraffic()
	s := a.store.Snapshot()
	active, totalUsed, monthlyUsed := 0, int64(0), int64(0)
	ports := make([]int, 0, len(s.Inbounds))
	for _, inbound := range s.Inbounds {
		if inbound.Enabled {
			ports = append(ports, inbound.Port)
		}
	}
	for _, client := range s.Clients {
		if client.Active(time.Now()) {
			active++
		}
		totalUsed += client.UsedBytes
		monthlyUsed += client.MonthlyUsedBytes
	}
	certificate := certificateStatus(a.runtime.TLSCert)
	services := []map[string]any{
		{"id": "panel", "name": "KotaUI 面板", "running": serviceRunning("kotaui")},
		{"id": "singbox", "name": "sing-box 核心", "running": serviceRunning("kotaui-singbox")},
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"metrics":             system.Collect(a.runtime.DataDir, ports),
		"activeClients":       active,
		"inboundCount":        len(s.Inbounds),
		"clientCount":         len(s.Clients),
		"totalUsed":           totalUsed,
		"monthlyUsed":         monthlyUsed,
		"panelURL":            a.panelURL(),
		"subscriptionBaseURL": a.subscriptionBaseURL(s.Settings.SubscriptionPath),
		"version":             config.Version,
		"certificate":         certificate,
		"services":            services,
		"healthHints":         dashboardHints(s, certificate, services),
	})
}

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
		if err := validateInbound(&inbound); err != nil {
			badRequest(w, err)
			return
		}
		inbound.ID = config.NewID()
		inbound.Enabled = true
		if err := a.mutate(func(s *config.State) error { s.Inbounds = append(s.Inbounds, inbound); return nil }); err != nil {
			serverError(w, err)
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
		writeJSON(w, http.StatusOK, a.store.Snapshot().Clients)
	case http.MethodPatch:
		var incoming config.Client
		if err := decodeJSON(r, &incoming); err != nil {
			badRequest(w, err)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/clients"), "/")
		if len(parts) < 2 || parts[1] == "" {
			badRequest(w, errors.New("客户端 ID 不能为空"))
			return
		}
		id := parts[1]
		if !usernamePattern.MatchString(incoming.Username) || len(incoming.InboundIDs) == 0 {
			badRequest(w, errors.New("客户端用户名或入站绑定无效"))
			return
		}
		if err := a.mutate(func(s *config.State) error {
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
			for _, inboundID := range incoming.InboundIDs {
				protocol, exists := types[inboundID]
				if !exists {
					return errors.New("选择的入站不存在")
				}
				secret := target.Credentials[inboundID]
				if secret == "" {
					if protocol == "shadowsocks2022" {
						secret = config.RandomBase64(32)
					} else {
						secret = config.RandomToken(16)
					}
				}
				credentials[inboundID] = secret
			}
			target.Username = incoming.Username
			target.Note = incoming.Note
			target.InboundIDs = incoming.InboundIDs
			target.Credentials = credentials
			target.TotalLimitBytes = incoming.TotalLimitBytes
			target.MonthlyLimitBytes = incoming.MonthlyLimitBytes
			target.ExpiresAt = incoming.ExpiresAt
			target.MaxOnlineIPs = incoming.MaxOnlineIPs
			return nil
		}); err != nil {
			badRequest(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodPost:
		var client config.Client

		if err := decodeJSON(r, &client); err != nil {
			badRequest(w, err)
			return
		}
		if !usernamePattern.MatchString(client.Username) {
			badRequest(w, errors.New("用户名仅允许 3–32 位字母、数字、下划线或连字符"))
			return
		}
		if len(client.InboundIDs) == 0 {
			badRequest(w, errors.New("至少选择一个入站"))
			return
		}
		client.ID = config.NewID()
		client.CreatedAt = time.Now().UTC()
		client.Month = time.Now().Format("2006-01")
		client.Credentials = map[string]string{}
		err := a.mutate(func(s *config.State) error {
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
				if protocol == "shadowsocks2022" {
					client.Credentials[id] = config.RandomBase64(32)
				} else {
					client.Credentials[id] = config.RandomToken(16)
				}
			}
			s.Clients = append(s.Clients, client)
			return nil
		})
		if err != nil {
			badRequest(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, client)
	default:
		methodNotAllowed(w)
	}
}
func (a *App) clientAction(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/clients/"), "/")
	if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodPatch {
		var incoming config.Client
		if err := decodeJSON(r, &incoming); err != nil {
			badRequest(w, err)
			return
		}
		if err := a.updateClient(parts[0], incoming); err != nil {
			badRequest(w, err)
			return
		}
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
			case "rotate":
				types := make(map[string]string, len(s.Inbounds))
				for _, inbound := range s.Inbounds {
					types[inbound.ID] = inbound.Type
				}
				for inboundID := range c.Credentials {
					if types[inboundID] == "shadowsocks2022" {
						c.Credentials[inboundID] = config.RandomBase64(32)
					} else {
						c.Credentials[inboundID] = config.RandomToken(16)
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

func (a *App) updateClient(id string, incoming config.Client) error {
	if !usernamePattern.MatchString(incoming.Username) || len(incoming.InboundIDs) == 0 {
		return errors.New("客户端用户名或入站绑定无效")
	}
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
		for _, inboundID := range incoming.InboundIDs {
			protocol, exists := types[inboundID]
			if !exists {
				return errors.New("选择的入站不存在")
			}
			secret := target.Credentials[inboundID]
			if secret == "" {
				if protocol == "shadowsocks2022" {
					secret = config.RandomBase64(32)
				} else {
					secret = config.RandomToken(16)
				}
			}
			credentials[inboundID] = secret
		}
		target.Username = incoming.Username
		target.Note = incoming.Note
		target.InboundIDs = incoming.InboundIDs
		target.Credentials = credentials
		target.TotalLimitBytes = incoming.TotalLimitBytes
		target.MonthlyLimitBytes = incoming.MonthlyLimitBytes
		target.ExpiresAt = incoming.ExpiresAt
		target.MaxOnlineIPs = incoming.MaxOnlineIPs
		return nil
	})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, a.store.Snapshot().Settings)
		return
	}
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	var incoming config.Settings
	if err := decodeJSON(r, &incoming); err != nil {
		badRequest(w, err)
		return
	}
	err := a.mutate(func(s *config.State) error {
		if strings.TrimSpace(incoming.PanelName) != "" {
			s.Settings.PanelName = strings.TrimSpace(incoming.PanelName)
		}
		s.Settings.TitleEnabled = incoming.TitleEnabled
		if incoming.RealityCandidates != nil {
			s.Settings.RealityCandidates = incoming.RealityCandidates
		}
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.store.Snapshot().Settings)
}
func (a *App) validateConfig(w http.ResponseWriter, _ *http.Request) {
	if err := proxy.Write(a.store.Snapshot(), a.runtime); err != nil {
		serverError(w, err)
		return
	}
	if filePresent(a.runtime.SingBoxBin) {
		cmd := exec.Command(a.runtime.SingBoxBin, "check", "-c", a.runtime.SingBoxConfig)
		if output, e := cmd.CombinedOutput(); e != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "output": string(output)})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) subscription(w http.ResponseWriter, r *http.Request) {
	username := strings.Trim(path.Base(r.URL.Path), "/")
	a.resetMonth()
	a.syncTraffic()
	state := a.store.Snapshot()
	links, ok := proxy.Subscription(state, a.runtime, username)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var client config.Client
	for _, value := range state.Clients {
		if value.Username == username {
			client = value
			break
		}
	}
	a.setSubscriptionHeaders(w, client, a.subscriptionBaseURL(state.Settings.SubscriptionPath)+"/"+username)
	if strings.Contains(strings.ToLower(r.Header.Get("user-agent")), "mozilla") {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, a.subscriptionPage(client, strings.Count(links, "\n")+1, a.subscriptionBaseURL(state.Settings.SubscriptionPath)+"/"+username))
		return
	}
	w.Header().Set("content-type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, links)
}

func (a *App) setSubscriptionHeaders(w http.ResponseWriter, client config.Client, subscriptionURL string) {
	fields := []string{"upload=" + strconv.FormatInt(client.UploadBytes, 10), "download=" + strconv.FormatInt(client.DownloadBytes, 10)}
	if client.TotalLimitBytes > 0 {
		fields = append(fields, "total="+strconv.FormatInt(client.TotalLimitBytes, 10))
	}
	if client.ExpiresAt != "" {
		if expires, err := time.ParseInLocation("2006-01-02", client.ExpiresAt, time.Local); err == nil {
			fields = append(fields, "expire="+strconv.FormatInt(expires.Unix(), 10))
		}
	}
	w.Header().Set("Subscription-Userinfo", strings.Join(fields, "; "))
	w.Header().Set("Profile-Title", "KotaUI · "+client.Username)
	w.Header().Set("Profile-Update-Interval", "12")
	w.Header().Set("Profile-Web-Page-Url", subscriptionURL)
}

func (a *App) subscriptionPage(client config.Client, linkCount int, subscriptionURL string) string {
	limit := "无限制"
	if client.TotalLimitBytes > 0 {
		limit = formatBytes(client.TotalLimitBytes)
	}
	expires := client.ExpiresAt
	if expires == "" {
		expires = "无到期日"
	}
	return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>KotaUI 订阅</title><style>*{box-sizing:border-box}body{margin:0;background:#f3f6fb;color:#182033;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.wrap{max-width:620px;margin:0 auto;padding:28px 16px 42px}.brand{display:flex;align-items:center;gap:12px;margin:4px 4px 20px}.logo{width:42px;height:42px;border-radius:14px;background:linear-gradient(145deg,#466df1,#6d51d8);color:#fff;display:grid;place-items:center;font-size:22px;box-shadow:0 8px 20px #4569e540}.brand b{display:block;font-size:22px}.brand small{display:block;margin-top:3px;color:#7d8799}.card{background:#fff;border:1px solid #e6eaf1;border-radius:22px;box-shadow:0 12px 30px #1c2d4b0d;overflow:hidden}.head{padding:20px 22px;border-bottom:1px solid #edf0f5;display:flex;justify-content:space-between;align-items:center}.head b{font-size:20px}.badge{padding:6px 10px;border-radius:999px;background:#eaf8f2;color:#17966d;font-size:13px}.table{padding:14px 20px}.row{display:grid;grid-template-columns:110px 1fr;border-bottom:1px solid #edf0f5;padding:14px 0;gap:10px}.row:last-child{border:0}.row span{color:#7d8799}.usage{margin:18px 20px;padding:18px;border-radius:16px;background:linear-gradient(135deg,#eef4ff,#f8f5ff);display:flex;justify-content:space-between;align-items:center}.usage b{font-size:25px}.sub{margin:20px;padding:16px;border:1px solid #e5eaf3;border-radius:16px}.sub label{display:block;color:#7d8799;font-size:13px;margin-bottom:8px}.url{font-size:13px;word-break:break-all;line-height:1.6;color:#273454}.copy{margin-top:12px;width:100%;border:0;border-radius:12px;padding:12px;background:#3569ee;color:#fff;font-size:15px}.foot{text-align:center;color:#8b94a4;font-size:13px;margin-top:22px;line-height:1.7}@media(max-width:420px){.wrap{padding:18px 12px}.row{grid-template-columns:92px 1fr}.usage b{font-size:21px}}</style><body><main class="wrap"><header class="brand"><div class="logo">◌</div><div><b>KotaUI</b><small>轻量 sing-box 订阅服务 · 作者：那么羡慕你</small></div></header><section class="card"><div class="head"><b>订阅信息</b><span class="badge">状态正常</span></div><div class="table"><div class="row"><span>订阅 ID</span><strong>` + htmlEscape(client.Username) + `</strong></div><div class="row"><span>订阅状态</span><strong>正常 · ` + strconv.Itoa(linkCount) + ` 个节点</strong></div><div class="row"><span>累计使用</span><strong>` + formatBytes(client.UsedBytes) + `</strong></div><div class="row"><span>本月使用</span><strong>` + formatBytes(client.MonthlyUsedBytes) + `</strong></div><div class="row"><span>总配额</span><strong>` + limit + `</strong></div><div class="row"><span>有效期</span><strong>` + htmlEscape(expires) + `</strong></div></div><div class="usage"><div><small>流量使用情况</small><br><b>` + formatBytes(client.UsedBytes) + ` / ` + limit + `</b></div><span class="badge">订阅可用</span></div><div class="sub"><label>订阅地址</label><div class="url" id="url">` + htmlEscape(subscriptionURL) + `</div><button class="copy" onclick="navigator.clipboard.writeText(document.querySelector('#url').textContent).then(()=>this.textContent='已复制订阅地址').catch(()=>this.textContent='请手动复制')">复制订阅地址</button></div></section><p class="foot">作者那么羡慕你，仅供学习自用，请勿随意传播。</p></main></body></html>`
}

func (a *App) mutate(fn func(*config.State) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.Update(fn); err != nil {
		return err
	}
	if err := proxy.Write(a.store.Snapshot(), a.runtime); err != nil {
		return err
	}
	if a.runtime.ManageSingBox && filePresent(a.runtime.SingBoxBin) {
		return exec.Command("systemctl", "restart", "kotaui-singbox").Run()
	}
	return nil
}
func (a *App) resetMonth() {
	current := time.Now().Format("2006-01")
	_ = a.store.Update(func(s *config.State) error {
		for i := range s.Clients {
			if s.Clients[i].Month != current {
				s.Clients[i].Month = current
				s.Clients[i].MonthlyUsedBytes = 0
			}
		}
		return nil
	})
}
func (a *App) panelURL() string {
	scheme := "http"
	if filePresent(a.runtime.TLSCert) {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, a.runtime.Domain, a.runtime.PanelPath)
}

func (a *App) subscriptionBaseURL(subscriptionPath string) string {
	scheme := "http"
	if filePresent(a.runtime.TLSCert) {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, a.runtime.Domain, a.runtime.SubscriptionPort, config.NormalizePath(subscriptionPath))
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
		return errors.New("入站名称不能为空")
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
		if v.HandshakePort == 0 {
			v.HandshakePort = 443
		}
		if v.ShortID == "" {
			v.ShortID = config.RandomHex(8)
		}
	case "hysteria2":
		if v.SNI == "" {
			return errors.New("Hysteria 2 需要 TLS server_name")
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
	default:
		return errors.New("仅支持 REALITY、Hysteria 2、Shadowsocks 2022")
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
			for id := range state.Clients[i].Credentials {
				if types[id] == "shadowsocks2022" && !validSS2022Key(state.Clients[i].Credentials[id]) {
					state.Clients[i].Credentials[id] = config.RandomBase64(32)
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

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(target)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func badRequest(w http.ResponseWriter, e error) { http.Error(w, e.Error(), http.StatusBadRequest) }
func serverError(w http.ResponseWriter, e error) {
	http.Error(w, "服务器错误："+e.Error(), http.StatusInternalServerError)
}
func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
}
func filePresent(path string) bool {
	if path == "" {
		return false
	}
	_, e := os.Stat(path)
	return e == nil
}
func htmlEscape(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(v)
}
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
