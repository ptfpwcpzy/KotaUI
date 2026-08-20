package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

// taskProgress is the common on-disk contract for maintenance work. The
// legacy two-line state/message format remains readable so an upgraded panel
// can finish or report work launched by an older installed script.
type taskProgress struct {
	RunID     string `json:"runId,omitempty"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

func writeTaskProgress(path, runID, state, message string) error {
	if !validTaskState(state) {
		return os.ErrInvalid
	}
	progress := taskProgress{
		RunID:     strings.TrimSpace(runID),
		State:     strings.TrimSpace(state),
		Message:   strings.TrimSpace(message),
		UpdatedAt: time.Now().Unix(),
	}
	body, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	temporary := path + ".tmp." + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(temporary, append(body, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func readTaskProgress(path string) taskProgress {
	body, err := os.ReadFile(path)
	if err != nil {
		return taskProgress{}
	}
	var progress taskProgress
	if json.Unmarshal(body, &progress) == nil && validTaskState(progress.State) {
		return progress
	}
	parts := strings.SplitN(strings.TrimSpace(string(body)), "\n", 2)
	if len(parts) == 0 || !validTaskState(parts[0]) {
		return taskProgress{}
	}
	progress.State = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		progress.Message = strings.TrimSpace(parts[1])
	}
	if info, statErr := os.Stat(path); statErr == nil {
		progress.UpdatedAt = info.ModTime().Unix()
	}
	return progress
}

func validTaskState(value string) bool {
	switch strings.TrimSpace(value) {
	case "running", "success", "failed":
		return true
	default:
		return false
	}
}
