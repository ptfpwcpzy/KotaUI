package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

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
		if err := application.ServeStandardSubscription(); err != nil {
			log.Printf("standard HTTPS subscription listener: %v", err)
		}
	}()
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
	values, err := readRuntimeEnv(filepath.Join(dataDir, "runtime.env"))
	if err != nil {
		return config.Runtime{}, err
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
	panelPort, err := portValue("KOTAUI_PANEL_PORT", value("KOTAUI_PANEL_PORT", "1989"), 1024)
	if err != nil {
		return config.Runtime{}, err
	}
	subscriptionPort, err := portValue("KOTAUI_SUBSCRIPTION_PORT", value("KOTAUI_SUBSCRIPTION_PORT", "1109"), 1)
	if err != nil {
		return config.Runtime{}, err
	}
	standardSubscriptionPort, err := portValue("KOTAUI_SUBSCRIPTION_HTTPS_PORT", value("KOTAUI_SUBSCRIPTION_HTTPS_PORT", "443"), 1)
	if err != nil {
		return config.Runtime{}, err
	}
	statsPort, err := portValue("KOTAUI_STATS_PORT", value("KOTAUI_STATS_PORT", "9090"), 1)
	if err != nil {
		return config.Runtime{}, err
	}
	if subscriptionPort == panelPort || standardSubscriptionPort == panelPort || statsPort == panelPort || statsPort == subscriptionPort || statsPort == standardSubscriptionPort || subscriptionPort == standardSubscriptionPort {
		return config.Runtime{}, fmt.Errorf("面板、订阅、标准订阅和统计端口不能重复")
	}
	listen := value("KOTAUI_LISTEN", fmt.Sprintf("0.0.0.0:%d", panelPort))
	_, listenPort, err := net.SplitHostPort(listen)
	if err != nil {
		return config.Runtime{}, fmt.Errorf("KOTAUI_LISTEN 无效：%w", err)
	}
	if listenPort != strconv.Itoa(panelPort) {
		return config.Runtime{}, fmt.Errorf("KOTAUI_LISTEN 端口必须与 KOTAUI_PANEL_PORT 一致")
	}
	adminUser := strings.TrimSpace(value("KOTAUI_ADMIN_USER", "admin"))
	adminPassword := value("KOTAUI_ADMIN_PASSWORD", "change-me")
	if adminUser == "" {
		return config.Runtime{}, fmt.Errorf("管理员账号不能为空")
	}
	if adminPassword == "change-me" || utf8.RuneCountInString(adminPassword) < 8 {
		return config.Runtime{}, fmt.Errorf("管理员密码至少需要 8 个字符，且不能使用默认密码")
	}
	domain := strings.TrimSpace(value("KOTAUI_DOMAIN", "127.0.0.1"))
	if domain == "" {
		return config.Runtime{}, fmt.Errorf("KOTAUI_DOMAIN 不能为空")
	}
	return config.Runtime{
		DataDir:               dataDir,
		Listen:                listen,
		PanelPath:             config.NormalizePath(value("KOTAUI_PANEL_PATH", "ptf")),
		SubscriptionPort:      subscriptionPort,
		SubscriptionHTTPSPort: standardSubscriptionPort,
		Domain:                domain,
		CertificateType:       value("KOTAUI_CERT_TYPE", "domain"),
		TLSCert:               value("KOTAUI_TLS_CERT", filepath.Join(dataDir, "certs", "fullchain.pem")),
		TLSKey:                value("KOTAUI_TLS_KEY", filepath.Join(dataDir, "certs", "privkey.pem")),
		AdminUser:             adminUser,
		AdminPassword:         adminPassword,
		SingBoxBin:            value("KOTAUI_SINGBOX_BIN", "/usr/local/bin/sing-box"),
		SingBoxConfig:         value("KOTAUI_SINGBOX_CONFIG", filepath.Join(dataDir, "sing-box", "config.json")),
		ManageSingBox:         value("KOTAUI_MANAGE_SINGBOX", "1") == "1",
		StatsPort:             statsPort,
	}, nil
}

func readRuntimeEnv(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pair := strings.SplitN(line, "=", 2)
		if len(pair) == 2 {
			values[pair[0]] = strings.Trim(strings.TrimSpace(pair[1]), "\"")
		}
	}
	return values, scanner.Err()
}

func portValue(name, raw string, minimum int) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port < minimum || port > 65535 {
		return 0, fmt.Errorf("%s 必须是 %d–65535 的端口", name, minimum)
	}
	return port, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
