package app

import (
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/store"
)

//go:embed web/*
var files embed.FS

type App struct {
	runtime             config.Runtime
	store               *store.Store
	key                 []byte
	mu                  sync.Mutex
	coreReloadMu        sync.Mutex
	trafficMu           sync.Mutex
	lastTrafficSync     time.Time
	lastTrafficSave     time.Time
	trafficSyncInterval time.Duration
	updateMu            sync.Mutex
	updateRunning       bool
	updateRunID         string
	updateMessage       string
	settingsApplyMu     sync.Mutex
	settingsApplying    bool
	settingsApplyErr    string
	certificateRenewMu  sync.Mutex
	certificateRenewing bool
	pings               *pingManager
	backgroundOnce      sync.Once
	pingRunning         atomic.Bool
	shaperMu            sync.Mutex
	shaper              *trafficShaper
	shaperLastErr       string
	terminalMu          sync.Mutex
	terminalCount       int
	startedAt           time.Time
	sniProbe            sniProbeFunc
	loginMu             sync.Mutex
	loginFails          map[string]loginAttempt
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
	if err := writeAndValidateConfig(s.Snapshot(), runtime); err != nil {
		return nil, err
	}
	keyHash := sha256.Sum256([]byte(runtime.AdminPassword + "|" + runtime.DataDir))
	// Keep the monotonic clock reading from time.Now(); converting to UTC strips
	// it, which would make panel uptime jump when the system wall clock changes.
	return &App{runtime: runtime, store: s, key: keyHash[:], trafficSyncInterval: 15 * time.Second, pings: newPingManager(runtime.DataDir), startedAt: time.Now(), sniProbe: probeSNI, loginFails: map[string]loginAttempt{}, shaper: newTrafficShaper()}, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc(a.store.Snapshot().Settings.SubscriptionPath+"/", a.subscription)
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/logout", a.logout)
	mux.HandleFunc("/api/state", a.auth(a.state))
	mux.HandleFunc("/api/overview", a.auth(a.dashboard))
	mux.HandleFunc("/api/dashboard", a.auth(a.dashboard))
	mux.HandleFunc("/api/route", a.auth(a.routeTrace))
	mux.HandleFunc("/api/inbounds", a.auth(a.inbounds))
	mux.HandleFunc("/api/inbounds/", a.auth(a.inboundAction))
	mux.HandleFunc("/api/clients", a.auth(a.clients))
	mux.HandleFunc("/api/clients/", a.auth(a.clientAction))
	mux.HandleFunc("/api/site-traffic", a.auth(a.siteTraffic))
	mux.HandleFunc("/api/settings", a.auth(a.settings))
	mux.HandleFunc("/api/settings/apply-status", a.auth(a.settingsApplyStatusHandler))
	mux.HandleFunc("/api/reality/test", a.auth(a.sniTest))
	mux.HandleFunc("/api/reality/test-all", a.auth(a.sniTestAll))
	mux.HandleFunc("/api/services/", a.auth(a.serviceAction))
	mux.HandleFunc("/api/update", a.auth(a.updatePanel))
	mux.HandleFunc("/api/update/status", a.auth(a.updateStatus))
	mux.HandleFunc("/api/certificate/renew", a.auth(a.renewCertificate))
	mux.HandleFunc("/api/certificate/status", a.auth(a.certificateRenewStatus))
	mux.HandleFunc("/api/network-quality", a.auth(a.networkQuality))
	mux.HandleFunc("/api/network-quality/targets", a.auth(a.addPingTarget))
	mux.HandleFunc("/api/network-quality/targets/", a.auth(a.pingTargetAction))
	mux.HandleFunc("/api/logs/", a.auth(a.logs))
	mux.HandleFunc("/api/terminal", a.auth(a.terminal))
	mux.HandleFunc("/assets/overview.css", embeddedAsset("web/overview.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/assets/overview.js", embeddedAsset("web/overview.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/assets/world-land.js", embeddedAsset("web/world-land.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/assets/client-subscription.js", embeddedAsset("web/client-subscription.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/assets/client-expiry.css", embeddedAsset("web/client-expiry.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/assets/tuic.css", embeddedAsset("web/tuic.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/assets/tuic.js", embeddedAsset("web/tuic.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/assets/xterm.js", embeddedAsset("web/xterm.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/assets/xterm.css", embeddedAsset("web/xterm.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/assets/xterm-addon-fit.js", embeddedAsset("web/xterm-addon-fit.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/assets/kotaui-logo.png", embeddedAsset("web/kotaui-logo.png", "image/png"))
	mux.HandleFunc("/favicon.ico", embeddedAsset("web/kotaui-logo.png", "image/png"))
	mux.HandleFunc(a.runtime.PanelPath, a.panel)
	mux.HandleFunc(a.runtime.PanelPath+"/", a.panel)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, a.runtime.PanelPath, http.StatusFound)
	})
	return withSecurityHeaders(mux)
}

func (a *App) Serve() error {
	a.startBackgroundTasks()
	server := &http.Server{Addr: a.runtime.Listen, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	return serveTLS(server, a.runtime)
}

// writeStaticBody 输出面板静态内容：浏览器接受 gzip 时压缩传输（图片
// 本身已压缩，不再压）；不接受 gzip 的客户端原样输出。只影响传输，
// 不改内容与缓存语义。
func writeStaticBody(w http.ResponseWriter, r *http.Request, body []byte, contentType string) {
	w.Header().Set("content-type", contentType)
	if r == nil || r.Method != http.MethodGet ||
		!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
		strings.HasPrefix(contentType, "image/") {
		_, _ = w.Write(body)
		return
	}
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("content-encoding", "gzip")
	gz := gzip.NewWriter(w)
	_, _ = gz.Write(body)
	_ = gz.Close()
}

func (a *App) panel(w http.ResponseWriter, r *http.Request) {
	body, _ := files.ReadFile("web/index.html")
	writeStaticBody(w, r, body, "text/html; charset=utf-8")
}

func embeddedAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := files.ReadFile(name)
		if err != nil {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("cache-control", "no-store")
		writeStaticBody(w, r, body, contentType)
	}
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": config.Version})
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
