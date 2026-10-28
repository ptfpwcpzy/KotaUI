package proxy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func TestWriteAddsClashAPI(t *testing.T) {
	directory := t.TempDir()
	runtime := config.Runtime{DataDir: directory, SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090}
	state := config.DefaultState("example.test")
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Experimental struct {
			ClashAPI struct {
				ExternalController string `json:"external_controller"`
				Secret             string `json:"secret"`
				SiteLogPath        string `json:"site_log_path"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	clash := decoded.Experimental.ClashAPI
	if clash.ExternalController != "127.0.0.1:19091" {
		t.Fatalf("clash controller = %q, want 127.0.0.1:19091", clash.ExternalController)
	}
	if clash.SiteLogPath != filepath.Join(directory, "site-records.jsonl") {
		t.Fatalf("site_log_path = %q", clash.SiteLogPath)
	}
	if len(clash.Secret) != 32 {
		t.Fatalf("clash secret length = %d, want 32 hex chars", len(clash.Secret))
	}
	stored, err := os.ReadFile(ClashSecretFile(directory))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(stored)) != clash.Secret {
		t.Fatal("clash secret must be stored and reused")
	}
	info, err := os.Stat(ClashSecretFile(directory))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("clash secret perm = %o, want 0600", info.Mode().Perm())
	}
	// 第二次写配置，密钥不变。
	if err := Write(state, runtime); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(runtime.SingBoxConfig)
	if err != nil {
		t.Fatal(err)
	}
	var second struct {
		Experimental struct {
			ClashAPI struct {
				Secret string `json:"secret"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatal(err)
	}
	if second.Experimental.ClashAPI.Secret != clash.Secret {
		t.Fatal("clash secret must be stable across writes")
	}
}

func TestClashPort(t *testing.T) {
	if got := ClashPort(19090); got != 19091 {
		t.Fatalf("ClashPort(19090) = %d, want 19091", got)
	}
	if got := ClashPort(0); got != 19091 {
		t.Fatalf("ClashPort(0) = %d, want 19091", got)
	}
	if got := ClashPort(65535); got != 19091 {
		t.Fatalf("ClashPort(65535) = %d, want 19091", got)
	}
}

func testSelfSignedCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"example.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "fullchain.pem")
	keyPath = filepath.Join(dir, "privkey.pem")
	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	_ = certOut.Close()
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}); err != nil {
		t.Fatal(err)
	}
	_ = keyOut.Close()
	return certPath, keyPath
}

// TestWriteConfigMatrixPassesSingBoxCheck 覆盖面板能生成的全部配置变体：
// 4 种入站协议 × 中转关/落地/中转全部/中转分类，外加域名封禁组合。
// 每种都用与部署版本一致的核心跑 sing-box check。
func TestWriteConfigMatrixPassesSingBoxCheck(t *testing.T) {
	binary := os.Getenv("KOTAUI_SINGBOX_STATS_BIN")
	if binary == "" {
		t.Skip("set KOTAUI_SINGBOX_STATS_BIN to run against a compatible sing-box core")
	}
	certPath, keyPath := testSelfSignedCert(t)
	realityKeyBytes := make([]byte, 32)
	if _, err := rand.Read(realityKeyBytes); err != nil {
		t.Fatal(err)
	}
	// sing-box generate reality-keypair 输出 base64url 密钥，标准 base64 的
	// + / 填补会被校验拒绝。
	realityKey := base64.RawURLEncoding.EncodeToString(realityKeyBytes)
	ssServerKey := config.RandomBase64(32)
	ssUserKey := config.RandomBase64(32)
	tuicUUID := config.RandomUUID()
	type protocolCase struct {
		name    string
		inbound config.Inbound
		client  config.Client
	}
	protocols := []protocolCase{
		{"reality", config.Inbound{ID: "in-reality", Name: "reality", Type: "reality", Listen: "0.0.0.0", Port: 24443, Enabled: true, SNI: "example.test", HandshakeServer: "example.test", HandshakePort: 443, PrivateKey: realityKey, ShortID: "abcd1234"},
			config.Client{Username: "alice", Credentials: map[string]string{"in-reality": config.RandomUUID()}}},
		{"hysteria2", config.Inbound{ID: "in-hy2", Name: "hy2", Type: "hysteria2", Listen: "0.0.0.0", Port: 24444, Enabled: true, UpMbps: 100, DownMbps: 100},
			config.Client{Username: "alice", Credentials: map[string]string{"in-hy2": "hy2secret"}}},
		{"tuic", config.Inbound{ID: "in-tuic", Name: "tuic", Type: "tuic", Listen: "0.0.0.0", Port: 24445, Enabled: true},
			config.Client{Username: "alice", Credentials: map[string]string{"in-tuic": tuicUUID}, TUICPasswords: map[string]string{"in-tuic": "tuicsecret"}}},
		{"shadowsocks2022", config.Inbound{ID: "in-ss", Name: "ss", Type: "shadowsocks2022", Listen: "0.0.0.0", Port: 24446, Enabled: true, ServerPassword: ssServerKey},
			config.Client{Username: "alice", Credentials: map[string]string{"in-ss": ssUserKey}}},
	}
	relays := []struct {
		name  string
		relay config.RelaySettings
	}{
		{"off", config.RelaySettings{}},
		{"landing", config.RelaySettings{Enabled: true, Role: "landing", Port: 41234, Username: "relay-abc", Password: "0123456789abcdef"}},
		{"transit-all", config.RelaySettings{Enabled: true, Role: "transit", Host: "127.0.0.1", Port: 41234, Username: "relay-abc", Password: "0123456789abcdef", Mode: "all"}},
		{"transit-categories", config.RelaySettings{Enabled: true, Role: "transit", Host: "127.0.0.1", Port: 41234, Username: "relay-abc", Password: "0123456789abcdef", Mode: "categories", Categories: []string{"youtube", "telegram"}}},
	}
	for _, protocol := range protocols {
		for _, relay := range relays {
			name := protocol.name + "/" + relay.name
			t.Run(name, func(t *testing.T) {
				directory := t.TempDir()
				state := config.DefaultState("example.test")
				state.Inbounds = []config.Inbound{protocol.inbound}
				state.Clients = []config.Client{protocol.client}
				state.Settings.Relay = relay.relay
				runtime := config.Runtime{DataDir: directory, SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090, TLSCert: certPath, TLSKey: keyPath}
				if err := Write(state, runtime); err != nil {
					t.Fatal(err)
				}
				command := exec.Command(binary, "check", "-c", runtime.SingBoxConfig)
				var logs bytes.Buffer
				command.Stdout = &logs
				command.Stderr = &logs
				if err := command.Run(); err != nil {
					t.Fatalf("sing-box check failed on %s: %v; output: %s", name, err, logs.String())
				}
			})
		}
	}
	t.Run("blocked", func(t *testing.T) {
		directory := t.TempDir()
		state := config.DefaultState("example.test")
		state.Inbounds = []config.Inbound{protocols[0].inbound}
		state.Clients = []config.Client{protocols[0].client}
		state.Settings.BlockedDomains = []string{"example.com", "tracker.example"}
		state.Settings.BlockBitTorrent = true
		runtime := config.Runtime{DataDir: directory, SingBoxConfig: filepath.Join(directory, "config.json"), StatsPort: 19090, TLSCert: certPath, TLSKey: keyPath}
		if err := Write(state, runtime); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(binary, "check", "-c", runtime.SingBoxConfig)
		var logs bytes.Buffer
		command.Stdout = &logs
		command.Stderr = &logs
		if err := command.Run(); err != nil {
			t.Fatalf("sing-box check failed on blocked variant: %v; output: %s", err, logs.String())
		}
	})
}
