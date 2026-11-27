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
	key  [32]byte
	mu   sync.RWMutex
	data config.State
}

func Open(dataDir, domain string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	key, err := loadOrCreateKey(dataDir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "state.json")
	s := &Store{path: path, key: key}
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = config.DefaultState(domain)
		return s, s.saveLocked()
	}
	if err != nil {
		return nil, err
	}
	data, migrated, _, err := s.loadState(bytes)
	if err != nil {
		// 主文件已损坏：尝试用上一份备份启动，只有两者都坏才报错。
		// 但主文件是解密失败（不是 JSON 损坏）时，备份里必须有能解开的
		// 密文才算回退成功——否则可能是密钥丢了/换了，静默回退到一份
		// 不含敏感字段的旧备份等于悄悄丢数据，不如直接报错。
		bakBytes, bakErr := os.ReadFile(s.path + ".bak")
		if bakErr != nil {
			return nil, err
		}
		bakData, bakMigrated, bakCipher, bakErr := s.loadState(bakBytes)
		if bakErr != nil {
			return nil, err
		}
		if errors.Is(err, errDecrypt) && !bakCipher {
			return nil, err
		}
		log.Printf("store: state.json is corrupt (%v); recovered from state.json.bak", err)
		data, migrated = bakData, bakMigrated
	}
	s.data = data
	if s.data.Inbounds == nil {
		s.data.Inbounds = []config.Inbound{}
	}
	if s.data.Clients == nil {
		s.data.Clients = []config.Client{}
	}
	needsSave := migrated
	if s.data.TrafficCounters == nil {
		s.data.TrafficCounters = map[string]config.TrafficCounters{}
		needsSave = true
	}
	// 旧明文文件读到这里已是明文内存态，立刻存一份加密的回去；
	// 备份也是加密的（见 backup），磁盘上不再留明文。
	if needsSave {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// errDecrypt 表示落盘密文解不开（密钥不对或密文损坏），与 JSON 损坏区分开。
var errDecrypt = errors.New("store: cannot decrypt state")

// loadState 把一份落盘 state.json 内容解成内存态：
// JSON 损坏、密文损坏、密钥不对都返回错误，不静默丢数据。
// 返回值依次是内存态、是否含旧明文字段、是否含密文字段。
func (s *Store) loadState(bytes []byte) (config.State, bool, bool, error) {
	var disk config.State
	if err := json.Unmarshal(bytes, &disk); err != nil {
		return config.State{}, false, false, err
	}
	decrypted, migrated, cipher, err := decryptState(s.key, disk)
	if err != nil {
		return config.State{}, false, false, errors.Join(errDecrypt, err)
	}
	return decrypted, migrated, cipher, nil
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
	// 只加密落盘那份，内存态保持明文；调用方传进来的 state 不改。
	encrypted, err := encryptState(s.key, state)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(encrypted, "", "  ")
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
// 旧明文备份先加密再写，磁盘上不留明文备份；已加密的直接原样拷；
// 读不懂的旧文件（已损坏）原样拷，不丢数据。
func (s *Store) backup() error {
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var disk config.State
	if err := json.Unmarshal(body, &disk); err == nil && hasPlaintext(disk) {
		encrypted, err := encryptState(s.key, disk)
		if err != nil {
			return err
		}
		if encBody, err := json.MarshalIndent(encrypted, "", "  "); err == nil {
			body = encBody
		}
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
