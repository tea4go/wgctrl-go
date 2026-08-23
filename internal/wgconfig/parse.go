package wgconfig

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Parse reads a wg(8) configuration file.
func Parse(r io.Reader, appendMode bool) (wgtypes.Config, error) {
	var cfg wgtypes.Config
	if !appendMode {
		zeroKey, zero := wgtypes.Key{}, 0
		cfg.PrivateKey, cfg.ListenPort, cfg.FirewallMark = &zeroKey, &zero, &zero
		cfg.ReplacePeers = true
	}
	section := ""
	var peer *wgtypes.PeerConfig
	var peerHasPublicKey []bool
	peerHasName := false
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for s.Scan() {
		lineNo++
		rawLine := s.Text()
		if encodedName, ok := peerNameComment(rawLine); ok {
			if section != "peer" || peer == nil {
				return cfg, fmt.Errorf("line %d: peer name outside a Peer section", lineNo)
			}
			if peerHasName {
				return cfg, fmt.Errorf("line %d: duplicate peer name", lineNo)
			}
			var name string
			if err := json.Unmarshal([]byte(encodedName), &name); err != nil {
				return cfg, fmt.Errorf("line %d: invalid peer name: %w", lineNo, err)
			}
			if err := validatePeerName(name); err != nil {
				return cfg, fmt.Errorf("line %d: invalid peer name: %w", lineNo, err)
			}
			peer.Name = &name
			peerHasName = true
			continue
		}
		line := cleanLine(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.ToLower(line[1 : len(line)-1])
			switch name {
			case "interface":
				section, peer = "interface", nil
				peerHasName = false
			case "peer":
				cfg.Peers = append(cfg.Peers, wgtypes.PeerConfig{ReplaceAllowedIPs: true})
				peerHasPublicKey = append(peerHasPublicKey, false)
				peer = &cfg.Peers[len(cfg.Peers)-1]
				section = "peer"
				peerHasName = false
			default:
				return cfg, fmt.Errorf("line %d: unknown section %q", lineNo, name)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return cfg, fmt.Errorf("line %d: invalid configuration line %q", lineNo, line)
		}
		key = strings.ToLower(key)
		if section == "interface" {
			switch key {
			case "privatekey":
				v, err := parseKey(value)
				if err != nil {
					return cfg, fieldErr(lineNo, "PrivateKey", value, err)
				}
				cfg.PrivateKey = &v
			case "listenport":
				v, err := parseUint(value, 65535)
				if err != nil {
					return cfg, fieldErr(lineNo, "ListenPort", value, err)
				}
				n := int(v)
				cfg.ListenPort = &n
			case "fwmark":
				v, err := parseFwmark(value)
				if err != nil {
					return cfg, fieldErr(lineNo, "FwMark", value, err)
				}
				n := int(v)
				cfg.FirewallMark = &n
			default:
				return cfg, fmt.Errorf("line %d: unknown interface field %q=%q", lineNo, key, value)
			}
		} else if section == "peer" && peer != nil {
			switch key {
			case "publickey":
				v, err := parseKey(value)
				if err != nil {
					return cfg, fieldErr(lineNo, "PublicKey", value, err)
				}
				peer.PublicKey = v
				peerHasPublicKey[len(peerHasPublicKey)-1] = true
			case "presharedkey":
				v, err := parseKey(value)
				if err != nil {
					return cfg, fieldErr(lineNo, "PresharedKey", value, err)
				}
				peer.PresharedKey = &v
			case "endpoint":
				v, err := resolveEndpoint(value)
				if err != nil {
					return cfg, fieldErr(lineNo, "Endpoint", value, err)
				}
				peer.Endpoint = v
			case "allowedips":
				peer.ReplaceAllowedIPs = true
				ops, incremental, err := parseAllowedIPs(value)
				if err != nil {
					return cfg, fieldErr(lineNo, "AllowedIPs", value, err)
				}
				peer.AllowedIPOperations = append(peer.AllowedIPOperations, ops...)
				if incremental {
					peer.ReplaceAllowedIPs = false
				}
			case "persistentkeepalive":
				v, err := parseUintOrOff(value, 65535)
				if err != nil {
					return cfg, fieldErr(lineNo, "PersistentKeepalive", value, err)
				}
				d := time.Duration(v) * time.Second
				peer.PersistentKeepaliveInterval = &d
			default:
				return cfg, fmt.Errorf("line %d: unknown peer field %q=%q", lineNo, key, value)
			}
		} else {
			return cfg, fmt.Errorf("line %d: field %q outside a section", lineNo, key)
		}
	}
	if err := s.Err(); err != nil {
		return cfg, fmt.Errorf("read configuration: %w", err)
	}
	for i := range cfg.Peers {
		if !peerHasPublicKey[i] {
			return cfg, fmt.Errorf("peer %d missing PublicKey", i+1)
		}
	}
	return cfg, nil
}

func peerNameComment(line string) (string, bool) {
	line = strings.TrimSpace(line)
	const prefix = "# wgctrl-peer-name"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	rest := strings.TrimSpace(line[len(prefix):])
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	return strings.TrimSpace(rest[1:]), true
}

func validatePeerName(name string) error {
	if !utf8.ValidString(name) {
		return fmt.Errorf("invalid UTF-8")
	}
	if len(name) > 255 {
		return fmt.Errorf("name is longer than 255 bytes")
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("name contains a forbidden control character")
	}
	return nil
}

func cleanLine(input string) string {
	if i := strings.IndexByte(input, '#'); i >= 0 {
		input = input[:i]
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r':
			return -1
		default:
			return r
		}
	}, input)
}

var (
	lookupEndpointIPs  = net.DefaultResolver.LookupIP
	lookupEndpointPort = net.LookupPort
	sleepEndpointRetry = time.Sleep
)

func resolveEndpoint(value string) (*net.UDPAddr, error) {
	retries, err := endpointResolutionRetries()
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, fmt.Errorf("empty endpoint")
	}
	host, portText, err := splitEndpoint(value)
	if err != nil {
		return nil, err
	}
	port, err := lookupEndpointPort("udp", portText)
	if err != nil {
		return nil, err
	}
	if i := strings.LastIndexByte(host, '%'); i >= 0 {
		if ip := net.ParseIP(host[:i]); ip != nil {
			return &net.UDPAddr{IP: ip, Port: port, Zone: host[i+1:]}, nil
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}, nil
	}
	backoff := time.Second
	for {
		ips, lookupErr := lookupEndpointIPs(context.Background(), "ip", host)
		if lookupErr == nil {
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IPv4 or IPv6 address for %q", host)
			}
			return &net.UDPAddr{IP: ips[0], Port: port}, nil
		}
		if permanentEndpointError(lookupErr) || retries == 0 {
			return nil, lookupErr
		}
		if retries > 0 {
			retries--
		}
		sleepEndpointRetry(backoff)
		backoff = minDuration(20*time.Second, backoff*6/5)
	}
}

func splitEndpoint(value string) (host, port string, err error) {
	if strings.HasPrefix(value, "[") {
		return net.SplitHostPort(value)
	}
	i := strings.LastIndexByte(value, ':')
	if i < 0 {
		return "", "", fmt.Errorf("missing port in address")
	}
	return value[:i], value[i+1:], nil
}

func permanentEndpointError(err error) bool {
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		return true
	}
	// Go does not expose the platform EAI_* result. IsNotFound maps clearly to
	// EAI_NONAME; other explicitly temporary or timeout DNS errors are retried.
	return dnsErr.IsNotFound || (!dnsErr.IsTemporary && !dnsErr.IsTimeout)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func endpointResolutionRetries() (int, error) {
	value, ok := os.LookupEnv("WG_ENDPOINT_RESOLUTION_RETRIES")
	if !ok {
		return 15, nil
	}
	if value == "infinity" {
		return -1, nil
	}
	number := value
	if strings.HasPrefix(number, "+") {
		number = number[1:]
	}
	v, err := strconv.ParseUint(number, 10, 31)
	if err != nil {
		return 0, fmt.Errorf("unable to parse WG_ENDPOINT_RESOLUTION_RETRIES: %q", value)
	}
	return int(v), nil
}

func parseKey(value string) (wgtypes.Key, error) {
	key, err := wgtypes.ParseKey(value)
	if err != nil {
		return wgtypes.Key{}, err
	}
	if key.String() != value {
		return wgtypes.Key{}, fmt.Errorf("non-canonical key")
	}
	return key, nil
}

func fieldErr(line int, field, value string, err error) error {
	return fmt.Errorf("line %d: %s=%q: %w", line, field, value, err)
}

func parseUint(value string, max uint64) (uint64, error) {
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("invalid number")
	}
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil || v > max {
		return 0, fmt.Errorf("out of range")
	}
	return v, nil
}
func parseUintOrOff(value string, max uint64) (uint64, error) {
	if strings.EqualFold(value, "off") {
		return 0, nil
	}
	return parseUint(value, max)
}
func parseFwmark(value string) (uint64, error) {
	if strings.EqualFold(value, "off") {
		return 0, nil
	}
	if strings.HasPrefix(value, "0x") {
		if len(value) == 2 {
			return 0, fmt.Errorf("invalid hexadecimal mark")
		}
		v, e := strconv.ParseUint(value[2:], 16, 32)
		return v, e
	}
	return parseUint(value, ^uint64(0)>>32)
}

func parseAllowedIPs(value string) ([]wgtypes.AllowedIPConfig, bool, error) {
	if value == "" {
		return nil, false, nil
	}
	parts := strings.Split(value, ",")
	ops := make([]wgtypes.AllowedIPConfig, 0, len(parts))
	incremental := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false, fmt.Errorf("empty entry")
		}
		op := wgtypes.AllowedIPSet
		if part[0] == '+' || part[0] == '-' {
			incremental = true
			if part[0] == '+' {
				op = wgtypes.AllowedIPAdd
			} else {
				op = wgtypes.AllowedIPRemove
			}
			part = part[1:]
		}
		ip, n, err := net.ParseCIDR(part)
		if err != nil {
			if !strings.Contains(part, "/") {
				ip = net.ParseIP(part)
				if ip == nil {
					return nil, false, err
				}
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				n = &net.IPNet{Mask: net.CIDRMask(bits, bits)}
			} else {
				return nil, false, err
			}
		}
		n.IP = ip
		ops = append(ops, wgtypes.AllowedIPConfig{IPNet: *n, Operation: op})
	}
	return ops, incremental, nil
}
