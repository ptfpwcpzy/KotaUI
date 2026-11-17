package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

func TestUpdateNoSaveAndPersist(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNoSave(func(st *config.State) error {
		st.DailyAnchor = 4242
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().DailyAnchor; got != 4242 {
		t.Fatalf("in-memory anchor = %d, want 4242", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "4242") {
		t.Fatal("state.json must not contain the unflushed value")
	}
	if err := s.Persist(); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "4242") {
		t.Fatal("state.json must contain the value after Persist")
	}
}

func setAnchor(t *testing.T, s *Store, v int64) {
	t.Helper()
	if err := s.Update(func(st *config.State) error {
		st.DailyAnchor = v
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// 每次保存前，旧 state.json 必须备份到 state.json.bak。
func TestSaveCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	bakPath := filepath.Join(dir, "state.json.bak")
	if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
		t.Fatal("no backup expected before the first overwrite")
	}
	setAnchor(t, s, 1111)
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("state.json.bak must exist after a save: %v", err)
	}
	setAnchor(t, s, 2222)
	if !strings.Contains(readFile(t, bakPath), "1111") {
		t.Fatal("backup must hold the previous generation (1111)")
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "state.json")), "2222") {
		t.Fatal("state.json must hold the latest value (2222)")
	}
}

// 主文件损坏时用备份启动，恢复到上一代状态。
func TestOpenFallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	setAnchor(t, s, 7777)
	setAnchor(t, s, 8888)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("not json{"), 0600); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir, "example.com")
	if err != nil {
		t.Fatalf("Open must recover from backup: %v", err)
	}
	if got := s2.Snapshot().DailyAnchor; got != 7777 {
		t.Fatalf("recovered anchor = %d, want 7777 (previous generation)", got)
	}
}

// 主文件和备份都坏了才返回错误。
func TestOpenBothCorruptReturnsError(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	setAnchor(t, s, 1)
	for _, name := range []string{"state.json", "state.json.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not json{"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(dir, "example.com"); err == nil {
		t.Fatal("Open must fail when both state.json and state.json.bak are corrupt")
	}
}
