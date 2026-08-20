package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	ALPN        string `json:"alpn,omitempty"`
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
	result, err := a.testSNI(r.Context(), request)
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// sniTestAll tests the built-in/configured candidates from the current VPS. The
// list is intentionally server-side only so the endpoint cannot become a general
// purpose internal-network probing endpoint.
func (a *App) sniTestAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	candidates := a.store.Snapshot().Settings.RealityCandidates
	if len(candidates) == 0 {
		writeJSON(w, http.StatusOK, []sniTestResult{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	results := make([]sniTestResult, len(candidates))
	sem := make(chan struct{}, 3)
	var group sync.WaitGroup
	for i, candidate := range candidates {
		group.Add(1)
		go func(index int, host string, port int) {
			defer group.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[index] = sniTestResult{Host: host, Port: port, Message: "不建议使用：批量测试超时"}
				return
			}
			defer func() { <-sem }()
			result, err := a.testSNI(ctx, sniTestRequest{Host: host, Port: port})
			if err != nil {
				result = sniTestResult{Host: host, Port: port, Message: "不建议使用：" + err.Error()}
			}
			results[index] = result
		}(i, candidate.Host, 443)
	}
	group.Wait()
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK
		}
		if !results[i].OK {
			return results[i].Host < results[j].Host
		}
		return results[i].LatencyMS < results[j].LatencyMS
	})
	writeJSON(w, http.StatusOK, results)
}

func (a *App) testSNI(parent context.Context, request sniTestRequest) (sniTestResult, error) {
	host, err := normalizeSNIHost(request.Host)
	if err != nil {
		return sniTestResult{}, err
	}
	port := request.Port
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return sniTestResult{}, errors.New("端口必须在 1–65535 之间")
	}
	ctx, cancel := context.WithTimeout(parent, sniTestTimeout)
	defer cancel()
	result, err := a.sniProbe(ctx, host, port)
	result.Host, result.Port = host, port
	if err != nil {
		result.OK = false
		result.Message = "不建议使用：" + err.Error()
		return result, nil
	}
	result.OK = true
	if result.LatencyMS <= 250 {
		result.Message = "推荐使用"
	} else {
		result.Message = "可用，延迟较高"
	}
	return result, nil
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
		tlsConn := tls.Client(conn, strictRealityTLSConfig(host))
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			lastErr = describeStrictRealityTLSError(err)
			continue
		}
		state := tlsConn.ConnectionState()
		_ = tlsConn.Close()
		if err := validateStrictRealityTLS(state); err != nil {
			lastErr = err
			continue
		}
		result := sniTestResult{Address: ip.String(), LatencyMS: time.Since(started).Milliseconds(), TLSVersion: tlsVersionName(state.Version), ALPN: state.NegotiatedProtocol}
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

// strictRealityTLSConfig offers only the panel's required TLS profile. Go does
// not expose the selected curve in ConnectionState, so an X25519-only offer is
// the enforcement point for that condition.
func strictRealityTLSConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName:       host,
		MinVersion:       tls.VersionTLS13,
		MaxVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519},
		NextProtos:       []string{"h2"},
	}
}

func validateStrictRealityTLS(state tls.ConnectionState) error {
	if state.Version != tls.VersionTLS13 {
		return errors.New("未协商 TLS 1.3")
	}
	if state.NegotiatedProtocol != "h2" {
		if state.NegotiatedProtocol == "" {
			return errors.New("未协商 h2（目标未提供 HTTP/2 ALPN）")
		}
		return fmt.Errorf("未协商 h2（实际为 %s）", state.NegotiatedProtocol)
	}
	return nil
}

func describeStrictRealityTLSError(err error) error {
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return errors.New("证书域名校验失败")
	}
	var authorityError x509.UnknownAuthorityError
	if errors.As(err, &authorityError) {
		return errors.New("证书链校验失败")
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "protocol version") {
		return errors.New("不支持 TLS 1.3")
	}
	if strings.Contains(message, "curve") || strings.Contains(message, "key share") {
		return errors.New("不支持 X25519 密钥交换")
	}
	return fmt.Errorf("TLS 1.3 / X25519 握手失败：%w", err)
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
