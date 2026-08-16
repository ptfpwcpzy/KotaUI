package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const sniTestTimeout = 8 * time.Second

type sniProbeFunc func(context.Context, string, int) (sniTestResult, error)

type sniTestRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type sniTestResult struct {
	Host        string `json:"host"`
	Address     string `json:"address,omitempty"`
	Port        int    `json:"port"`
	OK          bool   `json:"ok"`
	LatencyMS   int64  `json:"latencyMs"`
	TLSVersion  string `json:"tlsVersion,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	Message     string `json:"message,omitempty"`
}

func (a *App) sniTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request sniTestRequest
	if err := decodeJSON(r, &request); err != nil {
		badRequest(w, err)
		return
	}
	host, err := normalizeSNIHost(request.Host)
	if err != nil {
		badRequest(w, err)
		return
	}
	port := request.Port
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		badRequest(w, errors.New("端口必须在 1–65535 之间"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sniTestTimeout)
	defer cancel()
	result, err := a.sniProbe(ctx, host, port)
	result.Host, result.Port = host, port
	if err != nil {
		result.OK = false
		result.Message = "不建议使用：" + err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result.OK = true
	if result.LatencyMS <= 250 {
		result.Message = "推荐使用：TLS 握手与证书校验通过"
	} else {
		result.Message = "可用：TLS 握手与证书校验通过，延迟较高"
	}
	writeJSON(w, http.StatusOK, result)
}

func probeSNI(ctx context.Context, host string, port int) (sniTestResult, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return sniTestResult{}, errors.New("域名解析失败")
	}
	var lastErr error
	for _, ip := range ips {
		ip = ip.Unmap()
		if !isPublicSNIAddress(ip) {
			lastErr = errors.New("域名解析到了非公网地址")
			continue
		}
		started := time.Now()
		dialer := net.Dialer{Timeout: sniTestTimeout}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		if err != nil {
			lastErr = err
			continue
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			lastErr = err
			continue
		}
		state := tlsConn.ConnectionState()
		_ = tlsConn.Close()
		result := sniTestResult{Address: ip.String(), LatencyMS: time.Since(started).Milliseconds(), TLSVersion: tlsVersionName(state.Version)}
		if len(state.PeerCertificates) > 0 {
			result.Certificate = state.PeerCertificates[0].Subject.CommonName
		}
		return result, nil
	}
	if lastErr == nil {
		lastErr = errors.New("未找到可用公网地址")
	}
	return sniTestResult{}, lastErr
}

func normalizeSNIHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if host == "" || len(host) > 253 || net.ParseIP(host) != nil {
		return "", errors.New("请输入有效域名，伪装 SNI 不支持 IP 地址")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", errors.New("请输入完整域名，例如 www.cloudflare.com")
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("域名格式无效")
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", errors.New("域名仅允许字母、数字和连字符")
			}
		}
	}
	return host, nil
}

func isPublicSNIAddress(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip.Is4() {
		v4 := ip.As4()
		if v4[0] == 0 || v4[0] >= 224 || (v4[0] == 100 && v4[1]&0xc0 == 0x40) {
			return false
		}
	}
	return true
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return fmt.Sprintf("TLS 0x%x", version)
	}
}
