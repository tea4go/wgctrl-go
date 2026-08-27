package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type showconfTestClient struct {
	devices    []*wgtypes.Device
	devicesErr error
	device     *wgtypes.Device
}

func (c *showconfTestClient) Devices() ([]*wgtypes.Device, error) {
	return c.devices, c.devicesErr
}

func (c *showconfTestClient) Device(string) (*wgtypes.Device, error) {
	if c.device != nil {
		return c.device, nil
	}
	return nil, errors.New("unexpected Device call")
}

func (*showconfTestClient) ConfigureDevice(string, wgtypes.Config) error {
	return errors.New("unexpected ConfigureDevice call")
}

func (*showconfTestClient) Close() error { return nil }

func TestShowconfColorAlwaysUsesANSI(t *testing.T) {
	oldNewConfigClient := newConfigClient
	t.Cleanup(func() { newConfigClient = oldNewConfigClient })
	newConfigClient = func() (configClient, error) {
		return &showconfTestClient{device: &wgtypes.Device{Name: "wg0", ListenPort: 51820}}, nil
	}
	t.Setenv("WG_COLOR_MODE", "always")

	var out, errOut bytes.Buffer
	code := showconf([]string{"wg0"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d, stderr: %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("stdout does not contain ANSI color: %q", out.String())
	}
	plain := stripANSI(out.String())
	if want := "[Interface]\nListenPort = 51820\n\n"; plain != want {
		t.Fatalf("unexpected plain configuration: %q", plain)
	}
}

func TestShowconfColorNeverKeepsPlainConfiguration(t *testing.T) {
	oldNewConfigClient := newConfigClient
	t.Cleanup(func() { newConfigClient = oldNewConfigClient })
	newConfigClient = func() (configClient, error) {
		return &showconfTestClient{device: &wgtypes.Device{Name: "wg0", ListenPort: 51820}}, nil
	}
	t.Setenv("WG_COLOR_MODE", "never")

	var out, errOut bytes.Buffer
	code := showconf([]string{"wg0"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d, stderr: %q", code, errOut.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("stdout contains ANSI color: %q", out.String())
	}
	if want := "[Interface]\nListenPort = 51820\n\n"; out.String() != want {
		t.Fatalf("unexpected configuration: %q", out.String())
	}
}

func stripANSI(s string) string {
	for {
		start := strings.Index(s, "\x1b[")
		if start < 0 {
			return s
		}
		end := strings.IndexByte(s[start:], 'm')
		if end < 0 {
			return s
		}
		s = s[:start] + s[start+end+1:]
	}
}

func TestShowconfNoArgumentsListsInterfaces(t *testing.T) {
	oldNewConfigClient := newConfigClient
	t.Cleanup(func() { newConfigClient = oldNewConfigClient })
	newConfigClient = func() (configClient, error) {
		return &showconfTestClient{devices: []*wgtypes.Device{{Name: "wg0"}, {Name: "wg1"}}}, nil
	}

	var out, errOut bytes.Buffer
	code := showconf(nil, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
	if want := "用法: wg showconf <接口>\n可用接口: wg0 wg1\n"; errOut.String() != want {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestShowconfNoArgumentsReportsNoInterfaces(t *testing.T) {
	oldNewConfigClient := newConfigClient
	t.Cleanup(func() { newConfigClient = oldNewConfigClient })
	newConfigClient = func() (configClient, error) {
		return &showconfTestClient{}, nil
	}

	var out, errOut bytes.Buffer
	code := showconf(nil, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if want := "用法: wg showconf <接口>\n可用接口: 无\n"; errOut.String() != want {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestShowconfNoArgumentsReportsEnumerationError(t *testing.T) {
	oldNewConfigClient := newConfigClient
	t.Cleanup(func() { newConfigClient = oldNewConfigClient })
	newConfigClient = func() (configClient, error) {
		return &showconfTestClient{devicesErr: errors.New("枚举失败")}, nil
	}

	var out, errOut bytes.Buffer
	code := showconf(nil, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if want := "用法: wg showconf <接口>\n无法列出接口: 枚举失败\n"; errOut.String() != want {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestShowconfHelpDoesNotListInterfaces(t *testing.T) {
	oldNewConfigClient := newConfigClient
	t.Cleanup(func() { newConfigClient = oldNewConfigClient })
	called := false
	newConfigClient = func() (configClient, error) {
		called = true
		return &showconfTestClient{}, nil
	}

	var out, errOut bytes.Buffer
	code := showconf([]string{"--help"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if called {
		t.Fatal("newConfigClient was called")
	}
	if want := "用法: wg showconf <接口>\n"; errOut.String() != want {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}
