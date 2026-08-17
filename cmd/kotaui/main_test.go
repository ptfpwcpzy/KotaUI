package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeRejectsWeakDefaultPassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KOTAUI_DATA_DIR", dir)
	t.Setenv("KOTAUI_ADMIN_PASSWORD", "change-me")
	if _, err := loadRuntime(); err == nil {
		t.Fatal("expected weak default password to fail")
	}
}

func TestLoadRuntimeRejectsDuplicatePorts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KOTAUI_DATA_DIR", dir)
	t.Setenv("KOTAUI_ADMIN_PASSWORD", "strong-password")
	t.Setenv("KOTAUI_PANEL_PORT", "1989")
	t.Setenv("KOTAUI_SUBSCRIPTION_PORT", "1989")
	if _, err := loadRuntime(); err == nil {
		t.Fatal("expected duplicate ports to fail")
	}
}

func TestLoadRuntimeReadsValidRuntimeEnv(t *testing.T) {
	dir := t.TempDir()
	body := "KOTAUI_PANEL_PORT=1989\nKOTAUI_LISTEN=0.0.0.0:1989\nKOTAUI_SUBSCRIPTION_PORT=1109\nKOTAUI_STATS_PORT=9090\nKOTAUI_ADMIN_USER=admin\nKOTAUI_ADMIN_PASSWORD=strong-password\nKOTAUI_DOMAIN=example.test\n"
	if err := os.WriteFile(filepath.Join(dir, "runtime.env"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KOTAUI_DATA_DIR", dir)
	t.Setenv("KOTAUI_ADMIN_PASSWORD", "")
	t.Setenv("KOTAUI_PANEL_PORT", "")
	t.Setenv("KOTAUI_SUBSCRIPTION_PORT", "")
	t.Setenv("KOTAUI_STATS_PORT", "")
	t.Setenv("KOTAUI_LISTEN", "")
	t.Setenv("KOTAUI_DOMAIN", "")
	runtime, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Listen != "0.0.0.0:1989" || runtime.SubscriptionPort != 1109 || runtime.StatsPort != 9090 || runtime.AdminPassword != "strong-password" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
}
