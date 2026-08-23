package wgmeta

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var processMu sync.Mutex

type fileLock interface {
	Close() error
}

type fileData struct {
	Version    int                          `json:"version"`
	Interfaces map[string]map[string]string `json:"interfaces"`
}

type Store struct {
	path string
}

func New(path string) *Store { return &Store{path: path} }

func (s *Store) Names(interfaceName string) (map[wgtypes.Key]string, error) {
	processMu.Lock()
	defer processMu.Unlock()
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	return decodeNames(data.Interfaces[interfaceName])
}

func (s *Store) Update(interfaceName string, update func(map[wgtypes.Key]string)) error {
	processMu.Lock()
	defer processMu.Unlock()
	lock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	data, err := s.read()
	if err != nil {
		return err
	}
	names, err := decodeNames(data.Interfaces[interfaceName])
	if err != nil {
		return err
	}
	update(names)
	if len(names) == 0 {
		delete(data.Interfaces, interfaceName)
	} else {
		encoded := make(map[string]string, len(names))
		for key, name := range names {
			encoded[key.String()] = name
		}
		data.Interfaces[interfaceName] = encoded
	}
	return s.write(data)
}

func (s *Store) read() (fileData, error) {
	data := fileData{Version: 1, Interfaces: make(map[string]map[string]string)}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("读取节点名称元数据 %s: %w", s.path, err)
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return data, fmt.Errorf("解析节点名称元数据 %s: %w", s.path, err)
	}
	if data.Version != 1 {
		return data, fmt.Errorf("不支持的节点名称元数据版本 %d", data.Version)
	}
	if data.Interfaces == nil {
		data.Interfaces = make(map[string]map[string]string)
	}
	return data, nil
}

func (s *Store) write(data fileData) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".peer-names-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func decodeNames(encoded map[string]string) (map[wgtypes.Key]string, error) {
	names := make(map[wgtypes.Key]string, len(encoded))
	for value, name := range encoded {
		key, err := wgtypes.ParseKey(value)
		if err != nil {
			return nil, fmt.Errorf("解析节点公钥 %q: %w", value, err)
		}
		names[key] = name
	}
	return names, nil
}
