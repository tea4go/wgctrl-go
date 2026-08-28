package wgapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgtest"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type fakeClient struct {
	devices    []*wgtypes.Device
	device     *wgtypes.Device
	configured wgtypes.Config
	err        error
}

func (c *fakeClient) Devices() ([]*wgtypes.Device, error) { return c.devices, c.err }
func (c *fakeClient) Device(string) (*wgtypes.Device, error) {
	return c.device, c.err
}
func (c *fakeClient) ConfigureDevice(_ string, cfg wgtypes.Config) error {
	c.configured = cfg
	return c.err
}

// defaultTestAPIKey 是历史 newTestServer 用例使用的固定 key；会被 autoAuthed 包装自动注入请求头。
const defaultTestAPIKey = "test-server-default-api-key"

func newTestServer(t *testing.T, c Client) (*httptest.Server, string) {
	t.Helper()
	// 旧用例全部默认注入一个固定 key，并在 httptest.Server 外层自动补 header，保持原编写风格。
	srv := NewRestServer(c, t.TempDir(), APIKey(defaultTestAPIKey))
	ts := httptest.NewServer(autoAuthed{next: srv.Handler()})
	t.Cleanup(ts.Close)
	return ts, srv.metadata
}

// autoAuthed 在把请求交给真实 Handler 前自动注入默认 X-API-Key，
// 让大量历史用例无需逐个加 header；若请求里已经有 header 则保持原样。
type autoAuthed struct{ next http.Handler }

func (a autoAuthed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") == "" {
		r.Header.Set("X-API-Key", defaultTestAPIKey)
	}
	a.next.ServeHTTP(w, r)
}

func sampleDevice() *wgtypes.Device {
	priv := wgtest.MustPrivateKey()
	peerKey := wgtest.MustPrivateKey().PublicKey()
	endpoint := &net.UDPAddr{IP: net.ParseIP("203.0.113.1"), Port: 51820}
	return &wgtypes.Device{
		Name:          "wg0",
		Type:          wgtypes.Userspace,
		PrivateKey:    priv,
		HasPrivateKey: true,
		PublicKey:     priv.PublicKey(),
		HasPublicKey:  true,
		ListenPort:    51820,
		HasListenPort: true,
		Peers: []wgtypes.Peer{{
			PublicKey:                   peerKey,
			Endpoint:                    endpoint,
			HasEndpoint:                 true,
			PersistentKeepaliveInterval: 25 * time.Second,
			LastHandshakeTime:           time.Unix(1700000000, 0),
			ReceiveBytes:                1024,
			TransmitBytes:               2048,
			AllowedIPs:                  []net.IPNet{{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(24, 32)}},
		}},
	}
}

func TestHealth(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/health", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestAutotest(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	t.Logf("%s", ts.URL)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/autotest", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	if got := rec.Body.String(); got != "OK" {
		t.Fatalf("body=%q", got)
	}
}

func TestVersion(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/version", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v, body=%q", err, rec.Body.String())
	}
	if got["version"] != "wgctrl-go" {
		t.Fatalf("version=%q", got["version"])
	}
}

func TestVersionIncludesBuildInfo(t *testing.T) {
	srv := NewRestServer(
		&fakeClient{},
		t.TempDir(),
		Version("wgctrl-go wgd v4.1.0"),
		BuildInfo("2026-08-26(21:00:00)", "linux-amd64"),
		APIKey(defaultTestAPIKey), // 现在鉴权强制要求 Server 有 key，否则 /version 会 500
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	req.Header.Set("X-API-Key", defaultTestAPIKey)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v, body=%q", err, rec.Body.String())
	}
	if got["version"] != "wgctrl-go wgd v4.1.0" {
		t.Fatalf("version=%q", got["version"])
	}
	if got["build_time"] != "2026-08-26(21:00:00)" {
		t.Fatalf("build_time=%q", got["build_time"])
	}
	if got["platform"] != "linux-amd64" {
		t.Fatalf("platform=%q", got["platform"])
	}
}

func TestInterfaces(t *testing.T) {
	c := &fakeClient{devices: []*wgtypes.Device{{Name: "wg0"}, {Name: "wg1"}}}
	ts, _ := newTestServer(t, c)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/interfaces", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v, body=%q", err, rec.Body.String())
	}
	t.Logf("接口列表(原始body): %s", rec.Body.String())
	t.Logf("接口列表(已解析): %v", got)
	t.Logf("接口总数: %d", len(got))
	for i, name := range got {
		t.Logf("  [%d] %s", i, name)
	}
	if len(got) != 2 || got[0] != "wg0" || got[1] != "wg1" {
		t.Fatalf("interfaces=%v", got)
	}
}

func TestListDevices(t *testing.T) {
	c := &fakeClient{devices: []*wgtypes.Device{sampleDevice()}}
	ts, _ := newTestServer(t, c)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got []Device
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v, body=%q", err, rec.Body.String())
	}
	if len(got) != 1 || got[0].Name != "wg0" || got[0].ListenPort != 51820 {
		t.Fatalf("devices=%+v", got)
	}
	if len(got[0].Peers) != 1 || got[0].Peers[0].AllowedIPs[0] != "10.0.0.0/24" {
		t.Fatalf("allowed ips=%v", got[0].Peers[0].AllowedIPs)
	}
}

func TestGetDeviceNotFound(t *testing.T) {
	c := &fakeClient{err: os.ErrNotExist}
	ts, _ := newTestServer(t, c)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/devices/wg0", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestHideKeys(t *testing.T) {
	c := &fakeClient{device: sampleDevice()}
	srv := NewRestServer(c, t.TempDir(), HideKeys())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/devices/wg0", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got Device
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v, body=%q", err, rec.Body.String())
	}
	if got.PrivateKey != "" {
		t.Fatalf("private key 未被隐藏: %q", got.PrivateKey)
	}
}

func TestPostDeviceConfig(t *testing.T) {
	name := "node-a"
	key := wgtest.MustPrivateKey().PublicKey()
	c := &fakeClient{device: &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: key}}}}
	ts, metadataPath := newTestServer(t, c)
	body := `{"private_key":null,"listen_port":51820,"peers":[{"name":"node-a","public_key":"` + key.String() + `","allowed_ips":["10.0.0.0/24"],"persistent_keepalive_seconds":25}]}`
	req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/v1/devices/wg0", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if c.configured.ListenPort == nil || *c.configured.ListenPort != 51820 {
		t.Fatalf("listen port=%v", c.configured.ListenPort)
	}
	if len(c.configured.Peers) != 1 || c.configured.Peers[0].PublicKey != key {
		t.Fatalf("peers=%+v", c.configured.Peers)
	}
	if c.configured.Peers[0].PersistentKeepaliveInterval == nil || *c.configured.Peers[0].PersistentKeepaliveInterval != 25*time.Second {
		t.Fatalf("keepalive=%v", c.configured.Peers[0].PersistentKeepaliveInterval)
	}
	if c.configured.Peers[0].Name != nil {
		t.Fatalf("name passed to backend: %#v", c.configured.Peers[0].Name)
	}
	names, err := wgmeta.New(metadataPath).Names("wg0")
	if err != nil || names[key] != name {
		t.Fatalf("names=%v err=%v", names, err)
	}
}

func TestPostDeviceConfigInvalidKey(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	body := `{"peers":[{"public_key":"not-base64"}]}`
	req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/v1/devices/wg0", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestShowConf(t *testing.T) {
	c := &fakeClient{device: sampleDevice()}
	ts, _ := newTestServer(t, c)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/devices/wg0/conf", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "[Interface]") || !strings.Contains(body, "ListenPort = 51820") {
		t.Fatalf("conf=%q", body)
	}
}

func TestSetConf(t *testing.T) {
	c := &fakeClient{device: sampleDevice()}
	ts, _ := newTestServer(t, c)
	conf := "[Interface]\nListenPort = 51821\n"
	req := httptest.NewRequest(http.MethodPut, ts.URL+"/api/v1/devices/wg0/conf", bytes.NewBufferString(conf))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if !c.configured.ReplacePeers {
		t.Fatal("setconf 应替换对等节点")
	}
	if c.configured.ListenPort == nil || *c.configured.ListenPort != 51821 {
		t.Fatalf("listen port=%v", c.configured.ListenPort)
	}
}

func TestAddConf(t *testing.T) {
	c := &fakeClient{device: sampleDevice()}
	ts, _ := newTestServer(t, c)
	conf := "[Interface]\nListenPort = 51821\n"
	req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/v1/devices/wg0/conf", bytes.NewBufferString(conf))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	if c.configured.ReplacePeers {
		t.Fatal("addconf 不应替换对等节点")
	}
}

func TestSyncConf(t *testing.T) {
	kept := wgtest.MustPrivateKey().PublicKey()
	removed := wgtest.MustPrivateKey().PublicKey()
	c := &fakeClient{
		device: &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: kept}, {PublicKey: removed}}},
	}
	ts, _ := newTestServer(t, c)
	conf := "[Peer]\nPublicKey = " + kept.String() + "\n"
	req := httptest.NewRequest(http.MethodPut, ts.URL+"/api/v1/devices/wg0/conf?mode=sync", bytes.NewBufferString(conf))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	peers := c.configured.Peers
	if len(peers) != 2 || !peers[1].Remove || peers[1].PublicKey != removed {
		t.Fatalf("sync peers=%+v", peers)
	}
}

func TestConfInvalidMode(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{device: sampleDevice()})
	req := httptest.NewRequest(http.MethodPut, ts.URL+"/api/v1/devices/wg0/conf?mode=bogus", bytes.NewBufferString("[Interface]\n"))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGenKey(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/v1/genkey", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if _, err := wgtypes.ParseKey(got["key"]); err != nil {
		t.Fatalf("genkey 结果无效: %v", err)
	}
}

func TestGenPsk(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/v1/genpsk", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if _, err := wgtypes.ParseKey(got["key"]); err != nil {
		t.Fatalf("genpsk 结果无效: %v", err)
	}
}

func TestPubkey(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	priv := wgtest.MustPrivateKey()
	body := `{"key":"` + priv.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, ts.URL+"/api/v1/pubkey", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if got["key"] != priv.PublicKey().String() {
		t.Fatalf("pubkey=%q", got["key"])
	}
}

func TestUnknownPath(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{})
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/devices/wg0/unknown", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestMethodNotAllowed(t *testing.T) {
	ts, _ := newTestServer(t, &fakeClient{device: sampleDevice()})
	req := httptest.NewRequest(http.MethodDelete, ts.URL+"/api/v1/devices/wg0", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func newTestServerWithOpts(t *testing.T, c Client, opts ...Option) (*httptest.Server, string) {
	t.Helper()
	// 鉴权相关用例：完全控制自己的 header，不自动注入任何 X-API-Key。
	srv := NewRestServer(c, t.TempDir(), opts...)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv.metadata
}

func TestListDevicesError(t *testing.T) {
	c := &fakeClient{err: errors.New("boom")}
	ts, _ := newTestServer(t, c)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/devices", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddlewareNoKey(t *testing.T) {
	ts, _ := newTestServerWithOpts(t, &fakeClient{devices: []*wgtypes.Device{sampleDevice()}}, APIKey("s3cr3t"))
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/interfaces", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d want=%d body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if www := rec.Header().Get("WWW-Authenticate"); www != "X-API-Key" {
		t.Fatalf("WWW-Authenticate=%q", www)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if !strings.Contains(body["error"], "missing") && !strings.Contains(body["error"], "X-API-Key") {
		t.Fatalf("error=%q", body["error"])
	}
}

func TestAuthMiddlewareWrongKey(t *testing.T) {
	ts, _ := newTestServerWithOpts(t, &fakeClient{devices: []*wgtypes.Device{sampleDevice()}}, APIKey("s3cr3t"))
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/interfaces", nil)
	req.Header.Set("X-API-Key", "wr0ng")
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d want=%d body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAuthMiddlewareCorrectKey(t *testing.T) {
	ts, _ := newTestServerWithOpts(t, &fakeClient{devices: []*wgtypes.Device{sampleDevice()}}, APIKey("s3cr3t"))
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/interfaces", nil)
	req.Header.Set("X-API-Key", "s3cr3t")
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
	var names []string
	if err := json.Unmarshal(rec.Body.Bytes(), &names); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(names) != 1 || names[0] != "wg0" {
		t.Fatalf("names=%v", names)
	}
}

func TestAuthWhitelistHealthAndAutotest(t *testing.T) {
	ts, _ := newTestServerWithOpts(t, &fakeClient{}, APIKey("s3cr3t"))
	for _, path := range []string{"/autotest", "/api/v1/health"} {
		req := httptest.NewRequest(http.MethodGet, ts.URL+path, nil)
		rec := httptest.NewRecorder()
		ts.Config.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s code=%d want=200 (whitelist) body=%q", path, rec.Code, rec.Body.String())
		}
	}
	// 白名单不含 /api/v1/version，应返回 401
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/version", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("version code=%d want=401 body=%q", rec.Code, rec.Body.String())
	}
}

func TestAuthEmptyApiKeyRejectsAll(t *testing.T) {
	// 故意不传 APIKey：Server 层面对未注入 key 的情况必须返回 500 + 错误 JSON，
	// 绝对不能"偷偷放行"。
	ts, _ := newTestServerWithOpts(t, &fakeClient{devices: []*wgtypes.Device{sampleDevice()}})
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/interfaces", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d want=500 body=%q", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(body["error"], "misconfigured") && !strings.Contains(body["error"], "api key not set") {
		t.Fatalf("error=%q", body["error"])
	}
}
