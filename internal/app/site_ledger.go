package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

// 网站排行账本：核心在连接关闭时写一条 JSON 记录，面板增量聚合到每天一个
// 文件、只留 31 天。总量不单独计数，由明细求和得出；现有 v2ray 总量统计
// 不动，两套账各司其职。

// siteRecord 是核心 site_log 写出的一行。
type siteRecord struct {
	User     string `json:"user"`
	Host     string `json:"host"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
}

// siteTotals 是一天内某用户在某服务上的累计：双向流量 + 连接存活时间之和。
type siteTotals struct {
	Upload   int64 `json:"up"`
	Download int64 `json:"down"`
	Duration int64 `json:"dur"`
}

func (t *siteTotals) bytes() int64 { return t.Upload + t.Download }

// daySiteLedger 是一天的账本：用户 -> 服务 -> 累计。
type daySiteLedger struct {
	Date     string                            `json:"date"`
	Services map[string]map[string]*siteTotals `json:"services"`
}

func newDaySiteLedger(date string) *daySiteLedger {
	return &daySiteLedger{Date: date, Services: map[string]map[string]*siteTotals{}}
}

// siteService 是域名后缀到服务的映射：一个品牌一个服务，域名行显示的域名
// 是该服务的代表域名。
type siteService struct {
	suffixes []string
	id       string
	name     string
	domain   string
	icon     string
}

var siteServices = []siteService{
	{[]string{"youtube.com", "googlevideo.com", "ytimg.com", "youtube-nocookie.com"}, "youtube", "YouTube", "youtube.com", "yt"},
	{[]string{"netflix.com", "nflxext.com", "nflximg.net", "nflxvideo.net"}, "netflix", "Netflix", "netflix.com", "nf"},
	{[]string{"telegram.org", "t.me"}, "telegram", "Telegram", "telegram.org", "tg"},
	{[]string{"tiktok.com", "tiktokcdn.com"}, "tiktok", "TikTok", "tiktok.com", "tt"},
	{[]string{"pornhub.com"}, "pornhub", "Pornhub", "pornhub.com", "ph"},
	{[]string{"instagram.com", "cdninstagram.com"}, "instagram", "Instagram", "instagram.com", "ig"},
	{[]string{"facebook.com", "fbcdn.net"}, "facebook", "Facebook", "facebook.com", "fb"},
	{[]string{"whatsapp.com"}, "whatsapp", "WhatsApp", "whatsapp.com", "wa"},
	{[]string{"discord.com"}, "discord", "Discord", "discord.com", "dc"},
	{[]string{"x.com", "twitter.com", "twimg.com"}, "x", "X", "x.com", "x"},
	{[]string{"spotify.com", "scdn.co"}, "spotify", "Spotify", "spotify.com", "sp"},
	{[]string{"steampowered.com", "steamcommunity.com"}, "steam", "Steam", "steampowered.com", "st"},
	{[]string{"twitch.tv", "twitchcdn.net"}, "twitch", "Twitch", "twitch.tv", "tw"},
	{[]string{"google.com", "gmail.com", "googleapis.com", "gstatic.com"}, "google", "Google", "google.com", "gg"},
	{[]string{"openai.com", "chatgpt.com"}, "openai", "OpenAI", "openai.com", "ai"},
	{[]string{"claude.ai", "anthropic.com"}, "claude", "Claude", "claude.ai", "ai"},
	{[]string{"deepseek.com"}, "deepseek", "DeepSeek", "deepseek.com", "ai"},
	{[]string{"grok.com", "x.ai"}, "grok", "Grok", "grok.com", "ai"},
	{[]string{"disneyplus.com", "disney-plus.net"}, "disney", "Disney+", "disneyplus.com", "un"},
	{[]string{"max.com", "hbomax.com"}, "hbo", "HBO Max", "max.com", "un"},
	{[]string{"primevideo.com", "amazonvideo.com"}, "prime", "Prime Video", "primevideo.com", "un"},
	{[]string{"tv.apple.com"}, "appletv", "Apple TV+", "tv.apple.com", "un"},
	{[]string{"apple.com", "icloud.com"}, "apple", "Apple", "apple.com", "un"},
	{[]string{"microsoft.com", "office.com", "outlook.com", "live.com"}, "microsoft", "Microsoft", "microsoft.com", "un"},
	{[]string{"dropbox.com"}, "dropbox", "Dropbox", "dropbox.com", "un"},
	{[]string{"box.com"}, "box", "Box", "box.com", "un"},
	{[]string{"epicgames.com"}, "epic", "Epic Games", "epicgames.com", "un"},
	{[]string{"playstation.com"}, "playstation", "PlayStation", "playstation.com", "un"},
	{[]string{"xbox.com"}, "xbox", "Xbox", "xbox.com", "un"},
}

// siteSuffixIndex 是按后缀长度从长到短排好的匹配表：最长后缀优先，
// tv.apple.com 归 Apple TV+ 而不是 Apple，drive.google.com 归 Google。
type siteSuffixEntry struct {
	suffix  string
	service *siteService
}

var siteSuffixIndex []siteSuffixEntry

func init() {
	for i := range siteServices {
		for _, suffix := range siteServices[i].suffixes {
			siteSuffixIndex = append(siteSuffixIndex, siteSuffixEntry{suffix: suffix, service: &siteServices[i]})
		}
	}
	sort.Slice(siteSuffixIndex, func(a, b int) bool {
		return len(siteSuffixIndex[a].suffix) > len(siteSuffixIndex[b].suffix)
	})
}

// mapSiteHost 把连接域名归到服务：返回服务 id；未映射的域名返回以域名为
// 键的键，空域名返回未知服务键。
func mapSiteHost(host string) (key string, service *siteService) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "unknown", nil
	}
	for _, entry := range siteSuffixIndex {
		if host == entry.suffix || strings.HasSuffix(host, "."+entry.suffix) {
			return entry.service.id, entry.service
		}
	}
	return "domain:" + host, nil
}

func siteServiceByID(id string) *siteService {
	for i := range siteServices {
		if siteServices[i].id == id {
			return &siteServices[i]
		}
	}
	return nil
}

const (
	siteLedgerRetentionDays = 31
	siteRecordsMaxBytes     = 16 << 20
	siteAggregateMinGap     = 5 * time.Second
	siteCleanupMinGap       = time.Hour
)

var (
	siteAggregateMu   sync.Mutex
	siteLastAggregate time.Time
	siteLastCleanup   time.Time
)

func siteLedgerDir(dataDir string) string {
	return filepath.Join(dataDir, "site-ledger")
}

func siteLedgerOffsetPath(dataDir string) string {
	return filepath.Join(siteLedgerDir(dataDir), "offset")
}

func siteLedgerDayPath(dataDir, date string) string {
	return filepath.Join(siteLedgerDir(dataDir), date+".json")
}

func writeFileAtomic(path string, body []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readSiteOffset(dataDir string) (int64, error) {
	raw, err := os.ReadFile(siteLedgerOffsetPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || offset < 0 {
		// 偏移文件损坏：跳到文件末尾，只丢聚合间隙的新记录，
		// 绝不从头重算导致整月翻倍。
		return -1, nil
	}
	return offset, nil
}

// aggregateSiteRecords 把核心记录文件里的新记录聚合进日账本。force 为
// false 时 5 秒内只跑一次（仪表盘 2 秒一刷时不重复写盘）；钻取接口用
// force 保证看到最新数据。失败只记日志，不影响仪表盘和钻取返回。
func aggregateSiteRecords(dataDir, relayUsername string, force bool) {
	siteAggregateMu.Lock()
	defer siteAggregateMu.Unlock()
	now := time.Now()
	if !force && now.Sub(siteLastAggregate) < siteAggregateMinGap {
		return
	}
	siteLastAggregate = now
	if err := aggregateSiteRecordsOnce(dataDir, relayUsername, now); err != nil {
		log.Printf("聚合网站排行账本失败：%v", err)
	}
}

func aggregateSiteRecordsOnce(dataDir, relayUsername string, now time.Time) error {
	maybeCleanupSiteLedgers(dataDir, now)
	recordsPath := proxy.SiteRecordsPath(dataDir)
	info, err := os.Stat(recordsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	size := info.Size()
	offset, err := readSiteOffset(dataDir)
	if err != nil {
		return err
	}
	if offset < 0 {
		offset = size
	}
	if offset > size {
		// 记录文件被截断或轮转过，从头开始。
		offset = 0
	}
	if offset == size {
		return nil
	}
	raw, err := readSiteRecordsRange(recordsPath, offset, size)
	if err != nil {
		return err
	}
	// 只处理以换行结尾的完整行：最后一行可能正在被核心写入，
	// 留到下次再读，不丢也不重。
	lastNewline := bytes.LastIndexByte(raw, '\n')
	if lastNewline < 0 {
		return nil
	}
	complete := raw[:lastNewline+1]
	newOffset := offset + int64(len(complete))
	byDay := map[string][]siteRecord{}
	scanner := bufio.NewScanner(bytes.NewReader(complete))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var record siteRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.User == "" || record.Upload+record.Download == 0 {
			continue
		}
		day := time.Unix(record.End, 0).Format("2006-01-02")
		byDay[day] = append(byDay[day], record)
	}
	for day, records := range byDay {
		ledger, err := loadDaySiteLedger(dataDir, day)
		if err != nil {
			return err
		}
		for _, record := range records {
			key, _ := mapSiteHost(record.Host)
			userServices := ledger.Services[record.User]
			if userServices == nil {
				userServices = map[string]*siteTotals{}
				ledger.Services[record.User] = userServices
			}
			totals := userServices[key]
			if totals == nil {
				totals = &siteTotals{}
				userServices[key] = totals
			}
			totals.Upload += record.Upload
			totals.Download += record.Download
			duration := record.End - record.Start
			if duration > 0 {
				totals.Duration += duration
			}
		}
		body, err := json.Marshal(ledger)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(siteLedgerDayPath(dataDir, day), body, 0600); err != nil {
			return err
		}
	}
	if err := writeFileAtomic(siteLedgerOffsetPath(dataDir), []byte(strconv.FormatInt(newOffset, 10)), 0600); err != nil {
		return err
	}
	// 记录文件已全部消费且过大时截断归零：核心以 O_APPEND 打开，
	// 截断后新记录仍从文件头写，不会丢到旧偏移里。
	if size > siteRecordsMaxBytes && newOffset == size {
		if err := os.Truncate(recordsPath, 0); err != nil {
			return err
		}
		if err := writeFileAtomic(siteLedgerOffsetPath(dataDir), []byte("0"), 0600); err != nil {
			return err
		}
	}
	return nil
}

func readSiteRecordsRange(path string, offset, size int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, size-offset))
}

func loadDaySiteLedger(dataDir, date string) (*daySiteLedger, error) {
	ledger := newDaySiteLedger(date)
	raw, err := os.ReadFile(siteLedgerDayPath(dataDir, date))
	if err != nil {
		if os.IsNotExist(err) {
			return ledger, nil
		}
		return nil, err
	}
	var decoded daySiteLedger
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// 账本文件损坏：从空账本重算当天的增量，旧明细丢弃。
		log.Printf("网站排行账本 %s 损坏，从空账本重算", date)
		return ledger, nil
	}
	if decoded.Services == nil {
		decoded.Services = map[string]map[string]*siteTotals{}
	}
	decoded.Date = date
	return &decoded, nil
}

var siteLedgerDayFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.json$`)

func maybeCleanupSiteLedgers(dataDir string, now time.Time) {
	if now.Sub(siteLastCleanup) < siteCleanupMinGap {
		return
	}
	siteLastCleanup = now
	entries, err := os.ReadDir(siteLedgerDir(dataDir))
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -(siteLedgerRetentionDays - 1)).Format("2006-01-02")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parts := siteLedgerDayFilePattern.FindStringSubmatch(entry.Name())
		if parts == nil || parts[1] >= cutoff {
			continue
		}
		_ = os.Remove(filepath.Join(siteLedgerDir(dataDir), entry.Name()))
	}
}

// siteRow 是钻取接口返回的一行：流量为主，时长为辅。
type siteRow struct {
	Name     string `json:"name"`
	Domain   string `json:"domain"`
	Icon     string `json:"icon"`
	Bytes    int64  `json:"bytes"`
	Duration int64  `json:"duration"`
}

type siteRanking struct {
	Sites      []siteRow `json:"sites"`
	OtherCount int       `json:"otherCount"`
	OtherBytes int64     `json:"otherBytes"`
}

// buildSiteRanking 把某用户某天的账本排成前 10 + 其余合计。
func buildSiteRanking(ledger *daySiteLedger, username string) siteRanking {
	ranking := siteRanking{Sites: []siteRow{}}
	if ledger == nil || username == "" {
		return ranking
	}
	userServices := ledger.Services[username]
	if len(userServices) == 0 {
		return ranking
	}
	type scored struct {
		key    string
		totals *siteTotals
	}
	rows := make([]scored, 0, len(userServices))
	for key, totals := range userServices {
		if totals == nil || totals.bytes() == 0 {
			continue
		}
		rows = append(rows, scored{key: key, totals: totals})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].totals.bytes() != rows[b].totals.bytes() {
			return rows[a].totals.bytes() > rows[b].totals.bytes()
		}
		return rows[a].key < rows[b].key
	})
	for i, row := range rows {
		if i >= 10 {
			ranking.OtherCount++
			ranking.OtherBytes += row.totals.bytes()
			continue
		}
		ranking.Sites = append(ranking.Sites, siteRowFor(row.key, row.totals))
	}
	return ranking
}

func siteRowFor(key string, totals *siteTotals) siteRow {
	if key == "unknown" {
		return siteRow{Name: "未知域名", Domain: "", Icon: "un", Bytes: totals.bytes(), Duration: totals.Duration}
	}
	if service := siteServiceByID(key); service != nil {
		return siteRow{Name: service.name, Domain: service.domain, Icon: service.icon, Bytes: totals.bytes(), Duration: totals.Duration}
	}
	if host, ok := strings.CutPrefix(key, "domain:"); ok {
		return siteRow{Name: host, Domain: host, Icon: "un", Bytes: totals.bytes(), Duration: totals.Duration}
	}
	return siteRow{Name: key, Domain: key, Icon: "un", Bytes: totals.bytes(), Duration: totals.Duration}
}

// liveSiteIcons 查询核心 clash_api 的活动连接，去重后返回图标键：
// 只看图标，不分用户，正好绕开 clash_api 没有用户字段的坎。
// 核心没起、没配 clash_api、查询失败都返回空，不影响仪表盘。
func liveSiteIcons(dataDir string, statsPort int) []string {
	icons := []string{}
	secret := proxy.ReadClashSecret(dataDir)
	if secret == "" {
		return icons
	}
	client := &http.Client{Timeout: time.Second}
	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/connections", proxy.ClashPort(statsPort)), nil)
	if err != nil {
		return icons
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := client.Do(request)
	if err != nil {
		return icons
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return icons
	}
	var snapshot struct {
		Connections []struct {
			Metadata struct {
				Host string `json:"host"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		return icons
	}
	seen := map[string]bool{}
	for _, connection := range snapshot.Connections {
		host := strings.TrimSpace(connection.Metadata.Host)
		if host == "" {
			continue
		}
		_, service := mapSiteHost(host)
		icon := "un"
		if service != nil {
			icon = service.icon
		}
		// 预览只看图标：同一图标出现一次就够了。
		if seen[icon] {
			continue
		}
		seen[icon] = true
		icons = append(icons, icon)
		if len(icons) >= 30 {
			break
		}
	}
	return icons
}
