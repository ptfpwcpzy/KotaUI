package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// ClashPort returns the loopback port for the clash API controller, derived
// from the stats port so no extra configuration knob is needed. The
// controller only ever binds 127.0.0.1 and is reserved against inbound
// ports like the stats port is.
func ClashPort(statsPort int) int {
	if statsPort <= 0 || statsPort >= 65535 {
		return 19091
	}
	return statsPort + 1
}

// ClashSecretFile is the 0600 file holding the clash API secret.
func ClashSecretFile(dataDir string) string {
	return filepath.Join(dataDir, "clash-secret")
}

// SiteRecordsPath is the JSON-lines file the patched core appends one
// record per closed connection to, and the panel tails for site rankings.
func SiteRecordsPath(dataDir string) string {
	return filepath.Join(dataDir, "site-records.jsonl")
}

// EnsureClashSecret returns the stable secret the panel writes into the
// generated sing-box config and uses to query the clash API. It lives in
// its own file instead of being derived from the admin password, so
// changing the password never desyncs the panel from the core.
func EnsureClashSecret(dataDir string) (string, error) {
	if dataDir == "" {
		return "", os.ErrInvalid
	}
	path := ClashSecretFile(dataDir)
	if raw, err := os.ReadFile(path); err == nil {
		if secret := strings.TrimSpace(string(raw)); secret != "" {
			return secret, nil
		}
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	encoded := hex.EncodeToString(secret)
	if err := os.WriteFile(path, []byte(encoded), 0600); err != nil {
		return "", err
	}
	return encoded, nil
}

// ReadClashSecret returns the stored clash API secret, or "" when none has
// been created yet (core not yet configured by this panel version).
func ReadClashSecret(dataDir string) string {
	raw, err := os.ReadFile(ClashSecretFile(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
