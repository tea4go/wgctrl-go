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

func TestShowRejectsInvalidField(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := show([]string{"wg0", "unknown"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "unknown") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
