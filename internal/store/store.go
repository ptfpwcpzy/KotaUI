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

func (s *Store) saveLocked() error {
	return s.save(s.data)
}

func (s *Store) save(state config.State) error {
	body, err := json.MarshalIndent(state, "", "  ")
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
