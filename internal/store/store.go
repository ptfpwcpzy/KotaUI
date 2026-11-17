package store

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/ptfpwcpzy/KotaUI/internal/config"
)

type Store struct {
	path string
	mu   sync.RWMutex
	data config.State
}

func Open(dataDir, domain string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "state.json")
	s := &Store{path: path}
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = config.DefaultState(domain)
		return s, s.saveLocked()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bytes, &s.data); err != nil {
		// 主文件已损坏：尝试用上一份备份启动，只有两者都坏才报错。
		bakBytes, bakErr := os.ReadFile(s.path + ".bak")
		if bakErr != nil {
			return nil, err
		}
		var bakData config.State
		if bakErr := json.Unmarshal(bakBytes, &bakData); bakErr != nil {
			return nil, err
		}
		log.Printf("store: state.json is corrupt (%v); recovered from state.json.bak", err)
		s.data = bakData
	}
	if s.data.Inbounds == nil {
		s.data.Inbounds = []config.Inbound{}
	}
	if s.data.Clients == nil {
		s.data.Clients = []config.Client{}
	}
	if s.data.TrafficCounters == nil {
		s.data.TrafficCounters = map[string]config.TrafficCounters{}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Snapshot() config.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

func (s *Store) Update(fn func(*config.State) error) error {
	return s.UpdateWith(fn, nil)
}

// UpdateWith applies a derived artifact for the candidate state before committing it.
// If persistence fails, apply is called again with the previous state as a best-effort rollback.
func (s *Store) UpdateWith(fn func(*config.State) error, apply func(config.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := clone(s.data)
	candidate := clone(s.data)
	if err := fn(&candidate); err != nil {
		return err
	}
	if apply != nil {
		if err := apply(candidate); err != nil {
			return err
		}
	}
	if err := s.save(candidate); err != nil {
		if apply != nil {
			_ = apply(previous)
		}
		return err
	}
	s.data = candidate
	return nil
}

// UpdateNoSave applies fn to the in-memory state without writing state.json.
// Callers that batch frequent small mutations use it and flush with Persist.
func (s *Store) UpdateNoSave(fn func(*config.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := clone(s.data)
	if err := fn(&candidate); err != nil {
		return err
	}
	s.data = candidate
	return nil
}

// Persist writes the current in-memory state to state.json.
func (s *Store) Persist() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(s.data)
}

func (s *Store) saveLocked() error {
	return s.save(s.data)
}

func (s *Store) save(state config.State) error {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// 先备份旧主文件，再写新内容：写盘中途出问题，旧数据仍在 .bak 里。
	if err := s.backup(); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := writeFileSync(tmp, body, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	// 目录也要 fsync，否则断电可能丢掉这次 Rename。
	return syncDir(filepath.Dir(s.path))
}

// backup 把现有 state.json 拷一份到 state.json.bak；主文件不存在时不做任何事。
func (s *Store) backup() error {
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeFileSync(s.path+".bak", body, 0600)
}

// writeFileSync 写文件并 fsync 落盘；fsync 失败当作写错误返回。
func writeFileSync(path string, body []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func clone(in config.State) config.State {
	body, _ := json.Marshal(in)
	var out config.State
	_ = json.Unmarshal(body, &out)
	return out
}
