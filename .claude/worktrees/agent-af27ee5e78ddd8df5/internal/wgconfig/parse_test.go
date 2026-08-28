package wgconfig

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const testKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestParseValidConfiguration(t *testing.T) {
	input := " # comment\n [interface]\n privateKEY = " + testKey + " # inline\n ListenPort=65535\n FwMark = 0xca6c\n" +
		" [PEER]\n PublicKey = " + testKey + "\n PresharedKey = " + testKey + "\n Endpoint = 192.0.2.1:51820\n" +
		" AllowedIPs = 10.0.0.0/24,2001:db8::/64,+192.0.2.1/32,-198.51.100.0/24,203.0.113.1\n PersistentKeepalive = off\n" +
		" [Peer]\n PublicKey=" + testKey + "\n AllowedIPs=\n PersistentKeepalive=65535\n"
	got, err := Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.PrivateKey == nil || got.PrivateKey.String() != testKey || got.ListenPort == nil || *got.ListenPort != 65535 || got.FirewallMark == nil || *got.FirewallMark != 0xca6c || !got.ReplacePeers {
		t.Fatalf("unexpected interface config: %#v", got)
	}
	if len(got.Peers) != 2 || got.Peers[0].PublicKey.String() != testKey {
		t.Fatalf("unexpected peers: %#v", got.Peers)
	}
	p := got.Peers[0]
	if p.ReplaceAllowedIPs || len(p.AllowedIPOperations) != 5 {
		t.Fatalf("unexpected allowed IP mode: %#v", p)
	}
	if p.AllowedIPOperations[0].Operation != wgtypes.AllowedIPSet || p.AllowedIPOperations[2].Operation != wgtypes.AllowedIPAdd || p.AllowedIPOperations[3].Operation != wgtypes.AllowedIPRemove {
		t.Fatalf("unexpected allowed IP operations: %#v", p.AllowedIPOperations)
	}
	if p.Endpoint == nil || p.Endpoint.String() != "192.0.2.1:51820" {
		t.Fatalf("unexpected endpoint: %#v", p.Endpoint)
	}
	if p.PresharedKey == nil || p.PersistentKeepaliveInterval == nil || *p.PersistentKeepaliveInterval != 0 {
		t.Fatalf("unexpected peer values: %#v", p)
	}
	if !got.Peers[1].ReplaceAllowedIPs || got.Peers[1].AllowedIPOperations != nil || *got.Peers[1].PersistentKeepaliveInterval != 65535*time.Second {
		t.Fatalf("unexpected second peer: %#v", got.Peers[1])
	}
}

func TestParsePeerNameComment(t *testing.T) {
	input := "[Peer]\n# wgctrl-peer-name = \"北京 #1 \\\"出口\\\"\"\nPublicKey=" + testKey + "\n"
	got, err := Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Peers[0].Name == nil || *got.Peers[0].Name != "北京 #1 \"出口\"" {
		t.Fatalf("unexpected name: %#v", got.Peers[0].Name)
	}

	got, err = Parse(strings.NewReader("[Peer]\nPublicKey="+testKey+"\n"), false)
	if err != nil || got.Peers[0].Name != nil {
		t.Fatalf("legacy configuration name=%#v err=%v", got.Peers[0].Name, err)
	}

	got, err = Parse(strings.NewReader("[Peer]\n# wgctrl-peer-name = \"\"\nPublicKey="+testKey+"\n"), false)
	if err != nil || got.Peers[0].Name == nil || *got.Peers[0].Name != "" {
		t.Fatalf("empty name=%#v err=%v", got.Peers[0].Name, err)
	}
}

func TestParseRejectsInvalidPeerNameComment(t *testing.T) {
	for _, input := range []string{
		"# wgctrl-peer-name = \"outside\"\n[Peer]\nPublicKey=" + testKey,
		"[Peer]\n# wgctrl-peer-name = \"one\"\n# wgctrl-peer-name = \"two\"\nPublicKey=" + testKey,
		"[Peer]\n# wgctrl-peer-name = invalid\nPublicKey=" + testKey,
		"[Peer]\n# wgctrl-peer-name = \"bad\\nname\"\nPublicKey=" + testKey,
	} {
		if _, err := Parse(strings.NewReader(input+"\n"), false); err == nil {
			t.Fatalf("accepted invalid name comment: %q", input)
		}
	}
}

func TestParseRepeatedAllowedIPsFields(t *testing.T) {
	tests := []struct {
		name       string
		fields     string
		replace    bool
		operations []wgtypes.AllowedIPOperation
	}{
		{"incremental then ordinary", "AllowedIPs=+10.0.0.0/24\nAllowedIPs=192.0.2.0/24", true, []wgtypes.AllowedIPOperation{wgtypes.AllowedIPAdd, wgtypes.AllowedIPSet}},
		{"incremental then empty", "AllowedIPs=+10.0.0.0/24\nAllowedIPs=", true, []wgtypes.AllowedIPOperation{wgtypes.AllowedIPAdd}},
		{"ordinary then incremental", "AllowedIPs=10.0.0.0/24\nAllowedIPs=+192.0.2.0/24", false, []wgtypes.AllowedIPOperation{wgtypes.AllowedIPSet, wgtypes.AllowedIPAdd}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader("[Peer]\nPublicKey="+testKey+"\n"+tt.fields+"\n"), false)
			if err != nil {
				t.Fatal(err)
			}
			peer := got.Peers[0]
			if peer.ReplaceAllowedIPs != tt.replace || len(peer.AllowedIPOperations) != len(tt.operations) {
				t.Fatalf("unexpected peer: %#v", peer)
			}
			for i, want := range tt.operations {
				if peer.AllowedIPOperations[i].Operation != want {
					t.Fatalf("operation %d = %v, want %v", i, peer.AllowedIPOperations[i].Operation, want)
				}
			}
		})
	}
}

func TestParseRemovesASCIISpaceBeforeComment(t *testing.T) {
	input := "[\tInter\rface ]\n" +
		"Private Key=" + strings.Join(strings.Split(testKey, ""), "\t") + "\n" +
		"Listen Port=5\r1820\n" +
		"[ Pe\ter ]\n" +
		"Public Key=" + strings.Join(strings.Split(testKey, ""), "\r") + "\n" +
		"Endpoint=[2001: db8::1]:51\t820\n" +
		"Allowed IPs=10.0. 0.1/24,2001:db8:: 1/64\n" +
		"Persistent Keepalive=2\r5 # ignored \t text\n"
	got, err := Parse(strings.NewReader(input), false)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.ListenPort == nil || *got.ListenPort != 51820 || got.Peers[0].Endpoint.String() != "[2001:db8::1]:51820" {
		t.Fatalf("whitespace not removed: %#v", got)
	}
	if got.Peers[0].AllowedIPOperations[0].IPNet.String() != "10.0.0.1/24" || got.Peers[0].AllowedIPOperations[1].IPNet.String() != "2001:db8::1/64" {
		t.Fatalf("unexpected allowed IPs: %#v", got.Peers[0].AllowedIPOperations)
	}
}

func TestParseAcceptsLineLongerThanScannerLimit(t *testing.T) {
	if _, err := Parse(strings.NewReader(strings.Repeat(" ", 70*1024)+"[Interface]\n"), false); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

func TestParseAppendLeavesInterfaceFieldsUnset(t *testing.T) {
	got, err := Parse(strings.NewReader("[Interface]\n[Peer]\nPublicKey="+testKey+"\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if got.PrivateKey != nil || got.ListenPort != nil || got.FirewallMark != nil || got.ReplacePeers {
		t.Fatalf("append unexpectedly sets interface fields: %#v", got)
	}
}

func TestParseRejectsNonCanonicalKeys(t *testing.T) {
	nonCanonicalKey := testKey[:len(testKey)-2] + "B="
	for _, field := range []string{"PrivateKey", "PublicKey", "PresharedKey"} {
		input := "[Interface]\n" + field + "=" + nonCanonicalKey + "\n"
		if field != "PrivateKey" {
			input = "[Peer]\nPublicKey=" + testKey + "\n" + field + "=" + nonCanonicalKey + "\n"
		}
		if _, err := Parse(strings.NewReader(input), false); err == nil {
			t.Fatalf("Parse accepted non-canonical %s", field)
		}
	}
}

func TestParseStrictBoundaries(t *testing.T) {
	zeroKey := (wgtypes.Key{}).String()
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"field outside section", "ListenPort=1", false}, {"missing equals", "[Interface]\nListenPort", false},
		{"incomplete section open", "[Interface", false}, {"incomplete section close", "Interface]", false}, {"empty key", "[Interface]\n=1", false},
		{"empty endpoint", "[Peer]\nPublicKey=" + testKey + "\nEndpoint=", false}, {"empty listen port", "[Interface]\nListenPort=", false},
		{"empty fwmark", "[Interface]\nFwMark=", false}, {"empty keepalive", "[Peer]\nPublicKey=" + testKey + "\nPersistentKeepalive=", false},
		{"empty private key", "[Interface]\nPrivateKey=", false}, {"empty preshared key", "[Peer]\nPublicKey=" + testKey + "\nPresharedKey=", false},
		{"empty public key", "[Peer]\nPublicKey=", false}, {"zero public key", "[Peer]\nPublicKey=" + zeroKey, true},
		{"allowed IP empty entry middle", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=10.0.0.0/24,,192.0.2.0/24", false},
		{"allowed IP empty entry trailing", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=10.0.0.0/24,", false},
		{"allowed IP plus only", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=+", false}, {"allowed IP minus only", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=-", false},
		{"duplicate public key", "[Peer]\nPublicKey=" + testKey + "\nPublicKey=" + zeroKey, true},
		{"duplicate interface and peer fields", "[Interface]\nListenPort=1\nListenPort=2\n[Peer]\nPublicKey=" + testKey + "\nPersistentKeepalive=3\nPersistentKeepalive=4", true},
		{"duplicate endpoint", "[Peer]\nPublicKey=" + testKey + "\nEndpoint=192.0.2.1:1\nEndpoint=[2001:db8::1]:2", true},
		{"IPv6 allowed IP", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=2001:db8::1/128", true},
		{"host bits preserved", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=10.0.0.1/24", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input+"\n"), false)
			if tt.valid && err != nil {
				t.Fatalf("Parse rejected valid input: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("Parse accepted invalid input: %#v", got)
			}
			if tt.name == "host bits preserved" && err == nil && got.Peers[0].AllowedIPOperations[0].IPNet.String() != "10.0.0.1/24" {
				t.Fatalf("host bits lost: %s", got.Peers[0].AllowedIPOperations[0].IPNet.String())
			}
		})
	}
}

func TestParseRejectsWGQuickAndInvalidValues(t *testing.T) {
	cases := []string{
		"[Interface]\nAddress=10.0.0.1/24", "[Interface]\nDNS=1.1.1.1", "[Interface]\nMTU=1420", "[Interface]\nTable=auto",
		"[Interface]\nPreUp=echo x", "[Interface]\nSaveConfig=true", "[Unknown]\nFoo=bar", "[Interface]\nUnknown=x",
		"[Peer]\nAllowedIPs=10.0.0.0/24", "[Interface]\nListenPort=65536", "[Interface]\nFwMark=0x100000000", "[Interface]\nPrivateKey=bad",
		"[Peer]\nPublicKey=" + testKey + "\nEndpoint=192.0.2.1", "[Peer]\nPublicKey=" + testKey + "\nAllowedIPs=10.0.0.0/33",
		"[Peer]\nPublicKey=" + testKey + "\nPersistentKeepalive=65536",
	}
	for _, input := range cases {
		t.Run(strings.ReplaceAll(input, "\n", "/"), func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input), false); err == nil {
				t.Fatalf("Parse accepted %q", input)
			}
		})
	}
}

func TestResolveEndpointIPv6ZoneAndServiceName(t *testing.T) {
	oldLookupPort := lookupEndpointPort
	t.Cleanup(func() { lookupEndpointPort = oldLookupPort })
	lookupEndpointPort = func(network, service string) (int, error) {
		if network != "udp" || service != "domain" {
			t.Fatalf("LookupPort(%q, %q)", network, service)
		}
		return 53, nil
	}

	got, err := resolveEndpoint("[fe80::1%example]:domain")
	if err != nil {
		t.Fatalf("resolveEndpoint: %v", err)
	}
	if got.IP.String() != "fe80::1" || got.Zone != "example" || got.Port != 53 {
		t.Fatalf("unexpected endpoint: %#v", got)
	}
}

func TestResolveEndpointUnbracketedIPv6(t *testing.T) {
	for _, tt := range []struct {
		endpoint string
		ip       string
		zone     string
	}{
		{"2001:db8::1:51820", "2001:db8::1", ""},
		{"fe80::1%eth0:51820", "fe80::1", "eth0"},
	} {
		t.Run(tt.endpoint, func(t *testing.T) {
			got, err := resolveEndpoint(tt.endpoint)
			if err != nil {
				t.Fatalf("resolveEndpoint: %v", err)
			}
			if got.IP.String() != tt.ip || got.Zone != tt.zone || got.Port != 51820 {
				t.Fatalf("unexpected endpoint: %#v", got)
			}
		})
	}
}

func TestResolveEndpointRetries(t *testing.T) {
	oldLookup, oldSleep := lookupEndpointIPs, sleepEndpointRetry
	t.Cleanup(func() { lookupEndpointIPs, sleepEndpointRetry = oldLookup, oldSleep })
	t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", "2")
	attempts := 0
	var sleeps []time.Duration
	lookupEndpointIPs = func(context.Context, string, string) ([]net.IP, error) {
		attempts++
		if attempts < 3 {
			return nil, &net.DNSError{Err: "temporary", Name: "example.com", IsTemporary: true}
		}
		return []net.IP{net.ParseIP("192.0.2.1")}, nil
	}
	sleepEndpointRetry = func(d time.Duration) { sleeps = append(sleeps, d) }
	got, err := resolveEndpoint("example.com:51820")
	if err != nil || got.String() != "192.0.2.1:51820" || attempts != 3 || len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != 1200*time.Millisecond {
		t.Fatalf("got=%v err=%v attempts=%d sleeps=%v", got, err, attempts, sleeps)
	}
}

func TestResolveEndpointRetryLimitsAndErrors(t *testing.T) {
	oldLookup, oldSleep := lookupEndpointIPs, sleepEndpointRetry
	t.Cleanup(func() { lookupEndpointIPs, sleepEndpointRetry = oldLookup, oldSleep })
	sleepEndpointRetry = func(time.Duration) { t.Fatal("unexpected sleep") }
	for _, tt := range []struct {
		name, retries string
		err           error
	}{
		{"zero", "0", &net.DNSError{Err: "temporary", IsTemporary: true}},
		{"permanent DNS", "infinity", &net.DNSError{Err: "no such host", IsNotFound: true}},
		{"non DNS", "infinity", errors.New("boom")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", tt.retries)
			attempts := 0
			lookupEndpointIPs = func(context.Context, string, string) ([]net.IP, error) { attempts++; return nil, tt.err }
			if _, err := resolveEndpoint("example.com:1"); err == nil || attempts != 1 {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestResolveEndpointInfinityStopsAfterSuccess(t *testing.T) {
	oldLookup, oldSleep := lookupEndpointIPs, sleepEndpointRetry
	t.Cleanup(func() { lookupEndpointIPs, sleepEndpointRetry = oldLookup, oldSleep })
	t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", "infinity")
	attempts, sleeps := 0, 0
	lookupEndpointIPs = func(context.Context, string, string) ([]net.IP, error) {
		attempts++
		if attempts == 4 {
			return []net.IP{net.ParseIP("2001:db8::1")}, nil
		}
		return nil, &net.DNSError{Err: "timeout", IsTimeout: true}
	}
	sleepEndpointRetry = func(time.Duration) { sleeps++ }
	got, err := resolveEndpoint("example.com:2")
	if err != nil || got.String() != "[2001:db8::1]:2" || attempts != 4 || sleeps != 3 {
		t.Fatalf("got=%v err=%v attempts=%d sleeps=%d", got, err, attempts, sleeps)
	}
}

func TestEndpointResolutionRetriesEnvironment(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int
	}{
		{"+1", 1},
		{"0002", 2},
	} {
		t.Run("valid "+tt.value, func(t *testing.T) {
			t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", tt.value)
			got, err := endpointResolutionRetries()
			if err != nil || got != tt.want {
				t.Fatalf("got=%d err=%v, want %d", got, err, tt.want)
			}
		})
	}
	for _, value := range []string{"", "-1", "1x", "2147483648", "Infinity"} {
		t.Run("invalid "+value, func(t *testing.T) {
			t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", value)
			if _, err := endpointResolutionRetries(); err == nil {
				t.Fatalf("accepted %q", value)
			}
		})
	}
}

func TestResolveEndpointValidatesRetriesBeforeLiteralAddress(t *testing.T) {
	t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", "invalid")
	for _, endpoint := range []string{
		"",
		"192.0.2.1:51820",
		"[2001:db8::1]:51820",
		"[fe80::1%eth0]:51820",
	} {
		t.Run(endpoint, func(t *testing.T) {
			_, err := resolveEndpoint(endpoint)
			if err == nil || !strings.Contains(err.Error(), "unable to parse WG_ENDPOINT_RESOLUTION_RETRIES") {
				t.Fatalf("resolveEndpoint(%q) error = %v, want invalid retry environment error", endpoint, err)
			}
		})
	}
}

func TestResolveEndpointBackoffCapsAtTwentySeconds(t *testing.T) {
	oldLookup, oldSleep := lookupEndpointIPs, sleepEndpointRetry
	t.Cleanup(func() { lookupEndpointIPs, sleepEndpointRetry = oldLookup, oldSleep })
	t.Setenv("WG_ENDPOINT_RESOLUTION_RETRIES", "20")
	var sleeps []time.Duration
	lookupEndpointIPs = func(context.Context, string, string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "temporary", IsTemporary: true}
	}
	sleepEndpointRetry = func(d time.Duration) { sleeps = append(sleeps, d) }
	if _, err := resolveEndpoint("example.com:3"); err == nil {
		t.Fatal("expected retry exhaustion")
	}
	want := []time.Duration{time.Second, 1200 * time.Millisecond, 1440 * time.Millisecond, 1728 * time.Millisecond}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Fatalf("sleep %d = %v, want %v", i, sleeps[i], want[i])
		}
	}
	if sleeps[len(sleeps)-1] != 20*time.Second {
		t.Fatalf("last sleep = %v, want 20s; all=%v", sleeps[len(sleeps)-1], sleeps)
	}
}
