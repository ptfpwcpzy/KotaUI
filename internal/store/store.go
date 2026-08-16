package store

import (
	"encoding/json"
	"errors"
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
		return nil, err
	}
	if s.data.Inbounds == nil {
		s.data.Inbounds = []config.Inbound{}
	}
	if s.data.Clients == nil {
		s.data.Clients = []config.Client{}
	}
	return s, nil
}

func (s *Store) Snapshot() config.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

func (s *Store) Update(fn func(*config.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := clone(s.data)
	if err := fn(&candidate); err != nil {
		return err
	}
	s.data = candidate
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	body, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clone(in config.State) config.State {
	body, _ := json.Marshal(in)
	var out config.State
	_ = json.Unmarshal(body, &out)
	return out
}
