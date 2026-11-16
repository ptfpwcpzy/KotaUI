package app

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSplitHostAddresses(t *testing.T) {
	ipv4, ipv6 := splitHostAddresses([]string{
		"10.0.0.2/24",
		"192.168.1.10/24",
		"10.0.0.2/24",
		"127.0.0.1/8",
		"169.254.1.2/16",
		"2001:db8::2/64",
		"2001:db8::1%eth0/64",
		"::1/128",
		"fe80::1%eth0/64",
		"ff02::1/64",
		"not-an-ip",
		"",
	})
	want4 := []string{"10.0.0.2", "192.168.1.10"}
	want6 := []string{"2001:db8::1", "2001:db8::2"}
	if !reflect.DeepEqual(ipv4, want4) {
		t.Fatalf("ipv4 = %v, want %v", ipv4, want4)
	}
	if !reflect.DeepEqual(ipv6, want6) {
		t.Fatalf("ipv6 = %v, want %v", ipv6, want6)
	}
}

// TestDashboardRefreshSkipsUnchangedSections 保证仪表盘 2 秒刷新时，内容没
// 变的区块不再重写 innerHTML（重写会把用户正在选中的文字打断，主机地址
// 和流量详情里的网站选不中都是这么来的）。四个区块和流量详情区都要守，
// 首屏渲染也要把主机地址条算进去、并记下初始内容，否则第一次刷新仍会重写。
func TestDashboardRefreshSkipsUnchangedSections(t *testing.T) {
	raw, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, want := range []string{
		"if(last.gauge!==parts.gauge)gauge.innerHTML=parts.gauge;",
		"if(last.main!==parts.main)main.innerHTML=parts.main;",
		"if(last.strips!==parts.strips)strip.innerHTML=parts.strips;",
		"if(notes&&last.notices!==parts.notices)notes.innerHTML=parts.notices;",
		"lastDashParts=parts;",
		`<div class="dashboard-strips">${health}${online}${hostStrip}</div>`,
		"lastDashParts={gauge:gaugeHTML,main:mainHTML,strips:health+online+hostStrip,notices};",
		"if(html!==lastWeekDetailHTML){el.innerHTML=html;lastWeekDetailHTML=html}",
		"lastWeekDetailHTML=weekDetailHTML();",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("web/index.html missing %q", want)
		}
	}
}

// TestHostAddressCopyOnClick 保证主机地址每个地址都能点一下复制，且拖选
// 文字松开时不会误触发复制；没有地址时显示的“无”不带点击复制。
func TestHostAddressCopyOnClick(t *testing.T) {
	raw, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, want := range []string{
		`<span onclick="copyText(this)" title="点击复制">${esc(a)}</span>`,
		"function copyText(el){const sel=getSelection();if(sel&&!sel.isCollapsed)return;",
		"navigator.clipboard.writeText(el.textContent)",
		"||'<span>无</span>'",
		".host-addrs span[onclick]{cursor:pointer}",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("web/index.html missing %q", want)
		}
	}
}

// TestWeekSiteDomainCopyOnClick 保证流量详情里每条网站行的域名都能点一下复制。
func TestWeekSiteDomainCopyOnClick(t *testing.T) {
	raw, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(raw)
	for _, want := range []string{
		`<span class="week-site-dm" onclick="copyText(this)" title="点击复制">${esc(s.domain)}</span>`,
		".week-site-dm[onclick]{cursor:pointer}",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("web/index.html missing %q", want)
		}
	}
}
