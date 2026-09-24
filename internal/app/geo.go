package app

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

type geoLocation struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	CountryCode string  `json:"countryCode,omitempty"`
}

var logIPPattern = regexp.MustCompile(`(?i)(?:\d{1,3}\.){3}\d{1,3}|[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){2,}`)

func (a *App) geoLocationLoop() {
	a.refreshServerGeo()
	a.scanSingBoxConnections()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.refreshServerGeo()
		a.scanSingBoxConnections()
	}
}

func (a *App) refreshServerGeo() {
	addresses := a.publicNetworkSnapshot()
	if addresses.Latitude != 0 || addresses.Longitude != 0 {
		return
	}
	for _, value := range append(append([]string{}, addresses.IPv4...), addresses.IPv6...) {
		if location, ok := lookupGeoIP(value); ok {
			a.publicNetworkMu.Lock()
			a.publicNetwork.Latitude = location.Latitude
			a.publicNetwork.Longitude = location.Longitude
			a.publicNetwork.CountryCode = location.CountryCode
			a.publicNetworkMu.Unlock()
			return
		}
	}
}

func (a *App) scanSingBoxConnections() {
	path := filepath.Join(a.runtime.DataDir, "singbox-access.log")
	body, err := readGeoLogTail(path)
	if err != nil || len(body) == 0 {
		return
	}
	clients := a.store.Snapshot().Clients
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.Contains(strings.ToLower(line), "inbound") && !strings.Contains(strings.ToLower(line), "connection") {
			continue
		}
		username := ""
		for _, client := range clients {
			if client.Username != "" && strings.Contains(line, client.Username) {
				username = client.Username
				break
			}
		}
		if username == "" {
			continue
		}
		for _, token := range logIPPattern.FindAllString(line, -1) {
			ip := net.ParseIP(token)
			if !isPublicHostIP(ip) {
				continue
			}
			a.clientGeoMu.RLock()
			_, cached := a.clientGeo[username]
			previousIP := a.clientIPs[username]
			a.clientGeoMu.RUnlock()
			if cached || previousIP == ip.String() {
				continue
			}
			a.clientGeoMu.Lock()
			a.clientIPs[username] = ip.String()
			a.clientGeoMu.Unlock()
			if location, ok := lookupGeoIP(ip.String()); ok {
				a.clientGeoMu.Lock()
				a.clientGeo[username] = location
				a.clientGeoMu.Unlock()
			}
			break
		}
	}
}

func readGeoLogTail(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	const maxBytes int64 = 512 * 1024
	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(start, 0); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, maxBytes))
}

func lookupGeoIP(ip string) (geoLocation, bool) {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if !isPublicHostIP(parsed) {
		return geoLocation{}, false
	}
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get("https://ipwho.is/" + parsed.String())
	if err != nil {
		return geoLocation{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return geoLocation{}, false
	}
	var result struct {
		Success     bool    `json:"success"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		CountryCode string  `json:"country_code"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&result); err != nil || !result.Success {
		return geoLocation{}, false
	}
	if result.Latitude == 0 && result.Longitude == 0 {
		return geoLocation{}, false
	}
	return geoLocation{Latitude: result.Latitude, Longitude: result.Longitude, CountryCode: result.CountryCode}, true
}

func (a *App) onlineUsersWithGeo(clients []config.Client, now time.Time) []map[string]any {
	users := recentOnlineUsers(clients, now)
	a.clientGeoMu.RLock()
	defer a.clientGeoMu.RUnlock()
	for _, user := range users {
		name, _ := user["username"].(string)
		if location, ok := a.clientGeo[name]; ok {
			user["latitude"] = location.Latitude
			user["longitude"] = location.Longitude
			user["countryCode"] = location.CountryCode
		}
	}
	return users
}
