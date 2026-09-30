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

func TestLoadRuntimeReadsShellQuotedRuntimeEnv(t *testing.T) {
	dir := t.TempDir()
	// 与 install.sh 写入格式一致：单引号包裹，内含 '\'' 转义
	body := "KOTAUI_PANEL_PORT=1989\nKOTAUI_LISTEN=0.0.0.0:1989\nKOTAUI_SUBSCRIPTION_PORT=1109\nKOTAUI_STATS_PORT=9090\n" +
		"KOTAUI_ADMIN_USER='admin'\nKOTAUI_ADMIN_PASSWORD='p@ss'\\''w0rd'\nKOTAUI_DOMAIN=\"example.test\"\n"
	if err := os.WriteFile(filepath.Join(dir, "runtime.env"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KOTAUI_DATA_DIR", dir)
	t.Setenv("KOTAUI_ADMIN_PASSWORD", "")
	t.Setenv("KOTAUI_ADMIN_USER", "")
	t.Setenv("KOTAUI_PANEL_PORT", "")
	t.Setenv("KOTAUI_SUBSCRIPTION_PORT", "")
	t.Setenv("KOTAUI_STATS_PORT", "")
	t.Setenv("KOTAUI_LISTEN", "")
	t.Setenv("KOTAUI_DOMAIN", "")
	runtime, err := loadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.AdminUser != "admin" {
		t.Fatalf("single-quoted user not unquoted: %q", runtime.AdminUser)
	}
	if runtime.AdminPassword != "p@ss'w0rd" {
		t.Fatalf("escaped single quote not handled: %q", runtime.AdminPassword)
	}
	if runtime.Domain != "example.test" {
		t.Fatalf("double-quoted domain not unquoted: %q", runtime.Domain)
	}
}

func TestShellUnquote(t *testing.T) {
	cases := map[string]string{
		`admin`:        `admin`,
		`'admin'`:      `admin`,
		`"admin"`:      `admin`,
		`'a'\''b'`:     `a'b`,
		`"a\"b"`:       `a"b`,
		`it's`:         `it's`,
		`'unbalanced`:  `'unbalanced`,
		`p@ss=w0rd`:    `p@ss=w0rd`,
		`'p@ss=w0rd'`:  `p@ss=w0rd`,
		`  'spaced'  `: `spaced`,
		``:             ``,
		`''`:           ``,
		`'a'b`:         `'a'b`,
	}
	for in, want := range cases {
		if got := shellUnquote(in); got != want {
			t.Errorf("shellUnquote(%q) = %q, want %q", in, got, want)
		}
	}
}
