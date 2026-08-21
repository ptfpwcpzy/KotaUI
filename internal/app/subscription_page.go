package app

import (
	"strconv"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

// subscriptionPage deliberately uses one compact summary strip rather than pseudo-nodes.
// The raw subscription remains protocol links only, so clients that ignore metadata keep a clean node list.
func (a *App) subscriptionPage(client config.Client, linkCount int, subscriptionURL string) string {
	totalLimit := "无限制"
	totalRemaining := "无限制"
	if client.TotalLimitBytes > 0 {
		totalLimit = formatBytes(client.TotalLimitBytes)
		remaining := client.TotalLimitBytes - client.UsedBytes
		if remaining < 0 {
			remaining = 0
		}
		totalRemaining = formatBytes(remaining)
	}

	monthlyLimit := "无限制"
	monthlyRemaining := "无限制"
	if client.MonthlyLimitBytes > 0 {
		monthlyLimit = formatBytes(client.MonthlyLimitBytes)
		remaining := client.MonthlyLimitBytes - client.MonthlyUsedBytes
		if remaining < 0 {
			remaining = 0
		}
		monthlyRemaining = formatBytes(remaining)
	}

	expires := client.ExpiresAt
	if expires == "" {
		expires = "不限"
	}

	return `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>KotaUI 订阅</title><style>*{box-sizing:border-box}body{margin:0;background:#f6f9fc;color:#182536;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC",sans-serif}.wrap{max-width:640px;margin:0 auto;padding:26px 16px 42px}.brand{display:flex;align-items:center;justify-content:space-between;margin:2px 2px 16px}.identity{display:flex;align-items:center;gap:10px;min-width:0}.logo{width:38px;height:38px;flex:none;border-radius:12px}.brand b{display:block;font-size:22px;letter-spacing:.1px}.brand small{display:block;margin-top:4px;color:#7a8798;font-size:13px}.state{display:inline-flex;align-items:center;gap:7px;color:#168c69;font-size:13px;font-weight:700}.state i{width:7px;height:7px;border-radius:50%;background:currentColor;box-shadow:0 0 0 4px #dff7ed}.card{background:#fff;border:1px solid #e5ebf2;border-radius:18px;box-shadow:0 10px 26px #24344b0a;overflow:hidden}.summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));border-bottom:1px solid #edf1f5}.metric{min-width:0;padding:18px 15px}.metric+.metric{border-left:1px solid #edf1f5}.metric span{display:block;color:#7b8797;font-size:12px;margin-bottom:7px}.metric strong{display:block;font-size:17px;letter-spacing:-.25px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-variant-numeric:tabular-nums}.detail{padding:14px 17px;color:#657387;font-size:13px;line-height:1.75}.detail b{color:#27364a;font-weight:650}.sub{margin:16px;border:1px solid #e7ecf3;border-radius:14px;padding:14px}.sub label{display:block;color:#7a8797;font-size:12px;margin-bottom:7px}.url{font-size:13px;word-break:break-all;line-height:1.6;color:#2b3a51}.copy{margin-top:12px;width:100%;border:0;border-radius:10px;padding:11px;background:#2f6fdf;color:#fff;font-size:14px;font-weight:650}.foot{text-align:center;color:#8a95a4;font-size:12px;margin-top:19px;line-height:1.7}@media(max-width:420px){.wrap{padding:18px 12px 34px}.metric{padding:15px 11px}.metric strong{font-size:15px}.brand b{font-size:20px}.logo{width:34px;height:34px}}</style><body><main class="wrap"><header class="brand"><div class="identity"><span class="logo"><img src="/assets/kotaui-logo.png" alt="KotaUI"></span><div><b>KotaUI · ` + htmlEscape(client.Username) + `</b><small>` + strconv.Itoa(linkCount) + ` 个真实节点 · 订阅摘要</small></div></div><span class="state"><i></i>可用</span></header><section class="card"><div class="summary"><div class="metric"><span>本月余量</span><strong>` + monthlyRemaining + `</strong></div><div class="metric"><span>总余量</span><strong>` + totalRemaining + `</strong></div><div class="metric"><span>有效期</span><strong>` + htmlEscape(expires) + `</strong></div></div><div class="detail"><b>本月</b> ` + formatBytes(client.MonthlyUsedBytes) + ` / ` + monthlyLimit + `　·　<b>累计</b> ` + formatBytes(client.UsedBytes) + ` / ` + totalLimit + `</div><div class="sub"><label>订阅地址</label><div class="url" id="url">` + htmlEscape(subscriptionURL) + `</div><button class="copy" onclick="navigator.clipboard.writeText(document.querySelector('#url').textContent).then(()=>this.textContent='已复制订阅地址').catch(()=>this.textContent='请手动复制')">复制订阅地址</button></div></section><p class="foot">作者那么羡慕你，仅供学习自用，请勿随意传播。</p></main></body></html>`
}
