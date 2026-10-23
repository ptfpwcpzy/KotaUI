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
