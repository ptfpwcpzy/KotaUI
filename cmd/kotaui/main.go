package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ptfpwcpzy/KotaUI/internal/app"
	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func main() {
	runtime, err := loadRuntime()
	if err != nil {
		log.Fatal(err)
	}
	application, err := app.New(runtime)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := application.ServeSubscription(); err != nil {
			log.Printf("subscription listener: %v", err)
		}
	}()
	log.Printf("KotaUI listening on %s%s", runtime.Listen, runtime.PanelPath)
	if err := application.Serve(); err != nil {
		log.Fatal(err)
	}
}

func loadRuntime() (config.Runtime, error) {
	dataDir := env("KOTAUI_DATA_DIR", "/var/lib/kotaui")
	values := map[string]string{}
	if file, err := os.Open(filepath.Join(dataDir, "runtime.env")); err == nil {
		s := bufio.NewScanner(file)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			pair := strings.SplitN(line, "=", 2)
			if len(pair) == 2 {
				values[pair[0]] = strings.Trim(strings.TrimSpace(pair[1]), "\"")
			}
		}
		_ = file.Close()
	}
	value := func(key, fallback string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		if v := values[key]; v != "" {
			return v
		}
		return fallback
	}
	port := intValue(value("KOTAUI_PANEL_PORT", "1989"), 1989)
	return config.Runtime{DataDir: dataDir, Listen: value("KOTAUI_LISTEN", fmt.Sprintf("0.0.0.0:%d", port)), PanelPath: config.NormalizePath(value("KOTAUI_PANEL_PATH", "ptf")), SubscriptionPort: intValue(value("KOTAUI_SUBSCRIPTION_PORT", "1109"), 1109), Domain: value("KOTAUI_DOMAIN", "127.0.0.1"), CertificateType: value("KOTAUI_CERT_TYPE", "domain"), TLSCert: value("KOTAUI_TLS_CERT", filepath.Join(dataDir, "certs", "fullchain.pem")), TLSKey: value("KOTAUI_TLS_KEY", filepath.Join(dataDir, "certs", "privkey.pem")), AdminUser: value("KOTAUI_ADMIN_USER", "admin"), AdminPassword: value("KOTAUI_ADMIN_PASSWORD", "change-me"), SingBoxBin: value("KOTAUI_SINGBOX_BIN", "/usr/local/bin/sing-box"), SingBoxConfig: value("KOTAUI_SINGBOX_CONFIG", filepath.Join(dataDir, "sing-box", "config.json")), ManageSingBox: value("KOTAUI_MANAGE_SINGBOX", "1") == "1", StatsPort: intValue(value("KOTAUI_STATS_PORT", "9090"), 9090)}, nil
}
func env(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
func intValue(value string, fallback int) int {
	v, e := strconv.Atoi(value)
	if e != nil {
		return fallback
	}
	return v
}
