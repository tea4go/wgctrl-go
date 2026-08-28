package wgtypes

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/curve25519"
)

// DeviceType 用于指定 WireGuard 设备的底层实现类型。
type DeviceType int

// 可用的 DeviceType 枚举值。
const (
	Unknown DeviceType = iota
	LinuxKernel
	OpenBSDKernel
	FreeBSDKernel
	WindowsKernel
	Userspace
)

// String 返回 DeviceType 的字符串表示形式。
func (dt DeviceType) String() string {
	switch dt {
	case LinuxKernel:
		return "Linux kernel"
	case OpenBSDKernel:
		return "OpenBSD kernel"
	case FreeBSDKernel:
		return "FreeBSD kernel"
	case WindowsKernel:
		return "Windows kernel"
	case Userspace:
		return "userspace"
	default:
		return "unknown"
	}
}

// Device 表示一个 WireGuard 设备。
type Device struct {
	// Name 是设备的名称。
	Name string

	// Type 用于指定设备的底层实现类型。
	Type DeviceType

	// PrivateKey 是设备的私钥。
	PrivateKey Key

	// HasPrivateKey 指示是否已配置私钥。
	HasPrivateKey bool

	// PublicKey 是设备的公钥，由其私钥计算得出。
	PublicKey Key

	// HasPublicKey 指示是否已配置公钥。
	HasPublicKey bool

	// ListenPort 是设备的网络监听端口。
	ListenPort int

	// HasListenPort 指示是否已配置监听端口。
	HasListenPort bool

	// FirewallMark 是设备当前的防火墙标记。
	//
	// 防火墙标记可与防火墙软件结合使用，
	// 对出站的 WireGuard 数据包执行相应操作。
	FirewallMark int

	// HasFirewallMark 指示是否已配置防火墙标记。
	HasFirewallMark bool

	// Peers 是与此设备关联的网络对等节点列表。
	Peers []Peer
}

// KeyLen 是 WireGuard 密钥的期望长度。
const KeyLen = 32 // wgh.KeyLen

// Key 表示公钥、私钥或预共享密钥。可以使用本包中的 Key 构造函数
// 来创建适用于上述各种用途的 Key。
type Key [KeyLen]byte

// GenerateKey 从加密安全的随机源生成一个可用作预共享密钥的 Key。
//
// 输出的 Key 不应用作私钥；请改用 GeneratePrivateKey。
func GenerateKey() (Key, error) {
	b := make([]byte, KeyLen)
	if _, err := rand.Read(b); err != nil {
		return Key{}, fmt.Errorf("wgtypes: 读取随机字节失败: %v", err)
	}

	return NewKey(b)
}

// GeneratePrivateKey 从加密安全的随机源生成一个可用作私钥的 Key。
func GeneratePrivateKey() (Key, error) {
	key, err := GenerateKey()
	if err != nil {
		return Key{}, err
	}

	// 使用以下文档中描述的算法修改随机字节:
	// https://cr.yp.to/ecdh.html.
	// 第 0 字节低 3 位清零，保证其为 8 的倍数
	key[0] &= 248
	// 第 31 字节最高位清零，保证小于 2^255
	key[31] &= 127
	// 第 31 字节次高位置 1，保证大于等于 2^254
	key[31] |= 64

	return key, nil
}

// NewKey 从现有的字节切片创建一个 Key。字节切片的长度必须恰好为 32 字节。
func NewKey(b []byte) (Key, error) {
	if len(b) != KeyLen {
		return Key{}, fmt.Errorf("wgtypes: 密钥长度不正确: %d", len(b))
	}

	var k Key
	copy(k[:], b)

	return k, nil
}

// ParseKey 从 base64 编码的字符串中解析出 Key，格式与 Key.String 方法输出的一致。
func ParseKey(s string) (Key, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return Key{}, fmt.Errorf("wgtypes: 解析 base64 编码的密钥失败: %v", err)
	}

	return NewKey(b)
}

// PublicKey 从私钥 k 计算出对应的公钥。
//
// PublicKey 仅应在 k 是私钥时调用。
func (k Key) PublicKey() Key {
	var (
		pub  [KeyLen]byte
		priv = [KeyLen]byte(k)
	)

	// ScalarBaseMult 会根据 https://cr.yp.to/ecdh.html 使用正确的基点值，
	// 因此无需显式指定基点。
	// 使用 curve25519 标量乘法: pub = priv * G，其中 G 是 curve25519 的标准基点
	curve25519.ScalarBaseMult(&pub, &priv)

	return Key(pub)
}

// String 返回 Key 的 base64 编码字符串表示形式。
//
// 可以使用 ParseKey 从该字符串重新生成 Key。
func (k Key) String() string {
	return base64.StdEncoding.EncodeToString(k[:])
}

// Peer 表示 Device 的一个 WireGuard 对等节点。
type Peer struct {
	// Name 是用户态保存的可选对等节点名称。
	//
	// WireGuard 原生协议不保存此字段；nil 或空字符串表示未命名。
	Name *string

	// PublicKey 是对等节点的公钥，由其私钥计算得出。
	//
	// Peer 中始终存在 PublicKey 字段。
	PublicKey Key

	// PresharedKey 是可选的预共享密钥，可用作对等节点通信的
	// 额外安全层。
	//
	// 零值 Key 表示未配置预共享密钥。
	PresharedKey Key

	// HasPresharedKey 指示是否已配置预共享密钥。
	HasPresharedKey bool

	// Endpoint 是此对等节点最近一次通信时使用的源地址。
	Endpoint *net.UDPAddr

	// HasEndpoint 指示是否已配置端点地址。
	HasEndpoint bool

	// PersistentKeepaliveInterval 指定多久向对等节点发送一个
	// "空" 数据包以保持连接存活。
	//
	// 值为 0 表示禁用持续保持存活功能。
	PersistentKeepaliveInterval time.Duration

	// HasPersistentKeepaliveInterval 指示是否已配置持续保持存活间隔。
	HasPersistentKeepaliveInterval bool

	// LastHandshakeTime 指示与此对等节点最近一次执行握手的时间。
	//
	// 零值 time.Time 表示尚未与此对等节点进行过握手。
	LastHandshakeTime time.Time

	// ReceiveBytes 指示从此对等节点接收的字节数。
	ReceiveBytes int64

	// TransmitBytes 指示发送给此对等节点的字节数。
	TransmitBytes int64

	// AllowedIPs 指定允许此对等节点通信的 IPv4 和 IPv6 地址。
	//
	// 0.0.0.0/0 表示允许所有 IPv4 地址，::/0 表示允许所有 IPv6 地址。
	AllowedIPs []net.IPNet

	// ProtocolVersion 指定此对等节点使用的 WireGuard 协议版本。
	//
	// 值为 0 表示将使用最新的协议版本。
	ProtocolVersion int
}

// Config 表示 WireGuard 设备的配置。
//
// 由于某些 Go 类型的零值对 WireGuard 的 Config 字段可能具有特殊意义，
// 因此部分字段使用了指针类型。配置设备时，仅会应用非 nil 的指针字段。
type Config struct {
	// PrivateKey 在非 nil 时指定私钥配置。
	//
	// 非 nil 且为零值的 Key 将清除私钥。
	PrivateKey *Key

	// ListenPort 在非 nil 时指定设备的监听端口。
	ListenPort *int

	// FirewallMark 在非 nil 时指定设备的防火墙标记。
	//
	// 若非 nil 且设置为 0，将清除防火墙标记。
	FirewallMark *int

	// ReplacePeers 指示此配置中的 Peers 是否应替换现有对等节点列表，
	// 而非追加到现有列表中。
	ReplacePeers bool

	// Peers 指定要应用到设备的对等节点配置列表。
	Peers []PeerConfig
}

// TODO(mdlayher): 考虑在 PeerConfig 中添加 ProtocolVersion 字段。

// AllowedIPOperation 指定应如何应用允许的 IP 规则。
type AllowedIPOperation uint8

// 可用的 AllowedIPOperation 枚举值。
const (
	// AllowedIPSet 表示设置（替换）允许的 IP 列表
	AllowedIPSet AllowedIPOperation = iota
	// AllowedIPAdd 表示向允许的 IP 列表中添加
	AllowedIPAdd
	// AllowedIPRemove 表示从允许的 IP 列表中移除
	AllowedIPRemove
)

// AllowedIPConfig 用于指定一个允许的 IP 及其对应的操作类型。
type AllowedIPConfig struct {
	// IPNet 是 IP 网络地址段（CIDR 表示）
	IPNet net.IPNet
	// Operation 是对该 IP 段执行的操作（设置/添加/移除）
	Operation AllowedIPOperation
}

// PeerConfig 表示 WireGuard 设备的对等节点配置。
//
// 由于某些 Go 类型的零值对 WireGuard 的 PeerConfig 字段可能具有特殊意义，
// 因此部分字段使用了指针类型。配置对等节点时，仅会应用非 nil 的指针字段。
type PeerConfig struct {
	// Name 在非 nil 时指定用户态保存的对等节点名称。
	//
	// 非 nil 且为空字符串时清除名称。
	Name *string

	// PublicKey 指定此对等节点的公钥。PublicKey 是所有 PeerConfig 的必填字段。
	PublicKey Key

	// Remove 指示是否应从设备的对等节点列表中移除此公钥对应的对等节点。
	Remove bool

	// UpdateOnly 指定仅当此对等节点已作为接口的一部分存在时，
	// 才对其执行操作。
	UpdateOnly bool

	// PresharedKey 在非 nil 时指定对等节点的预共享密钥配置。
	//
	// 非 nil 且为零值的 Key 将清除预共享密钥。
	PresharedKey *Key

	// Endpoint 在非 nil 时指定此对等节点条目的端点地址。
	Endpoint *net.UDPAddr

	// PersistentKeepaliveInterval 在非 nil 时指定此对等节点的
	// 持续保持存活间隔。
	//
	// 非 nil 且值为 0 时将清除持续保持存活间隔设置。
	PersistentKeepaliveInterval *time.Duration

	// ReplaceAllowedIPs 指示此对等节点配置中指定的允许 IP 是否应
	// 替换任何现有的允许 IP，而非追加到允许 IP 列表中。
	ReplaceAllowedIPs bool

	// AllowedIPs 以 CIDR 表示法指定此对等节点的允许 IP 地址列表。
	AllowedIPs []net.IPNet

	// AllowedIPOperations 按顺序指定此对等节点的允许 IP 操作序列。
	AllowedIPOperations []AllowedIPConfig
}
