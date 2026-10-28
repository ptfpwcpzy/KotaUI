package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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
		{"zouter.5224210.xyz", "vmiss3.5224210.xyz", false},
		{"5224210.xyz", "vmiss3.5224210.xyz", false},
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
	today := time.Now().Format("2006-01-02")
	noon := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 12, 0, 0, 0, time.Local)
	record := func(user, host string, up, down int64) string {
		body, _ := json.Marshal(siteRecord{User: user, Host: host, Upload: up, Download: down, Start: noon.Unix(), End: noon.Add(time.Minute).Unix()})
		return string(body) + "\n"
	}
	// 本机域名本身、它的子域名都不入账；同主域名的另一台服务器照常入账。
	content := record("alice", "vmiss3.5224210.xyz", 100, 100)
	content += record("alice", "a.vmiss3.5224210.xyz", 100, 100)
	content += record("alice", "zouter.5224210.xyz", 10, 20)
	content += record("alice", "youtube.com", 50, 50)
	writeSiteRecords(t, dir, content)
	aggregateSiteRecords(dir, "", "vmiss3.5224210.xyz", true)
	ledger := readDayLedger(t, dir, today)
	alice := ledger.Services["alice"]
	if len(alice) != 2 {
		t.Fatalf("alice keys = %v, want only zouter + youtube", alice)
	}
	if alice["domain:5224210.xyz"] == nil || alice["domain:5224210.xyz"].bytes() != 30 {
		t.Fatalf("zouter must stay, got %+v", alice["domain:5224210.xyz"])
	}
	// 已入账的旧账本里，本机域名行在展示时也要排除，且不计入其余合计。
	ledger.Services["alice"]["domain:vmiss3.5224210.xyz"] = &siteTotals{Upload: 500, Download: 500}
	ranking := buildSiteRanking(ledger, "alice", "vmiss3.5224210.xyz")
	for _, row := range ranking.Sites {
		if row.Domain == "vmiss3.5224210.xyz" || row.Name == "vmiss3.5224210.xyz" {
			t.Fatalf("own domain must not be displayed, got %+v", row)
		}
	}
	if ranking.OtherBytes != 0 || ranking.OtherCount != 0 {
		t.Fatalf("other = %d/%d, want 0/0 (own domain excluded, rest all in top 10)", ranking.OtherCount, ranking.OtherBytes)
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
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	todayNoon := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 12, 0, 0, 0, time.Local)
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
			{"metadata":{"host":""}}
		]}`))
	}))
	defer server.Close()
	addr := strings.TrimPrefix(server.URL, "http://")
	var port int
	if _, err := fmt.Sscanf(addr[strings.LastIndex(addr, ":")+1:], "%d", &port); err != nil {
		t.Fatal(err)
	}
	icons := liveSiteIcons(dir, port-1)
	if len(icons) != 2 || icons[0] != "youtube" || icons[1] != "un" {
		t.Fatalf("icons = %v, want [youtube un]", icons)
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
