package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeBuildInfo(t *testing.T) {
	oldVersion, oldBuildTime := version, BuildTime
	t.Cleanup(func() {
		version = oldVersion
		BuildTime = oldBuildTime
	})

	version = "v4.1.0"
	BuildTime = ""

	gotVersion, gotBuildTime, gotPlatform := runtimeBuildInfo()
	if !strings.HasPrefix(gotVersion, "wgctrl-rest ") || !strings.HasSuffix(gotVersion, " v4.1.0") {
		t.Fatalf("version=%q (want: prefix \"wgctrl-rest \", suffix \" v4.1.0\")", gotVersion)
	}
	if gotBuildTime != "unknown" {
		t.Fatalf("buildTime=%q", gotBuildTime)
	}
	if want := runtime.GOOS + "-" + runtime.GOARCH; gotPlatform != want {
		t.Fatalf("platform=%q want=%q", gotPlatform, want)
	}
}
