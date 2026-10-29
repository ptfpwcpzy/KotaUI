package app

import (
	"net/http"
	"time"
)

// siteTraffic 返回某天某用户的网站排行：前 10 + 其余合计。
// user 传“中转主机A”时查落地机中转账号的账本；未知用户返回空排行。
func (a *App) siteTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	date := r.URL.Query().Get("date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		badRequest(w, err)
		return
	}
	user := r.URL.Query().Get("user")
	snapshot := a.store.Snapshot()
	relayUsername := snapshot.Settings.Relay.Username
	if !snapshot.Settings.SiteDetailEnabled {
		writeJSON(w, http.StatusOK, map[string]any{
			"date":       date,
			"user":       user,
			"sites":      []siteRow{},
			"otherCount": 0,
			"otherBytes": 0,
		})
		return
	}
	aggregateSiteRecords(a.runtime.DataDir, relayUsername, snapshot.Settings.Domain, true, true)
	lookupUser := user
	if user == "中转主机A" {
		lookupUser = relayUsername
	}
	ledger, err := loadDaySiteLedger(a.runtime.DataDir, date)
	if err != nil {
		serverError(w, err)
		return
	}
	ranking := buildSiteRanking(ledger, lookupUser, snapshot.Settings.Domain)
	writeJSON(w, http.StatusOK, map[string]any{
		"date":       date,
		"user":       user,
		"sites":      ranking.Sites,
		"otherCount": ranking.OtherCount,
		"otherBytes": ranking.OtherBytes,
	})
}
