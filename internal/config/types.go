package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const Version = "1.0.0"

type Runtime struct {
	DataDir          string
	Listen           string
	PanelPath        string
	SubscriptionPort int
	Domain           string
	CertificateType  string
	TLSCert          string
	TLSKey           string
	AdminUser        string
	AdminPassword    string
	SingBoxBin       string
	SingBoxConfig    string
	ManageSingBox    bool
	StatsPort        int
}

type State struct {
	Settings        Settings                   `json:"settings"`
	Inbounds        []Inbound                  `json:"inbounds"`
	Clients         []Client                   `json:"clients"`
	TrafficCounters map[string]TrafficCounters `json:"trafficCounters,omitempty"`
	Created         time.Time                  `json:"created"`
}

type TrafficCounters struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

type Settings struct {
	Domain            string             `json:"domain"`
	SubscriptionPath  string             `json:"subscriptionPath"`
	RealityCandidates []RealityCandidate `json:"realityCandidates"`
	OutboundStrategy  string             `json:"outboundStrategy"`
	BlockedDomains    []string           `json:"blockedDomains"`
	BlockBitTorrent   bool               `json:"blockBitTorrent"`
}

type RealityCandidate struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Inbound struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Listen          string `json:"listen"`
	Port            int    `json:"port"`
	Enabled         bool   `json:"enabled"`
	Network         string `json:"network,omitempty"`
	HandshakeServer string `json:"handshakeServer,omitempty"`
	HandshakePort   int    `json:"handshakePort,omitempty"`
	SNI             string `json:"sni,omitempty"`
	PublicKey       string `json:"publicKey,omitempty"`
	PrivateKey      string `json:"privateKey,omitempty"`
	ShortID         string `json:"shortId,omitempty"`
	UpMbps          int    `json:"upMbps,omitempty"`
	DownMbps        int    `json:"downMbps,omitempty"`
	ObfsPassword    string `json:"obfsPassword,omitempty"`
	ServerPassword  string `json:"serverPassword,omitempty"`
	UseIPv6         bool   `json:"useIPv6,omitempty"`
}

type Client struct {
	ID                string            `json:"id"`
	Username          string            `json:"username"`
	Note              string            `json:"note,omitempty"`
	InboundIDs        []string          `json:"inboundIds"`
	Credentials       map[string]string `json:"credentials"`
	TotalLimitBytes   int64             `json:"totalLimitBytes"`
	MonthlyLimitBytes int64             `json:"monthlyLimitBytes"`
	UsedBytes         int64             `json:"usedBytes"`
	UploadBytes       int64             `json:"uploadBytes"`
	DownloadBytes     int64             `json:"downloadBytes"`
	MonthlyUsedBytes  int64             `json:"monthlyUsedBytes"`
	Month             string            `json:"month"`
	ExpiresAt         string            `json:"expiresAt,omitempty"`
	MaxOnlineIPs      int               `json:"maxOnlineIps"`
	Paused            bool              `json:"paused"`
	LastActiveAt      time.Time         `json:"lastActiveAt,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
}

func DefaultState(domain string) State {
	candidates := []RealityCandidate{
		{Host: "www.cloudflare.com", Port: 443}, {Host: "www.microsoft.com", Port: 443},
		{Host: "www.apple.com", Port: 443}, {Host: "www.amazon.com", Port: 443},
		{Host: "www.bing.com", Port: 443}, {Host: "www.nvidia.com", Port: 443},
		{Host: "www.adobe.com", Port: 443}, {Host: "www.ibm.com", Port: 443},
		{Host: "www.oracle.com", Port: 443}, {Host: "www.mozilla.org", Port: 443},
	}
	return State{Settings: Settings{Domain: domain, SubscriptionPath: "/kota-sub", RealityCandidates: candidates, OutboundStrategy: "auto"}, Inbounds: []Inbound{}, Clients: []Client{}, TrafficCounters: map[string]TrafficCounters{}, Created: time.Now().UTC()}
}

func NewID() string            { return randomHex(16) }
func RandomHex(n int) string   { return randomHex(n) }
func RandomToken(n int) string { return randomHex(n) }
func RandomBase64(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("random source: %v", err))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("random source: %v", err))
	}
	return hex.EncodeToString(buf)
}

func NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	if path == "" {
		return "/ptf"
	}
	return "/" + path
}

func (c Client) Active(now time.Time) bool {
	if c.Paused {
		return false
	}
	if c.ExpiresAt != "" {
		if expiry, err := time.ParseInLocation("2006-01-02", c.ExpiresAt, time.Local); err != nil || !now.Before(expiry.AddDate(0, 0, 1)) {
			return false
		}
	}
	if c.TotalLimitBytes > 0 && c.UsedBytes >= c.TotalLimitBytes {
		return false
	}
	if c.MonthlyLimitBytes > 0 && c.MonthlyUsedBytes >= c.MonthlyLimitBytes {
		return false
	}
	return true
}
