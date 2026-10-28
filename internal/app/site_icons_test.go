package app

import (
	"os"
	"regexp"
	"testing"
)

// TestSiteIconKeysMatchFrontend 保证后端每个服务的图标键在前端
// siteIcons 里都有对应的真实图标（不再出现后端有键、前端查不到
// 回退通用图标的情况），反过来前端也不得多余的键。
func TestSiteIconKeysMatchFrontend(t *testing.T) {
	raw, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const siteIcons=\{(.*?)\};function siteIcon`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("siteIcons block not found in web/index.html")
	}
	frontend := map[string]bool{}
	for _, km := range regexp.MustCompile(`([A-Za-z0-9_]+):'<svg`).FindAllSubmatch(m[1], -1) {
		frontend[string(km[1])] = true
	}
	backend := map[string]bool{"un": true}
	for i := range siteServices {
		backend[siteServices[i].icon] = true
	}
	for key := range backend {
		if !frontend[key] {
			t.Errorf("backend icon key %q missing in frontend siteIcons", key)
		}
	}
	for key := range frontend {
		if !backend[key] {
			t.Errorf("frontend siteIcons key %q not used by any backend service", key)
		}
	}
}

// TestSiteSuffixesUnique 保证域名后缀在各服务间不重复：最长匹配
// 的排序在并列时是不稳定的，重复后缀会让归属随机。
func TestSiteSuffixesUnique(t *testing.T) {
	seen := map[string]string{}
	for i := range siteServices {
		for _, suffix := range siteServices[i].suffixes {
			if prev, ok := seen[suffix]; ok {
				t.Errorf("suffix %q used by both %q and %q", suffix, prev, siteServices[i].id)
			}
			seen[suffix] = siteServices[i].id
		}
	}
}
