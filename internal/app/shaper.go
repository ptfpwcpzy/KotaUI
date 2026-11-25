package app

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// 客户端限速用 Linux tc + HTB 实现：sing-box 核心没有按用户限速，
// 面板直接在服务器网卡上按用户在线 IP 限速。
//
// 方向（从服务器看）：
//   - 下载（服务器发给用户）是网卡出口流量，直接在主网卡上挂 HTB 限速；
//   - 上传（用户发给服务器）是进来的流量，没法直接限，先经 ingress qdisc
//     重定向到虚拟网卡 IFB 上再限速。IFB 不可用时只限下载，网页打开慢
//     这个目标不受影响。
//
// 规则只在有人被限速时才建，没人限速时面板不碰任何 tc 状态；
// 每次同步先比对期望集合，没变化就不调任何 tc 命令；
// 有变化时整棵 HTB 重建（删根重建），结构简单且能自我修复漂移。
// 网卡上已有别人的 tc 规则时直接拒绝，不动别人的配置。

const (
	shaperRootHandle = "1:"
	shaperIFBHandle  = "2:"
	shaperIFBDevice  = "ifb0"
	shaperFirstClass = 10
	// shaperUnlimitedRate 是默认类的速率：不限速的用户走这个类，基本等于不限。
	shaperUnlimitedRate = "10gbit"
)

type trafficShaper struct {
	mu sync.Mutex
	// run 执行一条外部命令，返回合并输出；测试时可替换成记录调用的假实现。
	run func(name string, args ...string) (string, error)

	iface           string
	ifbReady        bool
	ifbFailedLogged bool
	applied         map[string]int // ip -> kbps，已生效的规则
}

func newTrafficShaper() *trafficShaper {
	return &trafficShaper{
		run: func(name string, args ...string) (string, error) {
			out, err := exec.Command(name, args...).CombinedOutput()
			return string(out), err
		},
		applied: map[string]int{},
	}
}

func (s *trafficShaper) HasApplied() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.applied) > 0
}

// Sync 把内核 tc 规则同步成 desired（ip -> KB/s）。没变化时不调任何命令；
// desired 为空时把我们建的规则拆干净，网卡恢复原样。
func (s *trafficShaper) Sync(desired map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sameSpeedMap(desired, s.applied) {
		return nil
	}
	if len(desired) == 0 {
		s.teardownLocked()
		return nil
	}
	if err := s.setupLocked(); err != nil {
		return err
	}
	if err := s.rebuildLocked(desired); err != nil {
		return err
	}
	s.applied = cloneSpeedMap(desired)
	return nil
}

// teardownLocked 拆掉我们建的全部限速规则：主网卡根 qdisc 与入向 qdisc、
// ifb0 根 qdisc。网卡根 qdisc 不是我们自己的时不动它。
func (s *trafficShaper) teardownLocked() {
	if s.iface != "" {
		if out, err := s.run("tc", "qdisc", "show", "dev", s.iface); err == nil && !foreignRootQdisc(out) {
			s.run("tc", "qdisc", "del", "dev", s.iface, "root")
			s.run("tc", "qdisc", "del", "dev", s.iface, "ingress")
		}
	}
	s.run("tc", "qdisc", "del", "dev", shaperIFBDevice, "root")
	s.ifbReady = false
	s.applied = map[string]int{}
}

func sameSpeedMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for ip, kbps := range a {
		if b[ip] != kbps {
			return false
		}
	}
	return true
}

func cloneSpeedMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for ip, kbps := range in {
		out[ip] = kbps
	}
	return out
}

func sortedSpeedIPs(desired map[string]int) []string {
	ips := make([]string, 0, len(desired))
	for ip := range desired {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips
}

// kbpsToTCRate 把 KB/s 换成 tc 的速率写法：100 KB/s = 800 kbit/s。
func kbpsToTCRate(kbps int) string {
	return fmt.Sprintf("%dkbit", kbps*8)
}

// setupLocked 确认出口网卡与 IFB 就绪。IFB 失败不返回错误（只记一次
// 日志），调用方继续做下载限速。
func (s *trafficShaper) setupLocked() error {
	if s.iface == "" {
		iface, err := s.detectInterface()
		if err != nil {
			return err
		}
		s.iface = iface
	}
	out, err := s.run("tc", "qdisc", "show", "dev", s.iface)
	if err != nil {
		return fmt.Errorf("读取网卡 %s 的 tc 状态失败：%w", s.iface, err)
	}
	if foreignRootQdisc(out) {
		return fmt.Errorf("网卡 %s 上已有其他 tc 规则，限速未启用", s.iface)
	}
	if !s.ifbReady {
		if err := s.setupIngressLocked(); err != nil {
			if !s.ifbFailedLogged {
				log.Printf("上传限速不可用（IFB 未就绪）：%v；下载限速不受影响", err)
				s.ifbFailedLogged = true
			}
		}
	}
	return nil
}

// detectInterface 从默认路由里找出口网卡。
func (s *trafficShaper) detectInterface() (string, error) {
	out, err := s.run("ip", "route", "show", "default")
	if err != nil {
		return "", fmt.Errorf("查询默认路由失败：%w", err)
	}
	iface := parseDefaultRoute(out)
	if iface == "" {
		return "", errors.New("找不到默认路由网卡，限速未启用")
	}
	return iface, nil
}

func parseDefaultRoute(output string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		for i, field := range fields {
			if field == "dev" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}

// foreignRootQdisc 判断网卡根 qdisc 是不是别人配的：noqueue 表示没配过，
// htb 1: 是我们自己的，其余一律视为别人的，不碰。
func foreignRootQdisc(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, " root ") {
			continue
		}
		if strings.Contains(line, "noqueue") {
			continue
		}
		if strings.Contains(line, "htb 1:") {
			continue
		}
		return true
	}
	return false
}

// setupIngressLocked 准备 IFB：加载模块（编进内核时 modprobe 会失败，
// 忽略）、建 ifb0、把主网卡的入向流量重定向到 ifb0。
func (s *trafficShaper) setupIngressLocked() error {
	s.run("modprobe", "ifb")
	if _, err := s.run("ip", "link", "add", shaperIFBDevice, "type", "ifb"); err != nil {
		if out, err2 := s.run("ip", "link", "show", "dev", shaperIFBDevice); err2 != nil || !strings.Contains(out, shaperIFBDevice) {
			return fmt.Errorf("创建 IFB 设备失败：%v", err)
		}
	}
	if err := s.runErr("ip", "link", "set", "dev", shaperIFBDevice, "up"); err != nil {
		return err
	}
	out, err := s.run("tc", "qdisc", "show", "dev", s.iface)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "ingress ffff:") {
		if err := s.runErr("tc", "qdisc", "add", "dev", s.iface, "handle", "ffff:", "ingress"); err != nil {
			return err
		}
		if err := s.runErr("tc", "filter", "add", "dev", s.iface, "parent", "ffff:", "protocol", "all", "u32", "match", "u32", "0", "0", "action", "mirred", "egress", "redirect", "dev", shaperIFBDevice); err != nil {
			return err
		}
	}
	s.ifbReady = true
	return nil
}

// rebuildLocked 删根重建两棵 HTB：主网卡管下载，ifb0 管上传。
// 每棵树都是根类 10gbit + 默认类 30（不限速的流量走这里）+ 每个 IP 一个类。
func (s *trafficShaper) rebuildLocked(desired map[string]int) error {
	iface := s.iface
	s.run("tc", "qdisc", "del", "dev", iface, "root")
	if err := s.addHTBSkeletonLocked(iface, shaperRootHandle); err != nil {
		return fmt.Errorf("重建出口限速失败：%w", err)
	}
	if err := s.addIPRulesLocked(iface, shaperRootHandle, desired, false); err != nil {
		return fmt.Errorf("重建出口限速失败：%w", err)
	}
	if s.ifbReady {
		s.run("tc", "qdisc", "del", "dev", shaperIFBDevice, "root")
		if err := s.addHTBSkeletonLocked(shaperIFBDevice, shaperIFBHandle); err != nil {
			return fmt.Errorf("重建上传限速失败：%w", err)
		}
		if err := s.addIPRulesLocked(shaperIFBDevice, shaperIFBHandle, desired, true); err != nil {
			return fmt.Errorf("重建上传限速失败：%w", err)
		}
	}
	return nil
}

func (s *trafficShaper) addHTBSkeletonLocked(dev, handle string) error {
	if err := s.runErr("tc", "qdisc", "add", "dev", dev, "root", "handle", handle, "htb", "default", "30"); err != nil {
		return err
	}
	if err := s.runErr("tc", "class", "add", "dev", dev, "parent", handle, "classid", handle+"1", "htb", "rate", shaperUnlimitedRate, "ceil", shaperUnlimitedRate); err != nil {
		return err
	}
	return s.runErr("tc", "class", "add", "dev", dev, "parent", handle+"1", "classid", handle+"30", "htb", "rate", shaperUnlimitedRate, "ceil", shaperUnlimitedRate)
}

// addIPRulesLocked 给每个 IP 建一个 HTB 类并挂 filter：出口按目标 IP
// 匹配（服务器发给用户的包），IFB 上按来源 IP 匹配（用户发来的包）。
func (s *trafficShaper) addIPRulesLocked(dev, handle string, desired map[string]int, ingress bool) error {
	classID := shaperFirstClass
	for _, ip := range sortedSpeedIPs(desired) {
		rate := kbpsToTCRate(desired[ip])
		flow := fmt.Sprintf("%s%d", handle, classID)
		if err := s.runErr("tc", "class", "add", "dev", dev, "parent", handle+"1", "classid", flow, "htb", "rate", rate, "ceil", rate); err != nil {
			return err
		}
		if err := s.addIPFilterLocked(dev, handle, ip, flow, ingress); err != nil {
			return err
		}
		classID++
	}
	return nil
}

func (s *trafficShaper) addIPFilterLocked(dev, handle, ip, flow string, ingress bool) error {
	direction := "dst"
	if ingress {
		direction = "src"
	}
	if net.ParseIP(ip) != nil && strings.Contains(ip, ":") {
		return s.runErr("tc", "filter", "add", "dev", dev, "protocol", "ipv6", "parent", handle+"0", "prio", "1", "u32", "match", "ip6", direction, ip+"/128", "flowid", flow)
	}
	return s.runErr("tc", "filter", "add", "dev", dev, "protocol", "ip", "parent", handle+"0", "prio", "1", "u32", "match", "ip", direction, ip+"/32", "flowid", flow)
}

// runErr 执行命令，失败时把输出带进错误里。
func (s *trafficShaper) runErr(name string, args ...string) error {
	out, err := s.run(name, args...)
	if err != nil {
		return fmt.Errorf("%s 失败：%s", strings.Join(append([]string{name}, args...), " "), strings.TrimSpace(out))
	}
	return nil
}
