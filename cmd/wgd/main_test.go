package main

import (
	"runtime"
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
	if gotVersion != "wgctrl-go wgd v4.1.0" {
		t.Fatalf("version=%q", gotVersion)
	}
	if gotBuildTime != "unknown" {
		t.Fatalf("buildTime=%q", gotBuildTime)
	}
	if want := runtime.GOOS + "-" + runtime.GOARCH; gotPlatform != want {
		t.Fatalf("platform=%q want=%q", gotPlatform, want)
	}
}
