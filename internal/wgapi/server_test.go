package wgapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func newTestServer(t *testing.T, c Client) (*httptest.Server, string) {
	t.Helper()
	srv := New(c, filepath.Join(t.TempDir(), "peer-names.json"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv.metadata
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

func TestInterfaces(t *testing.T) {
	c := &fakeClient{devices: []*wgtypes.Device{{Name: "wg0"}, {Name: "wg1"}}}
	ts, _ := newTestServer(t, c)
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/v1/interfaces", nil)
	rec := httptest.NewRecorder()
	ts.Config.Handler.ServeHTTP(rec, req)
	var got []string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v, body=%q", err, rec.Body.String())
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
	srv := New(c, filepath.Join(t.TempDir(), "peer-names.json"), HideKeys())
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
