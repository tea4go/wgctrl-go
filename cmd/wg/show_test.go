package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type fakeShowClient struct {
	devices []*wgtypes.Device
	device  *wgtypes.Device
	err     error
}

func (c *fakeShowClient) Devices() ([]*wgtypes.Device, error)    { return c.devices, c.err }
func (c *fakeShowClient) Device(string) (*wgtypes.Device, error) { return c.device, c.err }
func (c *fakeShowClient) Close() error                           { return nil }

func TestShowInterfaces(t *testing.T) {
	old := newShowClient
	t.Cleanup(func() { newShowClient = old })
	newShowClient = func() (showClient, error) {
		return &fakeShowClient{devices: []*wgtypes.Device{{Name: "wg0"}, {Name: "wg1"}}}, nil
	}
	var out, errOut bytes.Buffer
	if code := show([]string{"interfaces"}, strings.NewReader(""), &out, &errOut); code != 0 || out.String() != "wg0 wg1\n" || errOut.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestShowDevice(t *testing.T) {
	old := newShowClient
	t.Cleanup(func() { newShowClient = old })
	newShowClient = func() (showClient, error) { return &fakeShowClient{device: &wgtypes.Device{Name: "wg0"}}, nil }
	var out, errOut bytes.Buffer
	if code := show([]string{"wg0"}, strings.NewReader(""), &out, &errOut); code != 0 || !strings.Contains(out.String(), "interface: wg0") || errOut.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestShowListError(t *testing.T) {
	old := newShowClient
	t.Cleanup(func() { newShowClient = old })
	newShowClient = func() (showClient, error) { return &fakeShowClient{err: errors.New("boom")}, nil }
	var out, errOut bytes.Buffer
	if code := show(nil, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestShowColorModes(t *testing.T) {
	old := newShowClient
	t.Cleanup(func() { newShowClient = old })
	newShowClient = func() (showClient, error) { return &fakeShowClient{device: &wgtypes.Device{Name: "wg0"}}, nil }

	for _, test := range []struct {
		name      string
		mode      string
		wantColor bool
	}{
		{"always", "always", true},
		{"never", "never", false},
		{"redirected default", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("WG_COLOR_MODE", test.mode)
			var out, errOut bytes.Buffer
			if code := show([]string{"wg0"}, strings.NewReader(""), &out, &errOut); code != 0 {
				t.Fatalf("code=%d err=%q", code, errOut.String())
			}
			if got := strings.Contains(out.String(), "\x1b["); got != test.wantColor {
				t.Fatalf("contains ANSI = %v, want %v: %q", got, test.wantColor, out.String())
			}
		})
	}
}

func TestShowFieldsNeverUseColor(t *testing.T) {
	old := newShowClient
	t.Cleanup(func() { newShowClient = old })
	newShowClient = func() (showClient, error) { return &fakeShowClient{device: &wgtypes.Device{Name: "wg0"}}, nil }
	t.Setenv("WG_COLOR_MODE", "always")

	for _, field := range []string{"public-key", "dump"} {
		var out, errOut bytes.Buffer
		if code := show([]string{"wg0", field}, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("field=%s code=%d err=%q", field, code, errOut.String())
		}
		if strings.Contains(out.String(), "\x1b[") {
			t.Fatalf("field %s contains ANSI: %q", field, out.String())
		}
	}
}

func TestShowRejectsInvalidField(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := show([]string{"wg0", "unknown"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unknown") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestShowLogsWhenNoInterfacesFound(t *testing.T) {
	oldClient := newShowClient
	oldNotice := showNoticeLog
	t.Cleanup(func() {
		newShowClient = oldClient
		showNoticeLog = oldNotice
	})
	newShowClient = func() (showClient, error) {
		return &fakeShowClient{devices: []*wgtypes.Device{}}, nil
	}

	var notices []string
	showNoticeLog = func(format string, v ...interface{}) {
		notices = append(notices, format)
	}

	for _, args := range [][]string{nil, {"interfaces"}} {
		var out, errOut bytes.Buffer
		if code := show(args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("args=%v code=%d err=%q", args, code, errOut.String())
		}
		if out.Len() != 0 {
			t.Fatalf("args=%v unexpected stdout: %q", args, out.String())
		}
	}

	if len(notices) != 2 {
		t.Fatalf("notices=%v", notices)
	}
	if notices[0] != "wg show 未发现任何 WireGuard 接口，输出为空" {
		t.Fatalf("unexpected notice[0]=%q", notices[0])
	}
	if notices[1] != "wg show interfaces 未发现任何 WireGuard 接口" {
		t.Fatalf("unexpected notice[1]=%q", notices[1])
	}
}
