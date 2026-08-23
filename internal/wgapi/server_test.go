package wgapi

import (
	"net"
	"net/http/httptest"
	"path/filepath"
	"testing"

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
	endpoint := &net.UDPAddr{IP: net.ParseIP("203.0.113.1"), Port: 51820}
	_ = endpoint
	key := wgtypes.MustPrivateKey
	return nil
}
