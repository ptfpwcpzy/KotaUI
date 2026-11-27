package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

const (
	testPrivateKey     = "test-private-key-aaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testServerPassword = "test-server-password"
	testObfsPassword   = "test-obfs-password"
	testCredential     = "11111111-2222-3333-4444-555555555555"
	testTUICPassword   = "test-tuic-password"
	testRelayPassword  = "test-relay-password"
)

func secretState() config.State {
	st := config.DefaultState("example.com")
	st.Inbounds = []config.Inbound{{
		ID: "in1", Name: "test", Type: "reality", Enabled: true,
		PrivateKey: testPrivateKey, ServerPassword: testServerPassword, ObfsPassword: testObfsPassword,
	}}
	st.Clients = []config.Client{{
		ID: "c1", Username: "alice", InboundIDs: []string{"in1"},
		Credentials:   map[string]string{"in1": testCredential},
		TUICPasswords: map[string]string{"in1": testTUICPassword},
	}}
	st.Settings.Relay.Password = testRelayPassword
	return st
}

func diskBody(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func assertNoPlaintext(t *testing.T, body string) {
	t.Helper()
	for _, secret := range []string{testPrivateKey, testServerPassword, testObfsPassword, testCredential, testTUICPassword, testRelayPassword} {
		if strings.Contains(body, secret) {
			t.Fatalf("disk file must not contain plaintext secret %q", secret)
		}
	}
}

// 加密解密往返：内存值不变，磁盘上全是密文。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := secretState()
	if err := s.Update(func(st *config.State) error {
		*st = want
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Inbounds[0].PrivateKey != testPrivateKey || got.Clients[0].Credentials["in1"] != testCredential || got.Settings.Relay.Password != testRelayPassword {
		t.Fatal("in-memory state must stay plaintext")
	}
	body := diskBody(t, dir, "state.json")
	assertNoPlaintext(t, body)
	if !strings.Contains(body, encPrefix) {
		t.Fatal("state.json must contain encrypted values")
	}
	s2, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Snapshot()
	if got.Inbounds[0].PrivateKey != testPrivateKey || got.Inbounds[0].ServerPassword != testServerPassword ||
		got.Inbounds[0].ObfsPassword != testObfsPassword || got.Clients[0].Credentials["in1"] != testCredential ||
		got.Clients[0].TUICPasswords["in1"] != testTUICPassword || got.Settings.Relay.Password != testRelayPassword {
		t.Fatalf("reopened state differs: %#v", got)
	}
}

// 密钥文件：32 字节、0600、重启复用同一把。
func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, "example.com"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "state.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("state.key perm = %o, want 0600", info.Mode().Perm())
	}
	key1, err := os.ReadFile(filepath.Join(dir, "state.key"))
	if err != nil {
		t.Fatal(err)
	}
	if len(key1) != 32 {
		t.Fatalf("state.key length = %d, want 32", len(key1))
	}
	if _, err := Open(dir, "example.com"); err != nil {
		t.Fatal(err)
	}
	key2, err := os.ReadFile(filepath.Join(dir, "state.key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(key1) != string(key2) {
		t.Fatal("state.key must be stable across restarts")
	}
}

// 明文迁移：旧 state.json（无 state.key）打开后自动加密，
// 主文件和备份里都不再有明文，再打开值还在。
func TestPlaintextMigration(t *testing.T) {
	dir := t.TempDir()
	raw, err := json.Marshal(secretState())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got.Inbounds[0].PrivateKey != testPrivateKey || got.Settings.Relay.Password != testRelayPassword {
		t.Fatal("migrated state must keep the plaintext values in memory")
	}
	assertNoPlaintext(t, diskBody(t, dir, "state.json"))
	if _, err := os.Stat(filepath.Join(dir, "state.json.bak")); err == nil {
		assertNoPlaintext(t, diskBody(t, dir, "state.json.bak"))
	}
	s2, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Snapshot(); got.Clients[0].Credentials["in1"] != testCredential {
		t.Fatal("migrated state must survive a second open")
	}
}

// 保存后的备份也是加密的。
func TestBackupIsEncrypted(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	setSecrets := func(st *config.State) error { *st = secretState(); return nil }
	if err := s.Update(setSecrets); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *config.State) error { st.DailyAnchor = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	assertNoPlaintext(t, diskBody(t, dir, "state.json.bak"))
}

// 密钥换了（丢了旧密钥）不能静默丢数据：打开必须报错。
func TestWrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *config.State) error { *st = secretState(); return nil }); err != nil {
		t.Fatal(err)
	}
	other := make([]byte, 32)
	for i := range other {
		other[i] = byte(i + 1)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.key"), other, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, "example.com"); err == nil {
		t.Fatal("Open must fail when state.key does not match state.json")
	}
}

// 空敏感字段不加密（不产生密文），已加密的值不再重复加密。
func TestEncryptValueEdgeCases(t *testing.T) {
	var key [32]byte
	if v, err := encryptValue(key, ""); err != nil || v != "" {
		t.Fatalf("empty must stay empty, got %q, %v", v, err)
	}
	enc, err := encryptValue(key, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, encPrefix) {
		t.Fatalf("encrypted value must carry the prefix, got %q", enc)
	}
	again, err := encryptValue(key, enc)
	if err != nil || again != enc {
		t.Fatalf("encrypt must be idempotent, got %q, %v", again, err)
	}
	plain, err := decryptValue(key, enc)
	if err != nil || plain != "secret" {
		t.Fatalf("decrypt = %q, %v, want secret", plain, err)
	}
	if _, err := decryptValue(key, encPrefix+"!!!not-base64!!!"); err == nil {
		t.Fatal("decrypt must fail on corrupt ciphertext")
	}
}

// 字段级往返：空 map、空值不产生密文也不丢数据。
func TestEncryptStateEmptyFields(t *testing.T) {
	var key [32]byte
	st := config.DefaultState("example.com")
	enc, err := encryptState(key, st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustJSON(t, enc)), encPrefix) {
		t.Fatal("state without secrets must not contain ciphertext")
	}
	dec, _, _, err := decryptState(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Settings.Domain != "example.com" {
		t.Fatal("decrypt must preserve non-secret fields")
	}
}

// 主文件密文损坏、备份完好时，从备份恢复，敏感字段还在。
func TestCorruptCiphertextFallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *config.State) error { *st = secretState(); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(st *config.State) error { st.DailyAnchor = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	body := diskBody(t, dir, "state.json")
	idx := strings.Index(body, encPrefix)
	if idx < 0 {
		t.Fatal("state.json must contain ciphertext")
	}
	corrupt := body[:idx+len(encPrefix)+4] + "XXXX" + body[idx+len(encPrefix)+8:]
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(corrupt), 0600); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir, "example.com")
	if err != nil {
		t.Fatalf("Open must recover from backup: %v", err)
	}
	if got := s2.Snapshot(); got.Inbounds[0].PrivateKey != testPrivateKey || got.Clients[0].Credentials["in1"] != testCredential {
		t.Fatal("recovered state must keep the secrets")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
