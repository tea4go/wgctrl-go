package wgapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	logs "github.com/tea4go/gh/log4go"

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
	client    Client
	metadata  string
	hideKeys  bool
	version   string
	buildTime string
	platform  string
	apiKey    string

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

// BuildInfo 设置 /api/v1/version 报告的构建时间和目标平台。
func BuildInfo(buildTime, platform string) Option {
	return func(s *Server) {
		s.buildTime = buildTime
		s.platform = platform
	}
}

// APIKey 设置访问 REST API 所需的静态密钥；空字符串表示不启用鉴权。
// 请求必须携带请求头 X-API-Key 并匹配该值，否则返回 401。
func APIKey(key string) Option {
	return func(s *Server) { s.apiKey = key }
}

// NewRestServer 创建一个暴露 WireGuard 设备管理 REST API 的 Server。
//
// metadataPath 用于定位对等节点友好名称的持久化存储，与原生
// WireGuard 配置文件按 interface 对齐：
//   - 传入目录：读写 {metadataPath}/{iface}.names.json（与 {iface}.conf / {iface}.conf.dpapi 并列）
//   - 传入具体 JSON：旧单文件兼容模式（全局一个 JSON 包含所有 interface）
//
// 默认值使用 wgmeta.DefaultPath（原生 WireGuard Configurations 目录）。
func NewRestServer(c Client, metadataPath string, opts ...Option) *Server {
	s := &Server{
		client:   c,
		metadata: metadataPath,
		version:  "wgctrl-go",
	}
	logs.Info("[API] Server Path=%s opts=%d", metadataPath, len(opts))
	for _, opt := range opts {
		opt(s)
	}
	logs.Info("[API] Server Version=%s build=%s/%s",
		s.version, s.buildTime, s.platform)
	logs.Info("[API] Server HideKeys=%v",
		s.hideKeys)
	return s
}

// Handler 返回 REST API 的 HTTP 处理器。
//
// 端点一览：
//
//	GET    /autotest                        自动化测试探活
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
	mux.HandleFunc("/autotest", s.handleAutotest)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/version", s.handleVersion)
	mux.HandleFunc("/api/v1/interfaces", s.handleInterfaces)
	mux.HandleFunc("/api/v1/devices", s.handleDevices)
	mux.HandleFunc("/api/v1/devices/", s.handleDevice)
	mux.HandleFunc("/api/v1/genkey", s.handleGenKey)
	mux.HandleFunc("/api/v1/genpsk", s.handleGenPsk)
	mux.HandleFunc("/api/v1/pubkey", s.handlePubkey)
	return s.withLogging(s.withAuth(mux))
}

// withAuth 是 X-API-Key 静态鉴权中间件。白名单路径：/autotest、/api/v1/health。
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if path == "/autotest" || path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-API-Key")
		if got == "" {
			logs.Warning("[API] 拒绝未鉴权请求 %s %s (X-API-Key 缺失)", r.Method, path)
			w.Header().Set("WWW-Authenticate", "X-API-Key")
			s.writeError(w, http.StatusUnauthorized, errors.New("unauthorized: missing X-API-Key header"))
			return
		}
		if !constantTimeEq(s.apiKey, got) {
			logs.Warning("[API] 拒绝鉴权失败请求 %s %s (X-API-Key 不匹配)", r.Method, path)
			w.Header().Set("WWW-Authenticate", "X-API-Key")
			s.writeError(w, http.StatusUnauthorized, errors.New("unauthorized: invalid X-API-Key"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// constantTimeEq 使用固定时间比较两个字符串，避免时序侧信道泄露。
func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// responseRecorder 包装 http.ResponseWriter 以捕获状态码、响应字节数与响应体摘要。
type responseRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	bytes       int
	body        bytes.Buffer
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	if r.body.Len() < 1024 {
		remaining := 1024 - r.body.Len()
		if n < remaining {
			r.body.Write(b)
		} else {
			r.body.Write(b[:remaining])
		}
	}
	return n, err
}

// withLogging 是 REST API 统一日志中间件，自动在每个请求处理前后打印入/出日志。
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		remote := r.RemoteAddr
		if remote == "" {
			remote = "-"
		}
		bodyStr, newBody := peekRequestBody(r)
		if newBody != nil {
			r.Body = newBody
		}
		logs.Info("[API] → %s %s (from=%s length=%d body=%s)",
			r.Method, fullURL(r), remote, r.ContentLength, bodyStr)

		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		elapsed := time.Since(start)
		logs.Info("[API] ← %s %s (status=%d bytes=%d elapsed=%s body=%s)",
			r.Method, fullURL(r), rec.status, rec.bytes, elapsed, summarize(rec.body.Bytes()))
	})
}

// fullURL 返回包含 RawQuery 的完整请求目标字符串，日志用。
func fullURL(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + r.URL.RawQuery
}

// peekRequestBody 最多读取 1 KB 请求体作为日志摘要，返回摘要字符串与可重放的新 Request.Body。
// 若无请求体（nil / 空）则返回 ("-", nil)。
func peekRequestBody(r *http.Request) (string, io.ReadCloser) {
	if r.Body == nil || r.ContentLength == 0 {
		return "-", nil
	}
	limit := int64(1024)
	if r.ContentLength > 0 && r.ContentLength < limit {
		limit = r.ContentLength
	}
	buf := make([]byte, limit)
	n, err := io.ReadFull(r.Body, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return fmt.Sprintf("<read error: %v>", err), r.Body
	}
	read := buf[:n]
	// 把已读取的部分 + 剩余未读部分拼回新 Body，供原 handler 继续消费。
	remainder, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	joined := make([]byte, 0, len(read)+len(remainder))
	joined = append(joined, read...)
	joined = append(joined, remainder...)
	newBody := io.NopCloser(bytes.NewReader(joined))
	r.ContentLength = int64(len(joined))
	return summarize(read), newBody
}

// summarize 将字节切片格式化为日志友好的摘要，最长 320 字符；
// 纯 ASCII 文本按原样截断，二进制则显示为 hex 摘要。
func summarize(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	isText := true
	for _, c := range b {
		if c < 0x08 || (c > 0x0D && c < 0x20) {
			isText = false
			break
		}
	}
	s := ""
	if isText {
		s = strings.TrimSpace(string(b))
	} else {
		s = fmt.Sprintf("<binary %d bytes>", len(b))
	}
	max := 320
	if len(s) > max {
		s = s[:max-3] + "..."
	}
	// 折叠多余空白，防止日志多行化。
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func (s *Server) handleAutotest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "OK")
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version":    s.version,
		"build_time": s.buildTime,
		"platform":   s.platform,
	})
}

func (s *Server) handleInterfaces(w http.ResponseWriter, _ *http.Request) {
	devices, err := s.client.Devices()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("列出接口失败: %w", err))
		return
	}
	if len(devices) == 0 {
		logs.Warning("[API] /api/v1/interfaces: 本机当前没有任何 WireGuard 接口可用。请先安装/激活 WireGuard（Windows: 官方WireGuardNT驱动+启用隧道 或 用户态命名管道；Linux: ip link add+wg-quick up；macOS: brew install wireguard-tools）")
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
	if len(devices) == 0 {
		logs.Warning("[API] /api/v1/devices: 本机当前没有任何 WireGuard 接口，返回空数组（请先安装/激活 WireGuard）")
	}
	out := make([]Device, 0, len(devices))
	for _, d := range devices {
		if err := wgconf.AttachNames(d, s.metadata); err != nil {
			logs.Warning("[API] 警告: 无法读取节点名称: %v", err)
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
			logs.Warning("[API] 警告: 无法读取节点名称: %v", err)
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
			logs.Warning("[API] 警告: 无法读取节点名称: %v", err)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := wgconfig.Encode(w, d); err != nil {
			logs.Error("[API] 编码配置失败: %v", err)
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
	logs.Error("[API] 错误: %v", err)
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
