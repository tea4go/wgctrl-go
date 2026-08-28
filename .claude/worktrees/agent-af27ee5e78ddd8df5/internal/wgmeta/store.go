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

// fileData 是每个 interface 对应一个 JSON 文件时的扁平结构。
// 不再支持旧版全局 peer-names.json 的嵌套 {interfaces:{...}} 结构。
type fileData struct {
	Version int               `json:"version"`
	Names   map[string]string `json:"names"`
}

// Store 提供对等节点友好名称的持久化读写能力。
//
// 通过 New(basePath) 创建时：
//   - basePath 为目录 → 按 interface 拆分读写：{basePath}/{iface}.names.json
//   - basePath 为 .json 文件 → 始终当作该 interface 的实际路径读写，内容为扁平结构
type Store struct {
	basePath string
}

func New(path string) *Store { return &Store{basePath: path} }

// Names 读取 interfaceName 对应的 {公钥 → 友好名称} 映射。
func (s *Store) Names(interfaceName string) (map[wgtypes.Key]string, error) {
	processMu.Lock()
	defer processMu.Unlock()
	data, err := s.read(ResolveFile(s.basePath, interfaceName))
	if err != nil {
		return nil, err
	}
	return decodeNames(data.Names)
}

// Update 原子地更新 interfaceName 的友好名称映射。
// 若映射被清空（长度为 0），则在底层 JSON 中写入空 names 映射。
func (s *Store) Update(interfaceName string, update func(map[wgtypes.Key]string)) error {
	processMu.Lock()
	defer processMu.Unlock()
	path := ResolveFile(s.basePath, interfaceName)
	lock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	data, err := s.read(path)
	if err != nil {
		return err
	}
	names, err := decodeNames(data.Names)
	if err != nil {
		return err
	}
	update(names)
	data.Names = encodeNames(names)
	return s.write(path, data)
}

func (s *Store) read(path string) (fileData, error) {
	data := fileData{Version: 1, Names: make(map[string]string)}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("读取节点名称元数据 %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return data, fmt.Errorf("解析节点名称元数据 %s: %w", path, err)
	}
	if data.Version != 1 {
		return data, fmt.Errorf("不支持的节点名称元数据版本 %d", data.Version)
	}
	if data.Names == nil {
		data.Names = make(map[string]string)
	}
	return data, nil
}

func (s *Store) write(path string, data fileData) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, b)
}

// atomicWriteFile 原子写入：先写临时文件，fsync 后再 rename 覆盖目标路径。
func atomicWriteFile(path string, b []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	prefix := "." + base + "-"
	tmp, err := os.CreateTemp(dir, prefix)
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
	return os.Rename(tmpName, path)
}

func encodeNames(names map[wgtypes.Key]string) map[string]string {
	encoded := make(map[string]string, len(names))
	for key, name := range names {
		if name == "" {
			continue
		}
		encoded[key.String()] = name
	}
	return encoded
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
