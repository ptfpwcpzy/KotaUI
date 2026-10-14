package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 回程路由：打开主页时从服务器向看面板的人的 IP 做一次路由追踪，
// 画在世界地图上。只测一次，不做后台定时探测；5 分钟内重复打开直接用缓存。
// 保持轻量：最多 20 跳，每跳最多等 1 秒，整体 45 秒封顶；定位只调一次免费接口批量查。

type routeHop struct {
	IP   string  `json:"ip"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Ms   float64 `json:"ms"`
}

type routeCacheEntry struct {
	result map[string]any
	at     time.Time
}

var (
	routeCacheMu sync.Mutex
	routeCache   = map[string]routeCacheEntry{}

	errNoTraceroute = errors.New("服务器缺少 traceroute 工具，请执行 apt install -y traceroute 后重试")
	errTraceFailed  = errors.New("路由探测失败，请稍后重试")
)

func (a *App) routeTrace(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	target := strings.Trim(host, "[]")
	if net.ParseIP(target) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "无法识别你的 IP"})
		return
	}
	routeCacheMu.Lock()
	if e, ok := routeCache[target]; ok && time.Since(e.at) < 5*time.Minute {
		res := e.result
		routeCacheMu.Unlock()
		writeJSON(w, http.StatusOK, res)
		return
	}
	routeCacheMu.Unlock()
	result, err := traceRoute(target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	routeCacheMu.Lock()
	routeCache[target] = routeCacheEntry{result: result, at: time.Now()}
	routeCacheMu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

type tracedHop struct {
	ip string
	ms float64
}

var (
	tracerouteLine = regexp.MustCompile(`^\s*\d+\s+([0-9a-fA-F:.]+)\s+([\d.]+)\s*ms`)
	tracepathLine  = regexp.MustCompile(`^\s*\d+:\s+(\S+)\s+([\d.]+)ms`)
)

func traceRoute(target string) (map[string]any, error) {
	tool := ""
	if _, err := exec.LookPath("traceroute"); err == nil {
		tool = "traceroute"
	} else if _, err := exec.LookPath("tracepath"); err == nil {
		tool = "tracepath"
	} else {
		return nil, errNoTraceroute
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var out []byte
	var err error
	if tool == "traceroute" {
		out, err = exec.CommandContext(ctx, "traceroute", "-n", "-w", "1", "-q", "1", "-m", "20", target).Output()
	} else {
		out, err = exec.CommandContext(ctx, "tracepath", "-n", target).Output()
	}
	if err != nil && len(out) == 0 {
		return nil, errTraceFailed
	}
	lineRe := tracerouteLine
	if tool == "tracepath" {
		lineRe = tracepathLine
	}
	seen := map[string]bool{}
	hops := []tracedHop{}
	for _, line := range strings.Split(string(out), "\n") {
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ip := m[1]
		if net.ParseIP(ip) == nil || seen[ip] {
			continue
		}
		ms, _ := strconv.ParseFloat(m[2], 64)
		seen[ip] = true
		hops = append(hops, tracedHop{ip: ip, ms: ms})
	}
	if len(hops) == 0 {
		return nil, errTraceFailed
	}
	// 定位：服务器自身 + 每一跳（内网 IP 不用查，定位接口也查不到）。
	serverLat, serverLon := geolocateSelf()
	geo := geolocateBatch(hopIPs(hops))
	result := []routeHop{{IP: "server", Name: "服务器", Lat: serverLat, Lon: serverLon, Ms: 0}}
	for i, h := range hops {
		name := h.ip
		lat, lon := 0.0, 0.0
		if g, ok := geo[h.ip]; ok && g.Status == "success" {
			lat, lon = g.Lat, g.Lon
			if n := chineseName(g); n != "" {
				name = n
			}
		}
		if i == len(hops)-1 {
			name = "你的位置"
		}
		result = append(result, routeHop{IP: h.ip, Name: name, Lat: lat, Lon: lon, Ms: h.ms})
	}
	return map[string]any{"viewerIp": target, "hops": result}, nil
}

func hopIPs(hops []tracedHop) []string {
	ips := make([]string, 0, len(hops))
	for _, h := range hops {
		ips = append(ips, h.ip)
	}
	return ips
}

type ipGeo struct {
	Status     string  `json:"status"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	City       string  `json:"city"`
	RegionName string  `json:"regionName"`
	Country    string  `json:"country"`
	Query      string  `json:"query"`
}

// 定位接口不翻城市名（还中英文混着来），用对照表转中文；
// 查不到的退回中文省份，再退回中文国家，最后才用英文原名。
var cityZh = map[string]string{
	"Beijing": "北京", "Shanghai": "上海", "Guangzhou": "广州", "Shenzhen": "深圳",
	"Chengdu": "成都", "Hangzhou": "杭州", "Wuhan": "武汉", "Nanjing": "南京",
	"Tianjin": "天津", "Chongqing": "重庆", "Xi'an": "西安", "Xian": "西安",
	"Suzhou": "苏州", "Changsha": "长沙", "Zhengzhou": "郑州", "Qingdao": "青岛",
	"Dalian": "大连", "Xiamen": "厦门", "Fuzhou": "福州", "Kunming": "昆明",
	"Shenyang": "沈阳", "Harbin": "哈尔滨", "Jinan": "济南", "Hefei": "合肥",
	"Nanchang": "南昌", "Changchun": "长春", "Lanzhou": "兰州", "Guiyang": "贵阳",
	"Nanning": "南宁", "Haikou": "海口", "Urumqi": "乌鲁木齐", "Ürümqi": "乌鲁木齐",
	"Hong Kong": "香港", "Taipei": "台北", "Macau": "澳门", "Shijiazhuang": "石家庄",
	"Taiyuan": "太原", "Hohhot": "呼和浩特", "Yinchuan": "银川", "Xining": "西宁",
	"Lhasa": "拉萨", "Ningbo": "宁波", "Wenzhou": "温州", "Dongguan": "东莞",
	"Foshan": "佛山", "Jinrongjie": "北京", "Chaoyang": "北京", "Haidian": "北京",
	"Los Angeles": "洛杉矶", "San Jose": "圣何塞", "San Francisco": "旧金山",
	"San Diego": "圣迭戈", "Sacramento": "萨克拉门托", "Seattle": "西雅图",
	"Portland": "波特兰", "Las Vegas": "拉斯维加斯", "Phoenix": "菲尼克斯",
	"Denver": "丹佛", "Dallas": "达拉斯", "Houston": "休斯顿", "Austin": "奥斯汀",
	"Chicago": "芝加哥", "New York": "纽约", "Newark": "纽瓦克", "Ashburn": "阿什本",
	"Atlanta": "亚特兰大", "Miami": "迈阿密", "Boston": "波士顿", "Philadelphia": "费城",
	"Detroit": "底特律", "Minneapolis": "明尼阿波利斯", "Tokyo": "东京", "Osaka": "大阪",
	"Nagoya": "名古屋", "Seoul": "首尔", "Busan": "釜山", "Singapore": "新加坡",
	"Kuala Lumpur": "吉隆坡", "Bangkok": "曼谷", "Jakarta": "雅加达", "Manila": "马尼拉",
	"Hanoi": "河内", "Ho Chi Minh City": "胡志明市", "London": "伦敦", "Paris": "巴黎",
	"Frankfurt": "法兰克福", "Amsterdam": "阿姆斯特丹", "Berlin": "柏林",
	"Madrid": "马德里", "Milan": "米兰", "Rome": "罗马", "Sydney": "悉尼",
	"Melbourne": "墨尔本", "Toronto": "多伦多", "Vancouver": "温哥华",
	"Montreal": "蒙特利尔", "Moscow": "莫斯科", "Dubai": "迪拜", "Mumbai": "孟买",
}

func hasChinese(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

func chineseName(g ipGeo) string {
	if zh, ok := cityZh[g.City]; ok {
		return zh
	}
	if hasChinese(g.City) {
		return g.City
	}
	if hasChinese(g.RegionName) {
		return g.RegionName
	}
	if hasChinese(g.Country) {
		return g.Country
	}
	return g.City
}

var geoClient = &http.Client{Timeout: 15 * time.Second}

// 查服务器自己的公网位置：不传 IP 时 ip-api 返回调用方自己的位置。
func geolocateSelf() (float64, float64) {
	resp, err := geoClient.Get("http://ip-api.com/json/?fields=status,lat,lon")
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	var g ipGeo
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil || g.Status != "success" {
		return 0, 0
	}
	return g.Lat, g.Lon
}

// 批量查每一跳的位置（免费接口，一次最多 100 个）。
func geolocateBatch(ips []string) map[string]ipGeo {
	out := map[string]ipGeo{}
	public := make([]string, 0, len(ips))
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil && !parsed.IsPrivate() && !parsed.IsLoopback() {
			public = append(public, ip)
		}
	}
	if len(public) == 0 {
		return out
	}
	queries := make([]map[string]any, 0, len(public))
	for _, ip := range public {
		queries = append(queries, map[string]any{"query": ip, "fields": "status,lat,lon,city,regionName,country", "lang": "zh-CN"})
	}
	body, _ := json.Marshal(queries)
	resp, err := geoClient.Post("http://ip-api.com/batch", "application/json", bytes.NewReader(body))
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	var list []ipGeo
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return out
	}
	for i, g := range list {
		if i < len(public) {
			out[public[i]] = g
		}
	}
	return out
}
