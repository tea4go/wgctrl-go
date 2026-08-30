package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestSyncGiteeCommandCreatesDefaultFile(t *testing.T) {
	oldToken, oldGistID := syncGiteeToken, syncGiteeGistID
	syncGiteeToken, syncGiteeGistID = "secret", "gist-1"
	t.Cleanup(func() { syncGiteeToken, syncGiteeGistID = oldToken, oldGistID })
	oldClient, oldBaseURL := newSyncGiteeClient, syncGiteeBaseURL
	oldAddresses, oldHostname := syncGiteeInterfaceAddresses, syncGiteeHostname
	oldPublicIP := syncGiteePublicIP
	t.Cleanup(func() {
		newSyncGiteeClient, syncGiteeBaseURL = oldClient, oldBaseURL
		syncGiteeInterfaceAddresses, syncGiteeHostname = oldAddresses, oldHostname
		syncGiteePublicIP = oldPublicIP
	})
	privateKey := testSyncKey(7)
	peerKey := testSyncKey(8)
	peerName := "peer-a"
	newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
		return &syncGiteeTestDeviceClient{device: &wgtypes.Device{
			Name: "wgtun5", PrivateKey: privateKey, HasPrivateKey: true,
			ListenPort: 51820,
			Peers:      []wgtypes.Peer{{Name: &peerName, PublicKey: peerKey, AllowedIPs: []net.IPNet{mustSyncCIDR(t, "192.168.190.8/32")}}},
		}}, nil
	}
	syncGiteeInterfaceAddresses = func(string) ([]net.IPNet, error) {
		return []net.IPNet{mustSyncCIDR(t, "192.168.190.1/24")}, nil
	}
	syncGiteeHostname = func() (string, error) { return "host-a", nil }
	syncGiteePublicIP = func() net.IP { return nil }

	var patched map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gists/gist-1" || r.URL.Query().Get("access_token") != "secret" {
			t.Fatalf("unexpected request: %s %s access_token=%q", r.Method, r.URL.Path, r.URL.Query().Get("access_token"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, `{"files":{}}`)
		case http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
				t.Fatal(err)
			}
			io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()
	syncGiteeBaseURL = server.URL

	var out, errOut bytes.Buffer
	code := syncgitee([]string{"wgtun5"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", code, errOut.String())
	}
	files := patched["files"].(map[string]interface{})
	content := files["default"].(map[string]interface{})["content"].(string)
	for _, want := range []string{
		"Name = host-a", "PublicKey = " + privateKey.PublicKey().String(),
		"PrivateKey = " + privateKey.String(),
		"AllowedIPs = 192.168.190.1/32", "Endpoint = 192.168.190.1:51820",
		"Name = peer-a", "PublicKey = " + peerKey.String(),
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q: %q", want, content)
		}
	}
}

func TestSyncGiteeCommandCreatesGistWhenIDIsOmitted(t *testing.T) {
	oldToken, oldGistID := syncGiteeToken, syncGiteeGistID
	syncGiteeToken, syncGiteeGistID = "secret", ""
	t.Cleanup(func() { syncGiteeToken, syncGiteeGistID = oldToken, oldGistID })
	oldClient, oldBaseURL := newSyncGiteeClient, syncGiteeBaseURL
	oldAddresses, oldHostname := syncGiteeInterfaceAddresses, syncGiteeHostname
	oldPublicIP := syncGiteePublicIP
	t.Cleanup(func() {
		newSyncGiteeClient, syncGiteeBaseURL = oldClient, oldBaseURL
		syncGiteeInterfaceAddresses, syncGiteeHostname = oldAddresses, oldHostname
		syncGiteePublicIP = oldPublicIP
	})
	privateKey := testSyncKey(7)
	newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
		return &syncGiteeTestDeviceClient{device: &wgtypes.Device{
			Name: "wg0", PrivateKey: privateKey, HasPrivateKey: true,
		}}, nil
	}
	syncGiteeInterfaceAddresses = func(string) ([]net.IPNet, error) { return nil, nil }
	syncGiteeHostname = func() (string, error) { return "host-a", nil }
	syncGiteePublicIP = func() net.IP { return nil }

	var created map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/gists" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("access_token") != "secret" {
			t.Fatalf("unexpected access token: %q", r.URL.Query().Get("access_token"))
		}
		if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, `{"id":"new-gist-id"}`)
	}))
	defer server.Close()
	syncGiteeBaseURL = server.URL

	var out, errOut bytes.Buffer
	if code := syncgitee([]string{"wg0"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", code, errOut.String())
	}
	if out.String() != "已创建 Gitee 代码片段 new-gist-id，并同步 1 个节点到文件 default\n" {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
	if created["description"] != "WireGuard 节点配置" || created["public"] != false {
		t.Fatalf("unexpected gist settings: %#v", created)
	}
	files := created["files"].(map[string]interface{})
	content := files["default"].(map[string]interface{})["content"].(string)
	for _, want := range []string{
		"PublicKey = " + privateKey.PublicKey().String(),
		"PrivateKey = " + privateKey.String(),
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("unexpected content missing %q: %q", want, content)
		}
	}
}

func TestSyncGiteeCommandUsage(t *testing.T) {
	oldToken := syncGiteeToken
	syncGiteeToken = ""
	t.Cleanup(func() { syncGiteeToken = oldToken })
	var out, errOut bytes.Buffer
	if code := syncgitee([]string{"wg0"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if want := "错误: 必须通过 --gitee_token 或 gitee_token 环境变量提供 Gitee token\n"; errOut.String() != want {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestSyncGiteeCommandMergesCustomFile(t *testing.T) {
	oldToken, oldGistID := syncGiteeToken, syncGiteeGistID
	syncGiteeToken, syncGiteeGistID = "secret", "gist-1"
	t.Cleanup(func() { syncGiteeToken, syncGiteeGistID = oldToken, oldGistID })
	oldClient, oldBaseURL := newSyncGiteeClient, syncGiteeBaseURL
	oldAddresses, oldHostname := syncGiteeInterfaceAddresses, syncGiteeHostname
	oldPublicIP := syncGiteePublicIP
	t.Cleanup(func() {
		newSyncGiteeClient, syncGiteeBaseURL = oldClient, oldBaseURL
		syncGiteeInterfaceAddresses, syncGiteeHostname = oldAddresses, oldHostname
		syncGiteePublicIP = oldPublicIP
	})
	privateKey := testSyncKey(7)
	updatedKey := testSyncKey(8)
	retainedKey := testSyncKey(9)
	updatedName := "updated"
	newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
		return &syncGiteeTestDeviceClient{device: &wgtypes.Device{
			Name: "wg0", PrivateKey: privateKey, HasPrivateKey: true,
			Peers: []wgtypes.Peer{{Name: &updatedName, PublicKey: updatedKey}},
		}}, nil
	}
	syncGiteeInterfaceAddresses = func(string) ([]net.IPNet, error) { return nil, nil }
	syncGiteeHostname = func() (string, error) { return "host-a", nil }
	syncGiteePublicIP = func() net.IP { return net.ParseIP("203.0.113.10") }

	remote := encodeSyncGiteeNodes([]syncGiteeNode{
		{Name: "old", PublicKey: updatedKey},
		{Name: "retained", PublicKey: retainedKey},
	})
	var patched map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"files": map[string]interface{}{"nodes.conf": map[string]string{"content": remote}},
			})
		case http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()
	syncGiteeBaseURL = server.URL

	var out, errOut bytes.Buffer
	if code := syncgitee([]string{"wg0", "nodes.conf"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("unexpected exit code: %d, stderr=%q", code, errOut.String())
	}
	files := patched["files"].(map[string]interface{})
	content := files["nodes.conf"].(map[string]interface{})["content"].(string)
	nodes, err := parseSyncGiteeNodes(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("unexpected node count: %d", len(nodes))
	}
	if nodes[0].Name != "updated" || nodes[1].Name != "retained" || nodes[2].Name != "host-a" {
		t.Fatalf("unexpected merged nodes: %#v", nodes)
	}
}

func TestSyncGiteeCommandRejectsInvalidRemoteWithoutPatch(t *testing.T) {
	oldToken, oldGistID := syncGiteeToken, syncGiteeGistID
	syncGiteeToken, syncGiteeGistID = "super-secret", "gist-1"
	t.Cleanup(func() { syncGiteeToken, syncGiteeGistID = oldToken, oldGistID })
	oldClient, oldBaseURL := newSyncGiteeClient, syncGiteeBaseURL
	oldAddresses, oldHostname := syncGiteeInterfaceAddresses, syncGiteeHostname
	oldPublicIP := syncGiteePublicIP
	t.Cleanup(func() {
		newSyncGiteeClient, syncGiteeBaseURL = oldClient, oldBaseURL
		syncGiteeInterfaceAddresses, syncGiteeHostname = oldAddresses, oldHostname
		syncGiteePublicIP = oldPublicIP
	})
	newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
		return &syncGiteeTestDeviceClient{device: &wgtypes.Device{
			Name: "wg0", PrivateKey: testSyncKey(7), HasPrivateKey: true,
		}}, nil
	}
	syncGiteeInterfaceAddresses = func(string) ([]net.IPNet, error) { return nil, nil }
	syncGiteeHostname = func() (string, error) { return "host-a", nil }
	syncGiteePublicIP = func() net.IP { return nil }

	patched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
		}
		io.WriteString(w, `{"files":{"default":{"content":"invalid"}}}`)
	}))
	defer server.Close()
	syncGiteeBaseURL = server.URL

	var out, errOut bytes.Buffer
	if code := syncgitee([]string{"wg0"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if patched {
		t.Fatal("invalid remote content must not be patched")
	}
	if strings.Contains(errOut.String(), "super-secret") {
		t.Fatalf("stderr leaked token: %q", errOut.String())
	}
}

func TestSyncGiteeNodesAddressAndNameFallbacks(t *testing.T) {
	firstKey := testSyncKey(1)
	secondKey := testSyncKey(2)
	device := &wgtypes.Device{
		Name: "wg0", PrivateKey: testSyncKey(7), HasPrivateKey: true, ListenPort: 51820,
		Peers: []wgtypes.Peer{
			{PublicKey: firstKey, AllowedIPs: []net.IPNet{mustSyncCIDR(t, "192.0.2.8/32")}},
			{PublicKey: secondKey},
		},
	}
	addresses := []net.IPNet{mustSyncCIDR(t, "2001:db8::1/64")}
	nodes, err := syncGiteeNodes(device, addresses, "host-a", net.ParseIP("203.0.113.10"))
	if err != nil {
		t.Fatal(err)
	}
	if got := nodes[0].Endpoint.String(); got != "203.0.113.10:51820" {
		t.Fatalf("unexpected public endpoint: %q", got)
	}
	if nodes[0].PrivateKey == nil || *nodes[0].PrivateKey != device.PrivateKey {
		t.Fatalf("unexpected local private key: %#v", nodes[0].PrivateKey)
	}
	if got := nodes[0].AllowedIPs[0].String(); got != "2001:db8::1/128" {
		t.Fatalf("unexpected local allowed IP: %q", got)
	}
	if nodes[1].Name != "192.0.2.8/32" {
		t.Fatalf("unexpected allowed IP fallback: %q", nodes[1].Name)
	}
	if nodes[2].Name != secondKey.String()[:8] {
		t.Fatalf("unexpected public key fallback: %q", nodes[2].Name)
	}

	nodes, err = syncGiteeNodes(device, addresses, "host-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := nodes[0].Endpoint.String(); got != "[2001:db8::1]:51820" {
		t.Fatalf("unexpected IPv6 fallback endpoint: %q", got)
	}
}

func TestSyncGiteePublicIPUsesFirstValidResponse(t *testing.T) {
	oldURLs, oldClient := syncGiteePublicIPURLs, syncGiteePublicIPHTTPClient
	t.Cleanup(func() {
		syncGiteePublicIPURLs, syncGiteePublicIPHTTPClient = oldURLs, oldClient
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invalid":
			io.WriteString(w, "service unavailable")
		case "/ip":
			io.WriteString(w, "当前 IP：203.0.113.10\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	syncGiteePublicIPURLs = []string{server.URL + "/invalid", server.URL + "/ip"}
	syncGiteePublicIPHTTPClient = server.Client()

	if got := lookupSyncGiteePublicIP(); !got.Equal(net.ParseIP("203.0.113.10")) {
		t.Fatalf("unexpected public IP: %v", got)
	}
}

func TestSyncGiteePublicIPReturnsNilWhenServicesFail(t *testing.T) {
	oldURLs, oldClient := syncGiteePublicIPURLs, syncGiteePublicIPHTTPClient
	t.Cleanup(func() {
		syncGiteePublicIPURLs, syncGiteePublicIPHTTPClient = oldURLs, oldClient
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusBadGateway)
	}))
	defer server.Close()

	syncGiteePublicIPURLs = []string{server.URL}
	syncGiteePublicIPHTTPClient = server.Client()

	if got := lookupSyncGiteePublicIP(); got != nil {
		t.Fatalf("unexpected public IP: %v", got)
	}
}

func TestGiteeRequestDoesNotLeakTokenOnTransportError(t *testing.T) {
	oldHTTPClient := syncGiteeHTTPClient
	t.Cleanup(func() { syncGiteeHTTPClient = oldHTTPClient })
	syncGiteeHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New(req.URL.String())
	})}

	err := giteeRequest(http.MethodGet, "gist-1", "super-secret", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked token: %q", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type syncGiteeTestDeviceClient struct {
	device *wgtypes.Device
	err    error
}

func (c *syncGiteeTestDeviceClient) Device(string) (*wgtypes.Device, error) {
	return c.device, c.err
}
func (*syncGiteeTestDeviceClient) Close() error { return nil }

func TestSyncGiteeCommandReportsMissingLocalInterface(t *testing.T) {
	oldToken, oldClient := syncGiteeToken, newSyncGiteeClient
	oldAddresses, oldHostname := syncGiteeInterfaceAddresses, syncGiteeHostname
	oldPublicIP, oldHTTPClient := syncGiteePublicIP, syncGiteeHTTPClient
	t.Cleanup(func() {
		syncGiteeToken, newSyncGiteeClient = oldToken, oldClient
		syncGiteeInterfaceAddresses, syncGiteeHostname = oldAddresses, oldHostname
		syncGiteePublicIP, syncGiteeHTTPClient = oldPublicIP, oldHTTPClient
	})
	syncGiteeToken = "secret"
	calls := 0
	newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
		return &syncGiteeTestDeviceClient{err: fmt.Errorf("device lookup: %w", syscall.ENOENT)}, nil
	}
	syncGiteeInterfaceAddresses = func(string) ([]net.IPNet, error) {
		calls++
		return nil, nil
	}
	syncGiteeHostname = func() (string, error) {
		calls++
		return "host-a", nil
	}
	syncGiteePublicIP = func() net.IP {
		calls++
		return net.ParseIP("203.0.113.10")
	}
	syncGiteeHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected Gitee request")
	})}
	var out, errOut bytes.Buffer
	if code := syncgitee([]string{"wgtun"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	message := errOut.String()
	for _, want := range []string{"本地 WireGuard 接口 \"wgtun\" 不存在", "syncgitee 只会上传已有本地接口数据到 Gitee"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error missing %q: %q", want, message)
		}
	}
	if strings.Contains(message, "file does not exist") || strings.Contains(message, "ENOENT") {
		t.Fatalf("error leaked low-level message: %q", message)
	}
	if calls != 0 {
		t.Fatalf("public IP lookup called %d times", calls)
	}
}

func TestSyncGiteeCommandPreservesNonMissingDeviceError(t *testing.T) {
	oldToken, oldClient := syncGiteeToken, newSyncGiteeClient
	t.Cleanup(func() { syncGiteeToken, newSyncGiteeClient = oldToken, oldClient })
	syncGiteeToken = "secret"
	deviceErr := fmt.Errorf("device lookup: %w", syscall.EPERM)
	newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
		return &syncGiteeTestDeviceClient{err: deviceErr}, nil
	}
	var out, errOut bytes.Buffer
	if code := syncgitee([]string{"wgtun"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if message := errOut.String(); !strings.Contains(message, "无法读取接口 wgtun") || !strings.Contains(message, "operation not permitted") {
		t.Fatalf("unexpected error: %q", message)
	}
	if err := syncGiteeDeviceError("wgtun", deviceErr); !errors.Is(err, syscall.EPERM) {
		t.Fatal("device error chain is not identifiable")
	}
}

func TestSyncGiteeCommandClassifiesWrappedNotExistErrors(t *testing.T) {
	for _, deviceErr := range []error{
		os.ErrNotExist,
		syscall.ENOENT,
		&os.PathError{Op: "open", Path: "wg", Err: syscall.ENOENT},
		fmt.Errorf("device lookup: %w", syscall.ENOENT),
	} {
		t.Run(deviceErr.Error(), func(t *testing.T) {
			oldToken, oldClient := syncGiteeToken, newSyncGiteeClient
			t.Cleanup(func() { syncGiteeToken, newSyncGiteeClient = oldToken, oldClient })
			syncGiteeToken = "secret"
			newSyncGiteeClient = func() (syncGiteeDeviceClient, error) {
				return &syncGiteeTestDeviceClient{err: deviceErr}, nil
			}
			var out, errOut bytes.Buffer
			syncgitee([]string{"wgtun"}, strings.NewReader(""), &out, &errOut)
			message := errOut.String()
			if strings.Contains(message, "file does not exist") || strings.Contains(message, "ENOENT") {
				t.Fatalf("unexpected low-level error: %q", message)
			}
			if !strings.Contains(message, "本地 WireGuard 接口 \"wgtun\" 不存在") {
				t.Fatalf("unexpected error: %q", message)
			}
		})
	}
}

func TestParseSyncGiteeNodesAndMerge(t *testing.T) {
	remoteKey := testSyncKey(1)
	localKey := testSyncKey(2)
	remote := "[Peer]\n" +
		"Name = old\n" +
		"PublicKey = " + remoteKey.String() + "\n" +
		"PresharedKey = " + wgtypes.Key{}.String() + "\n" +
		"AllowedIPs = 192.168.190.10/32\n" +
		"Endpoint = 1.1.1.1:51820\n" +
		"PersistentKeepalive = 25\n"

	nodes, err := parseSyncGiteeNodes(remote)
	if err != nil {
		t.Fatal(err)
	}
	local := []syncGiteeNode{
		{Name: "new", PublicKey: remoteKey, PrivateKey: keyRef(testSyncKey(3)), PresharedKey: wgtypes.Key{}, AllowedIPs: []net.IPNet{mustSyncCIDR(t, "192.168.190.11/32")}, PersistentKeepalive: 25 * time.Second},
		{Name: "added", PublicKey: localKey, PresharedKey: wgtypes.Key{}, AllowedIPs: []net.IPNet{mustSyncCIDR(t, "192.168.190.12/32")}},
	}
	merged := mergeSyncGiteeNodes(nodes, local)
	if len(merged) != 2 {
		t.Fatalf("unexpected node count: %d", len(merged))
	}
	if merged[0].Name != "new" || merged[1].Name != "added" {
		t.Fatalf("unexpected merge result: %#v", merged)
	}
	encoded := encodeSyncGiteeNodes(merged)
	for _, want := range []string{"Name = new", "Name = added", "PublicKey = " + localKey.String(), "PrivateKey = " + testSyncKey(3).String()} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("encoded content missing %q: %q", want, encoded)
		}
	}
	if strings.Contains(encoded, "PresharedKey") {
		t.Fatalf("encoded content leaked PresharedKey: %q", encoded)
	}
}

func TestParseSyncGiteeNodesRejectsInvalidContent(t *testing.T) {
	for _, content := range []string{
		"Name = outside\n",
		"[Peer]\nName = missing-key\n",
		"[Peer]\nPublicKey = invalid\n",
		"[Peer]\nPublicKey = " + testSyncKey(1).PublicKey().String() + "\nPrivateKey = invalid\n",
		"[Peer]\nPublicKey = " + testSyncKey(1).PublicKey().String() + "\nPrivateKey = " + testSyncKey(2).String() + "\n",
		"[Peer]\nPublicKey = " + testSyncKey(1).String() + "\nUnknown = value\n",
	} {
		if _, err := parseSyncGiteeNodes(content); err == nil {
			t.Fatalf("expected error for %q", content)
		}
	}
}

func testSyncKey(value byte) wgtypes.Key {
	var key wgtypes.Key
	for i := range key {
		key[i] = value
	}
	return key
}

func mustSyncCIDR(t *testing.T, value string) net.IPNet {
	t.Helper()
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		t.Fatal(err)
	}
	network.IP = ip
	return *network
}

func keyRef(key wgtypes.Key) *wgtypes.Key {
	return &key
}
