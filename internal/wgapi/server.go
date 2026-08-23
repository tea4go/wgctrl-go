package wgapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconf"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconfig"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Client 是 REST 服务所需的 WireGuard 设备控制接口。
// wgctrl.Client 满足该接口。
type Client interface {
	Devices() ([]*wgtypes.Device, error)
	Device(string) (*wgtypes.Device, error)
	ConfigureDevice(string, wgtypes.Config) error
}

// A Server 通过 REST API 暴露 WireGuard 设备管理能力，
// 覆盖 wg(8) 全部子命令对应的操作。
type Server struct {
	client   Client
	metadata string
	hideKeys bool
	version  string
	logf     *log.Logger

	// mu 串行化所有写操作，避免并发配置设备与元数据。
	mu sync.Mutex
}

// Option 配置 Server 的可选行为。
type Option func(*Server)

// HideKeys 使设备查询响应中省略私钥与预共享密钥。
func HideKeys() Option {
	return func(s *Server) { s.hideKeys = true }
}

// Version 设置 /api/v1/version 报告的版本字符串。
func Version(v string) Option {
	return func(s *Server) { s.version = v }
}

// Logger 设置服务日志输出；nil 表示丢弃日志。
func Logger(l *log.Logger) Option {
	return func(s *Server) { s.logf = l }
}

// New 创建一个暴露 WireGuard 设备管理 REST API 的 Server。
//
// metadataPath 用于持久化对等节点名称元数据，与 wg 命令行工具
// 共享同一存储（默认使用 wgmeta.DefaultPath）。
func New(c Client, metadataPath string, opts ...Option) *Server {
	s := &Server{
		client:   c,
		metadata: metadataPath,
		version:  "wgctrl-go",
		logf:     log.New(io.Discard, "", 0),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Handler 返回 REST API 的 HTTP 处理器。
//
// 端点一览：
//
//	GET    /api/v1/health                   服务健康检查
//	GET    /api/v1/version                  wg version
//	GET    /api/v1/interfaces               wg show interfaces
//	GET    /api/v1/devices                  wg show all
//	GET    /api/v1/devices/{name}           wg show {name}
//	POST   /api/v1/devices/{name}           wg set（结构化 JSON 配置）
//	GET    /api/v1/devices/{name}/conf      wg showconf
//	PUT    /api/v1/devices/{name}/conf      wg setconf/syncconf（?mode=set|sync）
//	POST   /api/v1/devices/{name}/conf      wg addconf（?mode=add|set|sync）
//	POST   /api/v1/genkey                   wg genkey
//	POST   /api/v1/genpsk                   wg genpsk
//	POST   /api/v1/pubkey                   wg pubkey
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/version", s.handleVersion)
	mux.HandleFunc("/api/v1/interfaces", s.handleInterfaces)
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/devices/", s.handleDevice)
	mux.HandleFunc("/api/v1/genkey", s.handleGenKey)
	mux.HandleFunc("/api/v1/genpsk", s.handleGenPsk)
	mux.HandleFunc("/api/v1/pubkey", s.handlePubkey)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

func (s *Server) handleInterfaces(w http.ResponseWriter, _ *http.Request) {
	devices, err := s.client.Devices()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("列出接口失败: %w", err))
		return
	}
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, d.Name)
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	devices, err := s.client.Devices()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("列出接口失败: %w", err))
		return
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		if err := wgconf.AttachNames(d, s.metadata); err != nil {
			s.logf.Printf("警告: 无法读取节点名称: %v", err)
		}
		out = append(out, deviceToJSON(d, s.hideKeys))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDevice 处理 /api/v1/devices/{name}[/conf] 的请求。
func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	rest, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/v1/devices/"))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("无效的设备名称: %w", err))
		return
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		s.writeError(w, http.StatusNotFound, errors.New("未指定设备名称"))
		return
	}
	name := parts[0]
	switch {
	case len(parts) == 1:
		s.handleDeviceResource(w, r, name)
	case len(parts) == 2 && parts[1] == "conf":
		s.handleDeviceConf(w, r, name)
	default:
		s.writeError(w, http.StatusNotFound, fmt.Errorf("未知的路径: %s", r.URL.Path))
	}
}

func (s *Server) handleDeviceResource(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		d, err := s.client.Device(name)
		if err != nil {
			s.writeError(w, deviceErrorStatus(err), fmt.Errorf("无法访问接口 %s: %w", name, err))
			return
		}
		if err := wgconf.AttachNames(d, s.metadata); err != nil {
			s.logf.Printf("警告: 无法读取节点名称: %v", err)
		}
		writeJSON(w, http.StatusOK, deviceToJSON(d, s.hideKeys))
	case http.MethodPost:
		s.mu.Lock()
		defer s.mu.Unlock()
		var cfgJSON Config
		if err := decodeJSON(w, r, &cfgJSON); err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Errorf("解析配置失败: %w", err))
			return
		}
		cfg, err := cfgJSON.ParseConfig()
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := wgconf.Apply(s.client, name, cfg, s.metadata); err != nil {
			s.writeError(w, deviceErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		methodNotAllowed(w, "GET, POST")
	}
}

func (s *Server) handleDeviceConf(w http.ResponseWriter, r *http.Request, name string) {
	switch r.Method {
	case http.MethodGet:
		d, err := s.client.Device(name)
		if err != nil {
			s.writeError(w, deviceErrorStatus(err), fmt.Errorf("无法访问接口 %s: %w", name, err))
			return
		}
		if err := wgconf.AttachNames(d, s.metadata); err != nil {
			s.logf.Printf("警告: 无法读取节点名称: %v", err)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := wgconfig.Encode(w, d); err != nil {
			s.logf.Printf("编码配置失败: %v", err)
		}
	case http.MethodPut, http.MethodPost:
		mode := r.URL.Query().Get("mode")
		if r.Method == http.MethodPut && mode == "" {
			mode = "set"
		}
		if r.Method == http.MethodPost && mode == "" {
			mode = "add"
		}
		appendMode, replacePeers, syncPeers, err := confMode(mode)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err)
			return
		}
		body := http.MaxBytesReader(w, r.Body, 4<<20)
		cfg, err := wgconfig.Parse(body, appendMode)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Errorf("解析配置失败: %w", err))
			return
		}
		cfg.ReplacePeers = replacePeers
		s.mu.Lock()
		defer s.mu.Unlock()
		if syncPeers {
			err = wgconf.Sync(s.client, name, cfg, s.metadata)
		} else {
			err = wgconf.Apply(s.client, name, cfg, s.metadata)
		}
		if err != nil {
			s.writeError(w, deviceErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		methodNotAllowed(w, "GET, PUT, POST")
	}
}

// confMode 将 REST 的 mode 参数映射为 wg(8) 配置应用语义。
func confMode(mode string) (appendMode, replacePeers, syncPeers bool, err error) {
	switch mode {
	case "add":
		return true, false, false, nil
	case "set":
		return false, true, false, nil
	case "sync":
		return false, false, true, nil
	default:
		return false, false, false, fmt.Errorf("无效的 mode: `%s'（可选 add/set/sync）", mode)
	}
}

func (s *Server) handleGenKey(w http.ResponseWriter, _ *http.Request) {
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("生成私钥失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key.String()})
}

func (s *Server) handleGenPsk(w http.ResponseWriter, _ *http.Request) {
	key, err := wgtypes.GenerateKey()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("生成预共享密钥失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key.String()})
}

func (s *Server) handlePubkey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("解析请求失败: %w", err))
		return
	}
	key, err := wgtypes.ParseKey(strings.TrimSpace(req.Key))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Errorf("解析私钥失败: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key.PublicKey().String()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	body := http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func deviceErrorStatus(err error) int {
	if errors.Is(err, os.ErrNotExist) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "不支持的 HTTP 方法"})
}

func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.logf.Printf("错误: %v", err)
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 响应头已写出，只能记录日志。
		// 由调用方负责在 writeJSON 前输出日志（若有 logger）。
		_ = err
	}
}
