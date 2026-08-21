package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskProgressRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.status")
	if err := writeTaskProgress(path, "123", "running", "正在构建核心…"); err != nil {
		t.Fatal(err)
	}
	got := readTaskProgress(path)
	if got.RunID != "123" || got.State != "running" || got.Message != "正在构建核心…" || got.UpdatedAt == 0 {
		t.Fatalf("unexpected task progress: %#v", got)
	}
}

func TestTaskProgressRejectsUnknownState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.status")
	if err := os.WriteFile(path, []byte("paused\n不应被当作有效任务\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readTaskProgress(path); got.State != "" {
		t.Fatalf("unexpected unknown task state: %#v", got)
	}
	if err := writeTaskProgress(path, "", "paused", "不应写入"); err == nil {
		t.Fatal("expected invalid task state error")
	}
}
