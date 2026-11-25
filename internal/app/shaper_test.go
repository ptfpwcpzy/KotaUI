package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

// fakeRunner 记录 tc/ip/modprobe 调用，按命令返回预设输出或错误。
type fakeRunner struct {
	calls    []string
	routeOut string
	qdiscOut string
	fail     map[string]error
}

func (f *fakeRunner) run(name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, cmd)
	if err, ok := f.fail[cmd]; ok {
		return "", err
	}
	switch {
	case strings.HasPrefix(cmd, "ip route "):
		return f.routeOut, nil
	case strings.HasPrefix(cmd, "tc qdisc show "):
		return f.qdiscOut, nil
	case strings.HasPrefix(cmd, "ip link show "):
		return "2: ifb0: <BROADCAST,NOARP,UP,LOWER_UP> mtu 1500", nil
	}
	return "", nil
}

func (f *fakeRunner) has(substr string) bool {
	for _, call := range f.calls {
		if strings.Contains(call, substr) {
			return true
		}
	}
	return false
}

func TestKbpsToTCRate(t *testing.T) {
	if got := kbpsToTCRate(100); got != "800kbit" {
		t.Fatalf("100 KB/s 应为 800kbit，得到 %s", got)
	}
	if got := kbpsToTCRate(1); got != "8kbit" {
		t.Fatalf("1 KB/s 应为 8kbit，得到 %s", got)
	}
}

func TestParseDefaultRoute(t *testing.T) {
	out := "default via 10.0.0.1 dev eth0 proto dhcp metric 100\n10.0.0.0/24 dev eth0 proto kernel scope link src 10.0.0.5\n"
	if got := parseDefaultRoute(out); got != "eth0" {
		t.Fatalf("期望 eth0，得到 %q", got)
	}
	if got := parseDefaultRoute("10.0.0.0/24 dev eth0 proto kernel scope link\n"); got != "" {
		t.Fatalf("无默认路由应返回空，得到 %q", got)
	}
}

func TestForeignRootQdisc(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"", false},
		{"qdisc noqueue 0: root refcnt 2 limit 1000p", false},
		{"qdisc htb 1: root refcnt 9 r2q 10 default 0x1e direct_packets_stat 0", false},
		{"qdisc fq_codel 0: root refcnt 2 limit 10240p flows 1024", true},
		{"qdisc ingress ffff: parent ffff:fff1 ----------------", false},
		{"qdisc htb 1: root refcnt 9 default 0x1e\nqdisc ingress ffff: parent ffff:fff1 ----------------", false},
	}
	for _, tc := range cases {
		if got := foreignRootQdisc(tc.out); got != tc.want {
			t.Fatalf("foreignRootQdisc(%q) = %v，期望 %v", tc.out, got, tc.want)
		}
	}
}

func TestEffectiveSpeedLimitKBps(t *testing.T) {
	if got := (config.Client{}).EffectiveSpeedLimitKBps(); got != 0 {
		t.Fatalf("未开启应为 0，得到 %d", got)
	}
	if got := (config.Client{SpeedLimitEnabled: true}).EffectiveSpeedLimitKBps(); got != config.DefaultSpeedLimitKBps {
		t.Fatalf("开启未填速度应为默认值 %d，得到 %d", config.DefaultSpeedLimitKBps, got)
	}
	if got := (config.Client{SpeedLimitEnabled: true, SpeedLimitKBps: 50}).EffectiveSpeedLimitKBps(); got != 50 {
		t.Fatalf("应为 50，得到 %d", got)
	}
}

func TestShaperSyncEmptyIsNoop(t *testing.T) {
	fake := &fakeRunner{}
	shaper := newTrafficShaper()
	shaper.run = fake.run
	if err := shaper.Sync(map[string]int{}); err != nil {
		t.Fatalf("空同步不应出错：%v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("空同步不应调任何命令，调了 %v", fake.calls)
	}
}

func TestShaperSyncAddsRules(t *testing.T) {
	fake := &fakeRunner{
		routeOut: "default via 10.0.0.1 dev eth0 proto dhcp\n",
		qdiscOut: "qdisc noqueue 0: root refcnt 2 limit 1000p\n",
	}
	shaper := newTrafficShaper()
	shaper.run = fake.run
	desired := map[string]int{"1.2.3.4": 100, "2001:db8::1": 100}
	if err := shaper.Sync(desired); err != nil {
		t.Fatalf("同步失败：%v", err)
	}
	want := []string{
		"ip route show default",
		"tc qdisc show dev eth0",
		"modprobe ifb",
		"ip link add ifb0 type ifb",
		"ip link set dev ifb0 up",
		"tc qdisc add dev eth0 handle ffff: ingress",
		"tc filter add dev eth0 parent ffff: protocol all u32 match u32 0 0 action mirred egress redirect dev ifb0",
		"tc qdisc del dev eth0 root",
		"tc qdisc add dev eth0 root handle 1: htb default 30",
		"tc class add dev eth0 parent 1: classid 1:1 htb rate 10gbit ceil 10gbit",
		"tc class add dev eth0 parent 1:1 classid 1:30 htb rate 10gbit ceil 10gbit",
		"tc class add dev eth0 parent 1:1 classid 1:10 htb rate 800kbit ceil 800kbit",
		"tc filter add dev eth0 protocol ip parent 1:0 prio 1 u32 match ip dst 1.2.3.4/32 flowid 1:10",
		"tc class add dev eth0 parent 1:1 classid 1:11 htb rate 800kbit ceil 800kbit",
		"tc filter add dev eth0 protocol ipv6 parent 1:0 prio 1 u32 match ip6 dst 2001:db8::1/128 flowid 1:11",
		"tc qdisc del dev ifb0 root",
		"tc qdisc add dev ifb0 root handle 2: htb default 30",
		"tc class add dev ifb0 parent 2: classid 2:1 htb rate 10gbit ceil 10gbit",
		"tc class add dev ifb0 parent 2:1 classid 2:30 htb rate 10gbit ceil 10gbit",
		"tc class add dev ifb0 parent 2:1 classid 2:10 htb rate 800kbit ceil 800kbit",
		"tc filter add dev ifb0 protocol ip parent 2:0 prio 1 u32 match ip src 1.2.3.4/32 flowid 2:10",
		"tc class add dev ifb0 parent 2:1 classid 2:11 htb rate 800kbit ceil 800kbit",
		"tc filter add dev ifb0 protocol ipv6 parent 2:0 prio 1 u32 match ip6 src 2001:db8::1/128 flowid 2:11",
	}
	for _, w := range want {
		if !fake.has(w) {
			t.Fatalf("缺少命令：%s\n实际调用：\n%s", w, strings.Join(fake.calls, "\n"))
		}
	}
	// 同样内容再同步一次：不应再调任何命令。
	before := len(fake.calls)
	if err := shaper.Sync(desired); err != nil {
		t.Fatalf("重复同步失败：%v", err)
	}
	if len(fake.calls) != before {
		t.Fatalf("无变化时不应再调命令，多了 %v", fake.calls[before:])
	}
}

func TestShaperSyncTeardown(t *testing.T) {
	fake := &fakeRunner{
		routeOut: "default via 10.0.0.1 dev eth0 proto dhcp\n",
		qdiscOut: "qdisc noqueue 0: root refcnt 2 limit 1000p\n",
	}
	shaper := newTrafficShaper()
	shaper.run = fake.run
	if err := shaper.Sync(map[string]int{"1.2.3.4": 100}); err != nil {
		t.Fatalf("同步失败：%v", err)
	}
	fake.calls = nil
	// 注意：teardown 前先让 qdisc show 返回我们自己的 HTB，否则会被当成别人的而不拆。
	fake.qdiscOut = "qdisc htb 1: root refcnt 9 r2q 10 default 0x1e\n"
	if err := shaper.Sync(map[string]int{}); err != nil {
		t.Fatalf("清空同步失败：%v", err)
	}
	for _, w := range []string{
		"tc qdisc del dev eth0 root",
		"tc qdisc del dev eth0 ingress",
		"tc qdisc del dev ifb0 root",
	} {
		if !fake.has(w) {
			t.Fatalf("拆规则时缺少命令：%s\n实际调用：\n%s", w, strings.Join(fake.calls, "\n"))
		}
	}
	if shaper.HasApplied() {
		t.Fatal("清空后不应还有生效的规则")
	}
}

func TestShaperSyncForeignQdiscRefuses(t *testing.T) {
	fake := &fakeRunner{
		routeOut: "default via 10.0.0.1 dev eth0 proto dhcp\n",
		qdiscOut: "qdisc fq_codel 0: root refcnt 2 limit 10240p flows 1024\n",
	}
	shaper := newTrafficShaper()
	shaper.run = fake.run
	if err := shaper.Sync(map[string]int{"1.2.3.4": 100}); err == nil {
		t.Fatal("网卡已有别人规则时应拒绝")
	}
	if fake.has("tc qdisc add dev eth0 root") || fake.has("tc qdisc del dev eth0 root") {
		t.Fatalf("拒绝时不应动网卡根 qdisc，实际调用：\n%s", strings.Join(fake.calls, "\n"))
	}
}

func TestShaperSyncIFBFailureStillShapesDownload(t *testing.T) {
	fake := &fakeRunner{
		routeOut: "default via 10.0.0.1 dev eth0 proto dhcp\n",
		qdiscOut: "qdisc noqueue 0: root refcnt 2 limit 1000p\n",
		fail: map[string]error{
			"ip link add ifb0 type ifb": errTest,
			"ip link show dev ifb0":     errTest,
		},
	}
	shaper := newTrafficShaper()
	shaper.run = fake.run
	if err := shaper.Sync(map[string]int{"1.2.3.4": 100}); err != nil {
		t.Fatalf("IFB 失败时下载限速仍应成功：%v", err)
	}
	if !fake.has("tc filter add dev eth0 protocol ip parent 1:0 prio 1 u32 match ip dst 1.2.3.4/32 flowid 1:10") {
		t.Fatal("IFB 失败时出口限速规则应仍建立")
	}
	if fake.has("classid 2:10") || fake.has("tc qdisc add dev ifb0 root") {
		t.Fatal("IFB 失败时不应建 ifb0 的限速规则")
	}
}

var errTest = errTestType("test error")

type errTestType string

func (e errTestType) Error() string { return string(e) }

func TestParseLiveConnections(t *testing.T) {
	body := `{"connections":[
		{"metadata":{"user":"zwj","sourceIP":"1.2.3.4"}},
		{"metadata":{"user":"zwj","sourceIP":"1.2.3.4"}},
		{"metadata":{"user":"zwj","sourceIP":"2001:db8::1"}},
		{"metadata":{"user":"tsy","sourceIP":"5.6.7.8"}},
		{"metadata":{"user":"","sourceIP":"9.9.9.9"}},
		{"metadata":{"sourceIP":"9.9.9.9"}},
		{"metadata":{"user":"usa","sourceIP":"not-an-ip"}}
	]}`
	var snapshot struct {
		Connections []liveConnection `json:"connections"`
	}
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	got := parseLiveConnections(snapshot.Connections)
	if len(got["zwj"]) != 2 || got["zwj"][0] != "1.2.3.4" || got["zwj"][1] != "2001:db8::1" {
		t.Fatalf("zwj 的 IP 归属不对：%v", got["zwj"])
	}
	if len(got["tsy"]) != 1 || got["tsy"][0] != "5.6.7.8" {
		t.Fatalf("tsy 的 IP 归属不对：%v", got["tsy"])
	}
	if _, ok := got["usa"]; ok {
		t.Fatalf("非法 IP 不应归属：%v", got)
	}
	if len(got) != 2 {
		t.Fatalf("只应有 2 个用户，得到 %v", got)
	}
}
