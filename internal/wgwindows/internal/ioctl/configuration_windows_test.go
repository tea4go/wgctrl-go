package ioctl

import (
	"testing"
	"unsafe"
)

func TestAllowedIPLayout(t *testing.T) {
	var allowedIP AllowedIP

	if got, want := unsafe.Sizeof(allowedIP), uintptr(24); got != want {
		t.Fatalf("unexpected AllowedIP size: got %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(allowedIP.Address), uintptr(0); got != want {
		t.Fatalf("unexpected Address offset: got %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(allowedIP.AddressFamily), uintptr(16); got != want {
		t.Fatalf("unexpected AddressFamily offset: got %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(allowedIP.Cidr), uintptr(18); got != want {
		t.Fatalf("unexpected Cidr offset: got %d, want %d", got, want)
	}
	if got, want := unsafe.Offsetof(allowedIP.Flags), uintptr(20); got != want {
		t.Fatalf("unexpected Flags offset: got %d, want %d", got, want)
	}
	if got, want := AllowedIPRemove, AllowedIPFlag(1<<0); got != want {
		t.Fatalf("unexpected remove flag: got %d, want %d", got, want)
	}
}
