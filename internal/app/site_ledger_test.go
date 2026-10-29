package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
	"github.com/ptfpwcpzy/KotaUI/internal/proxy"
)

func TestMapSiteHost(t *testing.T) {
	cases := []struct {
		host    string
		key     string
		service string
	}{
		{"youtube.com", "youtube", "YouTube"},
		{"www.youtube.com", "youtube", "YouTube"},
		{"googlevideo.com", "youtube", "YouTube"},
		{"tv.apple.com", "apple", "Apple"},
		{"apple.com", "apple", "Apple"},
		{"drive.google.com", "google", "Google"},
		{"gmail.com", "google", "Google"},
		{"maps.google.com", "google", "Google"},
		{"scholar.google.com", "google", "Google"},
		{"gemini.google.com", "google", "Google"},
		{"onedrive.live.com", "microsoft", "Microsoft"},
		{"copilot.microsoft.com", "microsoft", "Microsoft"},
		{"music.apple.com", "apple", "Apple"},
		{"icloud.com", "apple", "Apple"},
		{"music.youtube.com", "youtube", "YouTube"},
		{"graph.meta.ai", "meta", "Muse"},
		{"hatch-api.meta.ai", "meta", "Muse"},
		{"hatch.metaaivm.com", "meta", "Muse"},
		{"in.appcenter.ms", "microsoft", "Microsoft"},
		{"sub.unknown.example", "domain:unknown.example", ""},
		{"a.b.5224210.xyz", "domain:5224210.xyz", ""},
		{"1.2.3.4", "domain:1.2.3.4", ""},
		{"YouTube.COM", "youtube", "YouTube"},
		{"unknown.example", "domain:unknown.example", ""},
		{"api16-normal-c-useast1a.tiktokv.com", "tiktok", "TikTok"},
		{"p16-tiktok-sg.ibyteimg.com", "tiktok", "TikTok"},
		{"quic-normal-lb-gcp.byteoversea.net", "tiktok", "TikTok"},
		{"api2.musical.ly", "tiktok", "TikTok"},
		{"telegra.ph", "telegram", "Telegram"},
		{"lh3.googleusercontent.com", "google", "Google"},
		{"lh3.ggpht.com", "google", "Google"},
		{"doubleclick.net", "google", "Google"},
		{"lookaside.fbsbx.com", "facebook", "Facebook"},
		{"wa.me", "whatsapp", "WhatsApp"},
		{"t.co", "x", "X"},
		{"is1-ssl.mzstatic.com", "apple", "Apple"},
		{"login.microsoftonline.com", "microsoft", "Microsoft"},
		{"bing.com", "microsoft", "Microsoft"},
		{"fast.com", "netflix", "Netflix"},
		{"discord.gg", "discord", "Discord"},
		{"cdn.discordapp.com", "discord", "Discord"},
		{"i.redd.it", "reddit", "Reddit"},
		{"user.github.io", "github", "GitHub"},
		{"raw.githubusercontent.com", "github", "GitHub"},
		{"cdn.steamstatic.com", "steam", "Steam"},
		{"zoom.com", "zoom", "Zoom"},
		{"x.notion.site", "notion", "Notion"},
		{"dl.dropboxusercontent.com", "dropbox", "Dropbox"},
		{"oaistatic.com", "openai", "OpenAI"},
		{"static-cdn.jtvnw.net", "twitch", "Twitch"},
		{"qph.cf2.quoracdn.net", "quora", "Quora"},
		{"user.bsky.social", "bluesky", "Bluesky"},
		{"profile.line-scdn.net", "line", "LINE"},
		{"i.pinimg.com", "pinterest", "Pinterest"},
		{"hbo.com", "hbomax", "HBO Max"},
		{"spoti.fi", "spotify", "Spotify"},
		{"ei.phncdn.com", "pornhub", "Pornhub"},
		{"cdn1a.image.pornhub.phncdn.com", "pornhub", "Pornhub"},
		{"phprcdn.com", "pornhub", "Pornhub"},
		{"pornhubpremium.com", "pornhub", "Pornhub"},
		{"chromium.googlesource.com", "google", "Google"},
		{"connect.facebook.net", "facebook", "Facebook"},
		{"fb.watch", "facebook", "Facebook"},
		{"fb.me", "facebook", "Facebook"},
		{"fb.gg", "facebook", "Facebook"},
		{"instagr.am", "instagram", "Instagram"},
		{"telegram.me", "telegram", "Telegram"},
		{"cdn5.telesco.pe", "telegram", "Telegram"},
		{"tiktokshop.com", "tiktok", "TikTok"},
		{"windows.com", "microsoft", "Microsoft"},
		{"microsoft365.com", "microsoft", "Microsoft"},
		{"hotmail.com", "microsoft", "Microsoft"},
		{"onedrive.com", "microsoft", "Microsoft"},
		{"azure.com", "microsoft", "Microsoft"},
		{"visualstudio.com", "microsoft", "Microsoft"},
		{"gallerycdn.vsassets.io", "microsoft", "Microsoft"},
		{"m.media-amazon.com", "amazon", "Amazon"},
		{"aiv-cdn.net", "prime", "Prime Video"},
		{"githubassets.com", "github", "GitHub"},
		{"oaiusercontent.com", "openai", "OpenAI"},
		{"twitch.com", "twitch", "Twitch"},
		{"snap.com", "snapchat", "Snapchat"},
		{"huluim.com", "hulu", "Hulu"},
		{"cdn.sstatic.net", "stackoverflow", "Stack Overflow"},
		{"vimeocdn.com", "vimeo", "Vimeo"},
		{"dmcdn.net", "dailymotion", "Dailymotion"},
		{"cf-media.sndcdn.com", "soundcloud", "SoundCloud"},
		{"rbxcdn.com", "roblox", "Roblox"},
		{"nintendo.net", "nintendo", "Nintendo"},
		{"media.licdn.com", "linkedin", "LinkedIn"},
		{"xboxlive.com", "xbox", "Xbox"},
		{"playstation.net", "playstation", "PlayStation"},
		{"tiktokcdn-us.com", "tiktok", "TikTok"},
		{"tiktokcdn-eu.com", "tiktok", "TikTok"},
		{"ttlivecdn.com", "tiktok", "TikTok"},
		{"spotifycdn.com", "spotify", "Spotify"},
		{"spotify.link", "spotify", "Spotify"},
		{"ai.studio", "google", "Google"},
		{"deepmind.com", "google", "Google"},
		{"gemini.google", "google", "Google"},
		{"notebooklm.google", "google", "Google"},
		{"nflximg.com", "netflix", "Netflix"},
		{"fragment.com", "telegram", "Telegram"},
		{"ton.org", "telegram", "Telegram"},
		{"cdn-telegram.org", "telegram", "Telegram"},
		{"ggpht.cn", "youtube", "YouTube"},
		{"yt.be", "youtube", "YouTube"},
		{"youtubekids.com", "youtube", "YouTube"},
		{"pornhub-deutsch.net", "pornhub", "Pornhub"},
		{"pornhub.org", "pornhub", "Pornhub"},
		{"amazonaws.cn", "aws", "AWS"},
		{"amazoncognito.com", "aws", "AWS"},
		{"pv-cdn.net", "prime", "Prime Video"},
		{"primevideo.cc", "prime", "Prime Video"},
		{"boxofficemojo.com", "imdb", "IMDb"},
		{"huluplus.com", "hulu", "Hulu"},
		{"doom.com", "bethesda", "Bethesda"},
		{"origin.tv", "origin", "Origin"},
		{"eastore.com", "ea", "EA"},
		{"metabora.io", "kakao", "KakaoTalk"},
		{"xhcdn.com", "xhamster", "xHamster"},
		{"static-ec.xhcdn.com", "xhamster", "xHamster"},
		{"xhamster.com", "xhamster", "xHamster"},
		{"cdn13.com", "xhamster", "xHamster"},
		{"trafficjunky.net", "pornhub", "Pornhub"},
		{"trafficjunky.com", "pornhub", "Pornhub"},
		{"nodeseek.com", "nodeseek", "NodeSeek"},
		{"www.nodeseek.com", "nodeseek", "NodeSeek"},
		{"", "unknown", ""},
	}
	for _, tc := range cases {
		key, service := mapSiteHost(tc.host)
		if key != tc.key {
			t.Errorf("mapSiteHost(%q) key = %q, want %q", tc.host, key, tc.key)
		}
		name := ""
		if service != nil {
			name = service.name
		}
		if name != tc.service {
			t.Errorf("mapSiteHost(%q) service = %q, want %q", tc.host, name, tc.service)
		}
	}
}

func TestMapSiteIP(t *testing.T) {
	cases := []struct {
		dst     string
		service string
	}{
		{"149.154.167.51", "telegram"},
		{"149.154.175.53", "telegram"},
		{"149.154.175.100", "telegram"},
		{"149.154.167.91", "telegram"},
		{"149.154.164.250", "telegram"},
		{"149.154.171.5", "telegram"},
		{"91.108.56.130", "telegram"},
		{"91.105.192.100", "telegram"},
		{"2001:b28:f23d:f002::a", "telegram"},
		{"2001:67c:4e8:f004::a", "telegram"},
		{"::ffff:149.154.167.51", "telegram"},
		{"17.1.2.3", "apple"},
		{"17.254.0.1", "apple"},
		{"8.8.8.8", ""},
		{"1.1.1.1", ""},
		{"103.97.200.102", ""},
		{"", ""},
		{"not-an-ip", ""},
	}
	for _, tc := range cases {
		service := mapSiteIP(tc.dst)
		name := ""
		if service != nil {
			name = service.id
		}
		if name != tc.service {
			t.Errorf("mapSiteIP(%q) = %q, want %q", tc.dst, name, tc.service)
		}
	}
}

func TestServiceIPRangesValid(t *testing.T) {
	for id, ranges := range serviceIPRanges {
		if siteServiceByID(id) == nil {
			t.Errorf("IP range service %q not in siteServices", id)
		}
		for _, r := range ranges {
			if _, err := netip.ParsePrefix(r); err != nil {
				t.Errorf("service %q range %q does not parse: %v", id, r, err)
			}
		}
		if len(serviceIPNets[id]) != len(ranges) {
			t.Errorf("service %q parsed %d of %d ranges", id, len(serviceIPNets[id]), len(ranges))
		}
	}
}

func TestBaseDomain(t *testing.T) {
	cases := []struct{ host, want string }{
		{"graph.meta.ai", "meta.ai"},
		{"a.b.example.co.uk", "example.co.uk"},
		{"foo.github.io", "foo.github.io"},
		{"meta.ai", "meta.ai"},
		{"WWW.Example.COM", "example.com"},
		{"1.2.3.4", "1.2.3.4"},
		{"localhost", "localhost"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := baseDomain(tc.host); got != tc.want {
			t.Errorf("baseDomain(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestBuildSiteRankingMerge(t *testing.T) {
	// 旧账本里按完整域名存的行 + 新表下已归并的行，展示时要合成一行。
	ledger := newDaySiteLedger("2026-10-28")
	ledger.Services["usa"] = map[string]*siteTotals{
		"meta":                      {Upload: 100, Download: 100, Duration: 10},
		"domain:graph.meta.ai":      {Upload: 50, Download: 50, Duration: 20},
		"domain:hatch-api.meta.ai":  {Upload: 30, Download: 30, Duration: 30},
		"domain:hatch.metaaivm.com": {Upload: 40, Download: 40, Duration: 40},
		"domain:zouter.5224210.xyz": {Upload: 40, Download: 40, Duration: 5},
		"domain:vmiss3.5224210.xyz": {Upload: 60, Download: 60, Duration: 7},
	}
	ranking := buildSiteRanking(ledger, "usa", "")
	if len(ranking.Sites) != 2 {
		t.Fatalf("sites = %d, want 2: %+v", len(ranking.Sites), ranking.Sites)
	}
	meta := ranking.Sites[0]
	if meta.Name != "Muse" || meta.Domain != "meta.ai" || meta.Icon != "meta" || meta.Bytes != 440 || meta.Duration != 100 {
		t.Errorf("meta row = %+v, want Muse/meta.ai/meta/440/100", meta)
	}
	priv := ranking.Sites[1]
	if priv.Name != "5224210.xyz" || priv.Domain != "5224210.xyz" || priv.Icon != "un" || priv.Bytes != 200 || priv.Duration != 12 {
		t.Errorf("private row = %+v, want 5224210.xyz/5224210.xyz/un/200/12", priv)
	}
	if ranking.OtherCount != 0 || ranking.OtherBytes != 0 {
		t.Errorf("other = %d/%d, want 0/0", ranking.OtherCount, ranking.OtherBytes)
	}
}

func TestIsOwnDomain(t *testing.T) {
	cases := []struct {
		host, own string
		want      bool
	}{
		{"vmiss3.5224210.xyz", "vmiss3.5224210.xyz", true},
		{"a.vmiss3.5224210.xyz", "vmiss3.5224210.xyz", true},
		{"zouter.5224210.xyz", "vmiss3.5224210.xyz", true},
		{"5224210.xyz", "vmiss3.5224210.xyz", true},
		{"a.b.5224210.xyz", "vmiss3.5224210.xyz", true},
		{"example.com", "vmiss3.5224210.xyz", false},
		{"VMISS3.5224210.XYZ", "vmiss3.5224210.xyz", true},
		{"103.97.200.102", "103.97.200.102", true},
		{"anything.example", "", false},
		{"", "vmiss3.5224210.xyz", false},
	}
	for _, tc := range cases {
		if got := isOwnDomain(tc.host, tc.own); got != tc.want {
			t.Errorf("isOwnDomain(%q, %q) = %v, want %v", tc.host, tc.own, got, tc.want)
		}
	}
}

func TestOwnDomainExcluded(t *testing.T) {
	siteLastAggregate = time.Time{}
	siteLastCleanup = time.Time{}
	dir := t.TempDir()
	today := time.Now().In(config.PanelLocation).Format("2006-01-02")
	noon := time.Date(time.Now().In(config.PanelLocation).Year(), time.Now().In(config.PanelLocation).Month(), time.Now().In(config.PanelLocation).Day(), 12, 0, 0, 0, config.PanelLocation)
	record := func(user, host string, up, down int64) string {
		body, _ := json.Marshal(siteRecord{User: user, Host: host, Upload: up, Download: down, Start: noon.Unix(), End: noon.Add(time.Minute).Unix()})
		return string(body) + "\n"
	}
	// 本机域名家族都不入账：绑定域名本身、它的子域名、同主域名下的其他服务器。
	content := record("alice", "vmiss3.5224210.xyz", 100, 100)
	content += record("alice", "a.vmiss3.5224210.xyz", 100, 100)
	content += record("alice", "zouter.5224210.xyz", 10, 20)
	content += record("alice", "youtube.com", 50, 50)
	writeSiteRecords(t, dir, content)
	aggregateSiteRecords(dir, "", "vmiss3.5224210.xyz", true)
	ledger := readDayLedger(t, dir, today)
	alice := ledger.Services["alice"]
	if len(alice) != 1 || alice["youtube"] == nil || alice["youtube"].bytes() != 100 {
		t.Fatalf("alice keys = %v, want only youtube", alice)
	}
	// 已入账的旧账本里，本机域名行在展示时也要排除，且不计入其余合计。
	ledger.Services["alice"]["domain:zouter.5224210.xyz"] = &siteTotals{Upload: 500, Download: 500}
	ranking := buildSiteRanking(ledger, "alice", "vmiss3.5224210.xyz")
	if len(ranking.Sites) != 1 || ranking.Sites[0].Name != "YouTube" {
		t.Fatalf("sites = %+v, want only YouTube", ranking.Sites)
	}
	if ranking.OtherBytes != 0 || ranking.OtherCount != 0 {
		t.Fatalf("other = %d/%d, want 0/0", ranking.OtherCount, ranking.OtherBytes)
	}
}

func TestUnknownFoldedIntoOther(t *testing.T) {
	ledger := newDaySiteLedger("2026-10-28")
	ledger.Services["usa"] = map[string]*siteTotals{
		"unknown":            {Upload: 500, Download: 500},
		"youtube":            {Upload: 50, Download: 50},
		"domain:example.com": {Upload: 25, Download: 25},
	}
	ranking := buildSiteRanking(ledger, "usa", "")
	if len(ranking.Sites) != 2 {
		t.Fatalf("sites = %+v, want youtube + example.com", ranking.Sites)
	}
	for _, row := range ranking.Sites {
		if row.Name == "未知域名" {
			t.Fatalf("unknown must not be a row, got %+v", row)
		}
	}
	if ranking.OtherCount != 1 || ranking.OtherBytes != 1000 {
		t.Fatalf("other = %d/%d, want 1/1000", ranking.OtherCount, ranking.OtherBytes)
	}
}

func writeSiteRecords(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(proxy.SiteRecordsPath(dir), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func readDayLedger(t *testing.T, dir, date string) *daySiteLedger {
	t.Helper()
	ledger, err := loadDaySiteLedger(dir, date)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func TestAggregateSiteRecords(t *testing.T) {
	siteLastAggregate = time.Time{}
	siteLastCleanup = time.Time{}
	dir := t.TempDir()
	today := time.Now().In(config.PanelLocation).Format("2006-01-02")
	yesterday := time.Now().In(config.PanelLocation).AddDate(0, 0, -1).Format("2006-01-02")
	todayNoon := time.Date(time.Now().In(config.PanelLocation).Year(), time.Now().In(config.PanelLocation).Month(), time.Now().In(config.PanelLocation).Day(), 12, 0, 0, 0, config.PanelLocation)
	yesterdayNoon := todayNoon.AddDate(0, 0, -1)
	record := func(user, host string, up, down, start, end int64) string {
		body, _ := json.Marshal(siteRecord{User: user, Host: host, Upload: up, Download: down, Start: start, End: end})
		return string(body) + "\n"
	}
	// 正常记录 + 跨天记录 + 应跳过的记录 + 最后半截行（下次补全再算）。
	content := record("alice", "youtube.com", 100, 900, todayNoon.Unix(), todayNoon.Add(30*time.Minute).Unix())
	content += record("alice", "googlevideo.com", 50, 50, todayNoon.Unix(), todayNoon.Add(10*time.Minute).Unix())
	content += record("bob", "unknown.example", 10, 20, todayNoon.Unix(), todayNoon.Add(time.Minute).Unix())
	content += record("alice", "netflix.com", 500, 500, yesterdayNoon.Unix(), yesterdayNoon.Add(time.Hour).Unix())
	content += record("", "youtube.com", 10, 10, todayNoon.Unix(), todayNoon.Add(time.Minute).Unix())
	content += record("alice", "youtube.com", 0, 0, todayNoon.Unix(), todayNoon.Add(time.Hour).Unix())
	torn := record("alice", "spotify.com", 70, 30, todayNoon.Unix(), todayNoon.Add(5*time.Minute).Unix())
	content += torn[:len(torn)-5] // 半截行：不结尾换行，本次不算
	writeSiteRecords(t, dir, content)
	aggregateSiteRecords(dir, "relay-abc", "", true)

	ledger := readDayLedger(t, dir, today)
	alice := ledger.Services["alice"]
	if alice == nil || alice["youtube"] == nil || alice["youtube"].bytes() != 1100 {
		t.Fatalf("alice youtube = %+v, want 1100 bytes", alice["youtube"])
	}
	if alice["youtube"].Duration != 40*60 {
		t.Fatalf("alice youtube duration = %d, want %d", alice["youtube"].Duration, 40*60)
	}
	if alice["spotify"] != nil {
		t.Fatalf("torn spotify record must not be aggregated yet, got %+v", alice["spotify"])
	}
	if ledger.Services["bob"]["domain:unknown.example"] == nil || ledger.Services["bob"]["domain:unknown.example"].bytes() != 30 {
		t.Fatalf("bob unknown = %+v, want 30 bytes", ledger.Services["bob"])
	}
	if _, ok := ledger.Services[""]; ok {
		t.Fatal("empty user must be skipped")
	}
	yesterdayLedger := readDayLedger(t, dir, yesterday)
	if yesterdayLedger.Services["alice"]["netflix"].bytes() != 1000 {
		t.Fatalf("yesterday alice netflix = %+v, want 1000", yesterdayLedger.Services["alice"]["netflix"])
	}
	// 偏移只推进到最后一个完整换行处（即完整行的总长度）。
	offsetRaw, err := os.ReadFile(siteLedgerOffsetPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var offset int64
	if _, err := fmt.Sscanf(string(offsetRaw), "%d", &offset); err != nil {
		t.Fatal(err)
	}
	wantOffset := int64(len(content) - len(torn) + 5)
	if offset != wantOffset {
		t.Fatalf("offset = %d, want %d", offset, wantOffset)
	}
	// 补全半截行后，下次聚合算进去，不重算旧记录。
	writeSiteRecords(t, dir, content+torn[len(torn)-5:]+"\n")
	aggregateSiteRecords(dir, "relay-abc", "", true)
	ledger = readDayLedger(t, dir, today)
	if ledger.Services["alice"]["spotify"] == nil || ledger.Services["alice"]["spotify"].bytes() != 100 {
		t.Fatalf("spotify after completion = %+v, want 100", ledger.Services["alice"]["spotify"])
	}
	if ledger.Services["alice"]["youtube"].bytes() != 1100 {
		t.Fatalf("youtube must not double count, got %d", ledger.Services["alice"]["youtube"].bytes())
	}
}

func TestAggregateSiteRecordsIP(t *testing.T) {
	siteLastAggregate = time.Time{}
	siteLastCleanup = time.Time{}
	dir := t.TempDir()
	today := time.Now().In(config.PanelLocation).Format("2006-01-02")
	noon := time.Date(time.Now().In(config.PanelLocation).Year(), time.Now().In(config.PanelLocation).Month(), time.Now().In(config.PanelLocation).Day(), 12, 0, 0, 0, config.PanelLocation)
	record := func(user, host, dst string, up, down int64) string {
		body, _ := json.Marshal(siteRecord{User: user, Host: host, Dst: dst, Upload: up, Download: down, Start: noon.Unix(), End: noon.Add(10 * time.Minute).Unix()})
		return string(body) + "\n"
	}
	content := record("alice", "", "149.154.167.51", 100, 900)
	content += record("alice", "", "17.1.2.3", 50, 50)
	content += record("bob", "", "8.8.8.8", 10, 20)
	content += record("carol", "youtube.com", "149.154.167.51", 70, 30)
	writeSiteRecords(t, dir, content)
	aggregateSiteRecords(dir, "relay-abc", "", true)
	ledger := readDayLedger(t, dir, today)
	if ledger.Services["alice"]["telegram"] == nil || ledger.Services["alice"]["telegram"].bytes() != 1000 {
		t.Fatalf("alice telegram = %+v, want 1000 bytes", ledger.Services["alice"]["telegram"])
	}
	if ledger.Services["alice"]["apple"] == nil || ledger.Services["alice"]["apple"].bytes() != 100 {
		t.Fatalf("alice apple = %+v, want 100 bytes", ledger.Services["alice"]["apple"])
	}
	if ledger.Services["bob"]["unknown"] == nil || ledger.Services["bob"]["unknown"].bytes() != 30 {
		t.Fatalf("bob unknown = %+v, want 30 bytes", ledger.Services["bob"]["unknown"])
	}
	// 域名在时域名优先，目标 IP 不改道。
	if ledger.Services["carol"]["youtube"] == nil || ledger.Services["carol"]["youtube"].bytes() != 100 {
		t.Fatalf("carol youtube = %+v, want 100 bytes", ledger.Services["carol"]["youtube"])
	}
	if ledger.Services["carol"]["telegram"] != nil {
		t.Fatalf("carol telegram must be empty when host is present, got %+v", ledger.Services["carol"]["telegram"])
	}
}

func TestBuildSiteRanking(t *testing.T) {
	ledger := newDaySiteLedger("2026-10-28")
	ledger.Services["alice"] = map[string]*siteTotals{}
	for i := 0; i < 12; i++ {
		ledger.Services["alice"][fmt.Sprintf("domain:site%d.example", i)] = &siteTotals{Upload: 10, Download: int64(100 - i), Duration: int64(i)}
	}
	ranking := buildSiteRanking(ledger, "alice", "")
	if len(ranking.Sites) != 10 {
		t.Fatalf("sites = %d, want 10", len(ranking.Sites))
	}
	if ranking.OtherCount != 2 {
		t.Fatalf("otherCount = %d, want 2", ranking.OtherCount)
	}
	// 其余两项：site10 (100-10+10=100? 按 bytes=110-i) 与 site11。
	wantOther := int64(0)
	for i := 10; i < 12; i++ {
		wantOther += 10 + int64(100-i)
	}
	if ranking.OtherBytes != wantOther {
		t.Fatalf("otherBytes = %d, want %d", ranking.OtherBytes, wantOther)
	}
	if ranking.Sites[0].Bytes <= ranking.Sites[9].Bytes {
		t.Fatal("sites must be sorted by bytes desc")
	}
	if empty := buildSiteRanking(ledger, "nobody", ""); len(empty.Sites) != 0 || empty.OtherCount != 0 {
		t.Fatalf("unknown user must be empty, got %+v", empty)
	}
}

func TestSiteLedgerRetention(t *testing.T) {
	siteLastAggregate = time.Time{}
	siteLastCleanup = time.Time{}
	dir := t.TempDir()
	if err := os.MkdirAll(siteLedgerDir(dir), 0700); err != nil {
		t.Fatal(err)
	}
	writeSiteRecords(t, dir, "")
	keep := time.Now().AddDate(0, 0, -(siteLedgerRetentionDays - 1)).Format("2006-01-02")
	drop := time.Now().AddDate(0, 0, -siteLedgerRetentionDays).Format("2006-01-02")
	for _, date := range []string{keep, drop, time.Now().Format("2006-01-02")} {
		if err := os.WriteFile(siteLedgerDayPath(dir, date), []byte(`{"date":"`+date+`","services":{}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	aggregateSiteRecords(dir, "", "", true)
	if _, err := os.Stat(siteLedgerDayPath(dir, drop)); !os.IsNotExist(err) {
		t.Fatalf("day file %s must be deleted", drop)
	}
	if _, err := os.Stat(siteLedgerDayPath(dir, keep)); err != nil {
		t.Fatalf("day file %s must be kept: %v", keep, err)
	}
}

func TestLiveSiteIcons(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(proxy.ClashSecretFile(dir), []byte("testsecret"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testsecret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"connections":[
			{"metadata":{"host":"youtube.com"}},
			{"metadata":{"host":"googlevideo.com"}},
			{"metadata":{"host":"unknown.example"}},
			{"metadata":{"host":""}},
			{"metadata":{"host":"","destinationIP":"149.154.167.51"}}
		]}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	var port int
	if _, err := fmt.Sscanf(addr[strings.LastIndex(addr, ":")+1:], "%d", &port); err != nil {
		t.Fatal(err)
	}
	icons := liveSiteIcons(dir, port-1)
	if len(icons) != 3 || icons[0] != "youtube" || icons[1] != "un" || icons[2] != "telegram" {
		t.Fatalf("icons = %v, want [youtube un telegram]", icons)
	}
	// 核心没起时返回空，不报错。
	if icons := liveSiteIcons(dir, 59998); len(icons) != 0 {
		t.Fatalf("down core must give empty icons, got %v", icons)
	}
}

func TestSiteRecordsTruncation(t *testing.T) {
	siteLastAggregate = time.Time{}
	siteLastCleanup = time.Time{}
	dir := t.TempDir()
	var builder strings.Builder
	line, _ := json.Marshal(siteRecord{User: "alice", Host: "youtube.com", Upload: 100, Download: 100, Start: 1000, End: 1060})
	for builder.Len() <= siteRecordsMaxBytes {
		builder.Write(line)
		builder.WriteByte('\n')
	}
	writeSiteRecords(t, dir, builder.String())
	aggregateSiteRecords(dir, "", "", true)
	info, err := os.Stat(proxy.SiteRecordsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("records file must be truncated after full consume, size = %d", info.Size())
	}
	raw, err := os.ReadFile(siteLedgerOffsetPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "0" {
		t.Fatalf("offset after truncation = %q, want 0", raw)
	}
}

func TestSiteSuffixesWellFormed(t *testing.T) {
	for _, svc := range siteServices {
		for _, suffix := range svc.suffixes {
			if suffix == "" || suffix != strings.ToLower(suffix) || strings.ContainsAny(suffix, " @") || !strings.Contains(suffix, ".") || strings.HasPrefix(suffix, ".") || strings.HasSuffix(suffix, ".") {
				t.Errorf("service %s has malformed suffix %q", svc.id, suffix)
			}
			for _, label := range strings.Split(suffix, ".") {
				if label == "" {
					t.Errorf("service %s has empty label in suffix %q", svc.id, suffix)
				}
			}
		}
	}
}

// 账本必须按面板时区（上海）切天，不能按服务器本地时间。
// 2026-10-28 20:00 UTC 是上海 10-29 04:00：服务器本地还是 10-28，
// 但记录必须进 10-29 的账本；上海午夜前后两分钟也必须分属两天。
func TestAggregateSiteRecordsPanelDay(t *testing.T) {
	siteLastAggregate = time.Time{}
	siteLastCleanup = time.Time{}
	dir := t.TempDir()
	record := func(user, host string, start, end int64) string {
		body, _ := json.Marshal(siteRecord{User: user, Host: host, Upload: 10, Download: 90, Start: start, End: end})
		return string(body) + "\n"
	}
	crossEnd := time.Date(2026, 10, 29, 4, 0, 0, 0, config.PanelLocation)
	midnight := time.Date(2026, 10, 29, 0, 0, 0, 0, config.PanelLocation)
	beforeMidnight := time.Date(2026, 10, 28, 23, 59, 0, 0, config.PanelLocation)
	content := record("alice", "youtube.com", crossEnd.Add(-time.Hour).Unix(), crossEnd.Unix())
	content += record("alice", "youtube.com", midnight.Add(-time.Hour).Unix(), midnight.Unix())
	content += record("alice", "youtube.com", beforeMidnight.Add(-time.Hour).Unix(), beforeMidnight.Unix())
	writeSiteRecords(t, dir, content)
	aggregateSiteRecords(dir, "relay-abc", "", true)
	day29 := readDayLedger(t, dir, "2026-10-29")
	if day29.Services["alice"]["youtube"] == nil || day29.Services["alice"]["youtube"].bytes() != 200 {
		t.Fatalf("2026-10-29 alice youtube = %+v, want 200 bytes", day29.Services["alice"]["youtube"])
	}
	day28 := readDayLedger(t, dir, "2026-10-28")
	if day28.Services["alice"]["youtube"] == nil || day28.Services["alice"]["youtube"].bytes() != 100 {
		t.Fatalf("2026-10-28 alice youtube = %+v, want 100 bytes", day28.Services["alice"]["youtube"])
	}
}
