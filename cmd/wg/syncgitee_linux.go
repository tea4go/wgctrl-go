//go:build linux

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconf"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type syncGiteeNode struct {
	Name                string
	PublicKey           wgtypes.Key
	PrivateKey          *wgtypes.Key
	PresharedKey        wgtypes.Key
	AllowedIPs          []net.IPNet
	Endpoint            *net.UDPAddr
	PersistentKeepalive time.Duration
}

type syncGiteeDeviceClient interface {
	Device(string) (*wgtypes.Device, error)
	Close() error
}

var newSyncGiteeClient = func() (syncGiteeDeviceClient, error) { return wgctrl.New() }
var syncGiteeBaseURL = "https://gitee.com/api/v5"
var syncGiteeHTTPClient = http.DefaultClient
var syncGiteeHostname = os.Hostname
var syncGiteePublicIP = lookupSyncGiteePublicIP
var syncGiteePublicIPURLs = []string{
	"https://ip.cn",
	"https://cip.cc",
	"https://ipinfo.io/ip",
	"https://ifconfig.me/ip",
	"https://ipx.sh/ip",
	"https://api.ip.sb/ip",
	"https://ident.me",
}
var syncGiteePublicIPHTTPClient = &http.Client{Timeout: 5 * time.Second}
var syncGiteeInterfaceAddresses = interfaceAddresses

func lookupSyncGiteePublicIP() net.IP {
	for _, url := range syncGiteePublicIPURLs {
		resp, err := syncGiteePublicIPHTTPClient.Get(url)
		if err != nil {
			continue
		}
		var content strings.Builder
		_, _ = io.Copy(&content, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}
		for _, field := range strings.Fields(content.String()) {
			field = strings.Trim(field, " \t\r\n:：,，;；()（）[]{}<>")
			if i := strings.LastIndex(field, "："); i >= 0 {
				field = field[i+len("："):]
			}
			if ip := net.ParseIP(field); ip != nil {
				return ip
			}
		}
	}
	return nil
}

type syncGiteeDeviceReadError struct {
	name string
	err  error
}

func (e *syncGiteeDeviceReadError) Error() string {
	if errors.Is(e.err, os.ErrNotExist) {
		return fmt.Sprintf("本地 WireGuard 接口 %q 不存在；syncgitee 只会上传已有本地接口数据到 Gitee", e.name)
	}
	return fmt.Sprintf("无法读取接口 %s: %v", e.name, e.err)
}

func (e *syncGiteeDeviceReadError) Unwrap() error { return e.err }

func syncGiteeDeviceError(name string, err error) error {
	return &syncGiteeDeviceReadError{name: name, err: err}
}

func syncgitee(args []string, _ io.Reader, out, errOut io.Writer) int {
	const usage = "用法: wg syncgitee <接口> [文件名]"
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(errOut, usage)
		return 1
	}
	if strings.TrimSpace(syncGiteeToken) == "" {
		fmt.Fprintln(errOut, "错误: 必须通过 --gitee_token 或 gitee_token 环境变量提供 Gitee token")
		return 1
	}
	filename := "default"
	if len(args) == 2 {
		filename = args[1]
	}
	client, err := newSyncGiteeClient()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer client.Close()
	device, err := client.Device(args[0])
	if err != nil {
		fmt.Fprintln(errOut, syncGiteeDeviceError(args[0], err))
		return 1
	}
	if err := wgconf.AttachNames(device, wgmeta.DefaultPath()); err != nil {
		fmt.Fprintf(errOut, "警告: 无法读取节点名称: %v\n", err)
	}
	addresses, err := syncGiteeInterfaceAddresses(args[0])
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	hostname, err := syncGiteeHostname()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	local, err := syncGiteeNodes(device, addresses, hostname, syncGiteePublicIP())
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if strings.TrimSpace(syncGiteeGistID) == "" {
		gistID, err := createGiteeGist(syncGiteeToken, filename, encodeSyncGiteeNodes(local))
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		fmt.Fprintf(out, "已创建 Gitee 代码片段 %s，并同步 %d 个节点到文件 %s\n", gistID, len(local), filename)
		return 0
	}
	remoteContent, err := readGiteeGist(syncGiteeToken, syncGiteeGistID, filename)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	remote, err := parseSyncGiteeNodes(remoteContent)
	if err != nil {
		fmt.Fprintf(errOut, "Gitee 节点内容无效: %v\n", err)
		return 1
	}
	content := encodeSyncGiteeNodes(mergeSyncGiteeNodes(remote, local))
	if err := updateGiteeGist(syncGiteeToken, syncGiteeGistID, filename, content); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	fmt.Fprintf(out, "已同步 %d 个节点到 Gitee 文件 %s\n", len(local), filename)
	return 0
}

func syncGiteeNodes(device *wgtypes.Device, addresses []net.IPNet, hostname string, publicIP net.IP) ([]syncGiteeNode, error) {
	if !device.HasPrivateKey {
		return nil, fmt.Errorf("接口 %s 未配置私钥", device.Name)
	}
	nodes := make([]syncGiteeNode, 0, len(device.Peers)+1)
	allowedIPs := hostRoutes(addresses)
	endpointIP := publicIP
	if endpointIP == nil {
		endpointIP = preferredAddress(addresses)
	}
	local := syncGiteeNode{
		Name: hostname, PublicKey: device.PrivateKey.PublicKey(), PrivateKey: &device.PrivateKey, PresharedKey: wgtypes.Key{},
		AllowedIPs: allowedIPs, PersistentKeepalive: 25 * time.Second,
	}
	if endpointIP != nil && device.ListenPort != 0 {
		local.Endpoint = &net.UDPAddr{IP: endpointIP, Port: device.ListenPort}
	}
	nodes = append(nodes, local)
	for _, peer := range device.Peers {
		name := ""
		if peer.Name != nil {
			name = *peer.Name
		}
		if name == "" && len(peer.AllowedIPs) > 0 {
			name = peer.AllowedIPs[0].String()
		}
		if name == "" {
			name = peer.PublicKey.String()[:8]
		}
		nodes = append(nodes, syncGiteeNode{
			Name: name, PublicKey: peer.PublicKey, PresharedKey: peer.PresharedKey,
			AllowedIPs: peer.AllowedIPs, Endpoint: peer.Endpoint,
			PersistentKeepalive: peer.PersistentKeepaliveInterval,
		})
	}
	return nodes, nil
}

func interfaceAddresses(name string) ([]net.IPNet, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("无法读取接口 %s: %w", name, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("无法读取接口 %s 的地址: %w", name, err)
	}
	var result []net.IPNet
	for _, addr := range addrs {
		ip, network, err := net.ParseCIDR(addr.String())
		if err != nil || ip.IsLoopback() {
			continue
		}
		network.IP = ip
		result = append(result, *network)
	}
	return result, nil
}

func preferredAddress(addresses []net.IPNet) net.IP {
	for _, address := range addresses {
		if address.IP.To4() != nil && !address.IP.IsLoopback() {
			return address.IP
		}
	}
	for _, address := range addresses {
		if address.IP.To16() != nil && !address.IP.IsLoopback() {
			return address.IP
		}
	}
	return nil
}

func hostRoutes(addresses []net.IPNet) []net.IPNet {
	routes := make([]net.IPNet, 0, len(addresses))
	for _, address := range addresses {
		bits := 128
		if address.IP.To4() != nil {
			bits = 32
		}
		routes = append(routes, net.IPNet{IP: address.IP, Mask: net.CIDRMask(bits, bits)})
	}
	return routes
}

type giteeGist struct {
	Files map[string]struct {
		Content string `json:"content"`
	} `json:"files"`
}

func readGiteeGist(token, gistID, filename string) (string, error) {
	var gist giteeGist
	if err := giteeRequest(http.MethodGet, gistID, token, nil, &gist); err != nil {
		return "", err
	}
	return gist.Files[filename].Content, nil
}

func updateGiteeGist(token, gistID, filename, content string) error {
	body := map[string]interface{}{"files": map[string]interface{}{filename: map[string]string{"content": content}}}
	return giteeRequest(http.MethodPatch, gistID, token, body, nil)
}

func createGiteeGist(token, filename, content string) (string, error) {
	body := map[string]interface{}{
		"description": "WireGuard 节点配置",
		"public":      false,
		"files":       map[string]interface{}{filename: map[string]string{"content": content}},
	}
	var gist struct {
		ID string `json:"id"`
	}
	if err := giteeRequest(http.MethodPost, "", token, body, &gist); err != nil {
		return "", err
	}
	return gist.ID, nil
}

func giteeRequest(method, gistID, token string, body, result interface{}) error {
	var reader io.Reader
	if body != nil {
		var b bytes.Buffer
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			return err
		}
		reader = &b
	}
	url := strings.TrimRight(syncGiteeBaseURL, "/") + "/gists"
	if gistID != "" {
		url += "/" + gistID
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	query := req.URL.Query()
	query.Set("access_token", token)
	req.URL.RawQuery = query.Encode()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := syncGiteeHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("访问 Gitee 失败: %s", strings.ReplaceAll(err.Error(), token, "[REDACTED]"))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Gitee API 返回状态 %s", resp.Status)
	}
	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("解析 Gitee 响应失败: %w", err)
		}
	}
	return nil
}

func parseSyncGiteeNodes(content string) ([]syncGiteeNode, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var nodes []syncGiteeNode
	var node *syncGiteeNode
	var hasPublicKey bool
	s := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	finish := func() error {
		if node == nil {
			return nil
		}
		if !hasPublicKey {
			return fmt.Errorf("节点缺少 PublicKey")
		}
		if node.PrivateKey != nil && node.PrivateKey.PublicKey() != node.PublicKey {
			return fmt.Errorf("节点 PrivateKey 与 PublicKey 不匹配")
		}
		nodes = append(nodes, *node)
		return nil
	}
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[Peer]" {
			if err := finish(); err != nil {
				return nil, fmt.Errorf("第 %d 行前: %w", lineNo, err)
			}
			node = &syncGiteeNode{}
			hasPublicKey = false
			continue
		}
		if node == nil {
			return nil, fmt.Errorf("第 %d 行: 字段不在 [Peer] 区块内", lineNo)
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("第 %d 行: 无效内容", lineNo)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Name":
			node.Name = value
		case "PublicKey":
			v, err := wgtypes.ParseKey(value)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行: PublicKey 无效: %w", lineNo, err)
			}
			node.PublicKey, hasPublicKey = v, true
		case "PrivateKey":
			v, err := wgtypes.ParseKey(value)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行: PrivateKey 无效: %w", lineNo, err)
			}
			node.PrivateKey = &v
		case "PresharedKey":
			v, err := wgtypes.ParseKey(value)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行: PresharedKey 无效: %w", lineNo, err)
			}
			node.PresharedKey = v
		case "AllowedIPs":
			for _, item := range strings.Split(value, ",") {
				ip, network, err := net.ParseCIDR(strings.TrimSpace(item))
				if err != nil {
					return nil, fmt.Errorf("第 %d 行: AllowedIPs 无效: %w", lineNo, err)
				}
				network.IP = ip
				node.AllowedIPs = append(node.AllowedIPs, *network)
			}
		case "Endpoint":
			v, err := net.ResolveUDPAddr("udp", value)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行: Endpoint 无效: %w", lineNo, err)
			}
			node.Endpoint = v
		case "PersistentKeepalive":
			v, err := strconv.ParseUint(value, 10, 16)
			if err != nil {
				return nil, fmt.Errorf("第 %d 行: PersistentKeepalive 无效: %w", lineNo, err)
			}
			node.PersistentKeepalive = time.Duration(v) * time.Second
		default:
			return nil, fmt.Errorf("第 %d 行: 未知字段 %q", lineNo, key)
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func mergeSyncGiteeNodes(remote, local []syncGiteeNode) []syncGiteeNode {
	merged := append([]syncGiteeNode(nil), remote...)
	indexes := make(map[wgtypes.Key]int, len(merged))
	for i := range merged {
		indexes[merged[i].PublicKey] = i
	}
	for _, node := range local {
		if i, ok := indexes[node.PublicKey]; ok {
			merged[i] = node
			continue
		}
		indexes[node.PublicKey] = len(merged)
		merged = append(merged, node)
	}
	return merged
}

func encodeSyncGiteeNodes(nodes []syncGiteeNode) string {
	var b strings.Builder
	for i, node := range nodes {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintln(&b, "[Peer]")
		fmt.Fprintf(&b, "Name = %s\n", node.Name)
		fmt.Fprintf(&b, "PublicKey = %s\n", node.PublicKey)
		if node.PrivateKey != nil {
			fmt.Fprintf(&b, "PrivateKey = %s\n", node.PrivateKey.String())
		}
		if len(node.AllowedIPs) > 0 {
			values := make([]string, len(node.AllowedIPs))
			for i := range node.AllowedIPs {
				values[i] = node.AllowedIPs[i].String()
			}
			fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(values, ", "))
		}
		if node.Endpoint != nil {
			fmt.Fprintf(&b, "Endpoint = %s\n", node.Endpoint)
		}
		if node.PersistentKeepalive != 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", int64(node.PersistentKeepalive/time.Second))
		}
	}
	return b.String()
}
