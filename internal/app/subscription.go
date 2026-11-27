package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

func (a *App) ServeSubscription() error {
	server := &http.Server{Addr: fmt.Sprintf("0.0.0.0:%d", a.runtime.SubscriptionPort), Handler: a.subscriptionHandler(), ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	return serveTLS(server, a.runtime)
}

func serveTLS(server *http.Server, runtime config.Runtime) error {
	if runtime.TLSCert == "" || runtime.TLSKey == "" {
		return server.ListenAndServe()
	}
	if _, err := os.Stat(runtime.TLSCert); err != nil {
		return fmt.Errorf("TLS 证书不可用：%w", err)
	}
	if _, err := os.Stat(runtime.TLSKey); err != nil {
		return fmt.Errorf("TLS 私钥不可用：%w", err)
	}
	return server.ListenAndServeTLS(runtime.TLSCert, runtime.TLSKey)
}

func (a *App) subscriptionHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(a.store.Snapshot().Settings.SubscriptionPath+"/", a.subscription)
	mux.HandleFunc("/assets/kotaui-logo.png", embeddedAsset("web/kotaui-logo.png", "image/png"))
	mux.HandleFunc("/favicon.ico", embeddedAsset("web/kotaui-logo.png", "image/png"))
	return mux
}

// writeStaticBody 输出面板静态内容：浏览器接受 gzip 时压缩传输（图片
// 本身已压缩，不再压）；不接受 gzip 的客户端原样输出。只影响传输，
// 不改内容与缓存语义。

func subscriptionBrandLogo(page string) string {
	const icon = "/assets/kotaui-logo.png"
	const favicon = "/favicon.ico?v=china-map-v2"
	page = strings.Replace(page, "</title>", `</title><link rel="icon" href="`+favicon+`" type="image/png">`, 1)
	page = strings.Replace(page, "</style>", `.logo{background:#fff!important;border:1px solid #e4ebf4;padding:2px}.logo:after,.logo svg{display:none!important}.logo img{display:block;width:100%;height:100%;object-fit:contain;transform:scale(1.32)}</style>`, 1)
	return strings.Replace(page, `<div class="logo">`, `<div class="logo"><img src="`+icon+`" alt="KotaUI">`, 1)
}

func (a *App) subscription(w http.ResponseWriter, r *http.Request) {
	a.resetMonth()
	a.syncTraffic()
	state := a.store.Snapshot()
	prefix := config.NormalizePath(state.Settings.SubscriptionPath) + "/"
	subscriptionID := strings.TrimPrefix(r.URL.Path, prefix)
	var client config.Client
	found := false
	for _, value := range state.Clients {
		if clientSubscriptionID(value) == subscriptionID {
			client = value
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	links, ok := proxy.Subscription(state, a.runtime, client.Username)
	if !ok {
		http.NotFound(w, r)
		return
	}
	subscriptionURL := a.subscriptionBaseURL(state.Settings.SubscriptionPath) + "/" + clientSubscriptionID(client)
	a.setSubscriptionHeaders(w, client, subscriptionURL)
	if strings.Contains(strings.ToLower(r.Header.Get("user-agent")), "mozilla") {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, subscriptionBrandLogo(a.subscriptionPage(client, strings.Count(links, "\n")+1, subscriptionURL)))
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
		if expires, err := time.ParseInLocation("2006-01-02", client.ExpiresAt, config.PanelLocation); err == nil {
			expires = expires.AddDate(0, 0, 1).Add(-time.Second)
			fields = append(fields, "expire="+strconv.FormatInt(expires.Unix(), 10))
		}
	}
	w.Header().Set("Subscription-Userinfo", strings.Join(fields, "; "))
	w.Header().Set("Profile-Title", client.Username)
	w.Header().Set("profile-title", client.Username)
	w.Header().Set("Profile-Update-Interval", "12")
	w.Header().Set("Profile-Web-Page-Url", subscriptionURL)
}

func (a *App) subscriptionPage(client config.Client, linkCount int, subscriptionURL string) string {
	remainingTotal := "无限制"
	if client.TotalLimitBytes > 0 {
		remaining := client.TotalLimitBytes - client.UsedBytes
		if remaining < 0 {
			remaining = 0
		}
		remainingTotal = formatBytes(remaining)
	}
	expires := client.ExpiresAt
	if expires == "" {
		expires = "无到期日"
	}
	return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + htmlEscape(client.Username) + `</title><style>*{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 8% 6%,#eaf1ff,transparent 30%),#f5f8fc;color:#15223a;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC",sans-serif}.wrap{max-width:640px;margin:0 auto;padding:30px 16px 46px}.brand{display:flex;align-items:center;gap:12px;margin:4px 4px 20px}.logo{width:44px;height:44px;border-radius:15px;background:linear-gradient(145deg,#3077ef,#7562e7);color:#fff;display:grid;place-items:center;box-shadow:0 10px 24px #4e73de38;position:relative;overflow:hidden}.logo:after{content:"";position:absolute;inset:5px;border:1px solid #ffffff42;border-radius:11px}.logo svg{position:relative;z-index:1}.brand b{display:block;font-size:22px;letter-spacing:.1px}.brand small{display:block;margin-top:3px;color:#78869a}.card{background:#fff;border:1px solid #e4ebf4;border-radius:22px;box-shadow:0 14px 34px #2334510c;overflow:hidden}.head{padding:20px 22px;border-bottom:1px solid #edf1f6;display:flex;justify-content:space-between;align-items:center}.head b{font-size:20px}.badge{display:inline-flex;align-items:center;gap:6px;padding:6px 10px;border-radius:999px;background:#eaf8f2;color:#168c69;font-size:13px;font-weight:700}.badge i{width:7px;height:7px;border-radius:50%;background:currentColor;box-shadow:0 0 0 4px #dff7ed;animation:lamp 1.8s ease-in-out infinite}@keyframes lamp{0%,100%{opacity:.82;transform:scale(.94);box-shadow:0 0 0 4px #dff7ed}50%{opacity:1;transform:scale(1);box-shadow:0 0 0 7px #d5f7e8}}.table{padding:10px 20px}.row{display:grid;grid-template-columns:110px 1fr;border-bottom:1px solid #edf1f6;padding:13px 0;gap:10px}.row:last-child{border:0}.row span{color:#78869a;font-size:13px}.row strong{font-variant-numeric:tabular-nums}.usage{margin:18px 20px;padding:18px;border-radius:17px;background:linear-gradient(135deg,#eef4ff,#f7f4ff);display:flex;justify-content:space-between;align-items:center;gap:12px}.usage small{color:#6f7e94}.usage b{font-size:24px;letter-spacing:-.3px}.sub{margin:20px;padding:16px;border:1px solid #e5eaf3;border-radius:16px}.sub label{display:block;color:#7d8799;font-size:13px;margin-bottom:8px}.url{font-size:13px;word-break:break-all;line-height:1.6;color:#273454}.copy{margin-top:12px;width:100%;border:0;border-radius:12px;padding:12px;background:#3569ee;color:#fff;font-size:15px}.foot{text-align:center;color:#8b94a4;font-size:13px;margin-top:22px;line-height:1.7}@media(max-width:420px){.wrap{padding:18px 12px}.row{grid-template-columns:92px 1fr}.usage b{font-size:21px}}</style><body><main class="wrap"><header class="brand"><div class="logo"><svg width="25" height="25" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><circle cx="6" cy="12" r="2.4"/><circle cx="18" cy="12" r="2.4"/><path d="M8.5 12h2.2c1.2 0 1.6-2.8 2.8-2.8h1.1M8.5 12h2.2c1.2 0 1.6 2.8 2.8 2.8h1.1"/><path d="M3.5 7.2v9.6M20.5 7.2v9.6" opacity=".55"/></svg></div><div><b>KotaUI</b><small>轻量 sing-box 订阅服务</small></div></header><section class="card"><div class="head"><b>订阅信息</b><span class="badge"><i></i>连接正常</span></div><div class="table"><div class="row"><span>订阅 ID</span><strong>` + htmlEscape(client.Username) + `</strong></div><div class="row"><span>订阅状态</span><strong>正常 · ` + strconv.Itoa(linkCount) + ` 个节点</strong></div><div class="row"><span>用流量</span><strong>` + formatBytes(client.UsedBytes) + `</strong></div><div class="row"><span>剩余总流量</span><strong>` + remainingTotal + `</strong></div><div class="row"><span>到期日</span><strong>` + htmlEscape(expires) + `</strong></div></div><div class="usage"><div><small>用流量</small><br><b>` + formatBytes(client.UsedBytes) + `</b><br><small>剩余总流量 ` + remainingTotal + `</small></div><span class="badge"><i></i>订阅可用</span></div><div class="sub"><label>订阅地址</label><div class="url" id="url">` + htmlEscape(subscriptionURL) + `</div><button class="copy" onclick="navigator.clipboard.writeText(document.querySelector('#url').textContent).then(()=>this.textContent='已复制订阅地址').catch(()=>this.textContent='请手动复制')">复制订阅地址</button></div></section></main></body></html>`
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
