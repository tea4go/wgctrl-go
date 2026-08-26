package main

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestExecuteVersion(t *testing.T) {
	for _, arg := range []string{"version"} {
		t.Run(arg, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := execute([]string{arg}, strings.NewReader(""), &out, &errOut)
			if code != 0 {
				t.Fatalf("unexpected exit code: %d", code)
			}
			if want := versionText(); out.String() != want {
				t.Fatalf("unexpected stdout: %q", out.String())
			}
			if errOut.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", errOut.String())
			}
		})
	}
}

func TestVersionTextIncludesBuildMetadata(t *testing.T) {
	oldVersion, oldBuildTime := version, BuildTime
	t.Cleanup(func() {
		version = oldVersion
		BuildTime = oldBuildTime
	})

	version = "v4.1.0"
	BuildTime = "2026-08-26(21:00:00)"

	got := versionText()
	for _, want := range []string{
		"wireguard-tools v4.1.0",
		"Build Time : 2026-08-26(21:00:00)",
		"Platform   : " + runtime.GOOS + "-" + runtime.GOARCH,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("versionText()=%q, missing %q", got, want)
		}
	}
}

func TestVersionTextUsesUnknownBuildTime(t *testing.T) {
	oldBuildTime := BuildTime
	t.Cleanup(func() { BuildTime = oldBuildTime })
	BuildTime = ""

	if got := versionText(); !strings.Contains(got, "Build Time : unknown") {
		t.Fatalf("versionText()=%q", got)
	}
}

func TestExecuteHelp(t *testing.T) {
	for _, arg := range []string{"help"} {
		t.Run(arg, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := execute([]string{arg}, strings.NewReader(""), &out, &errOut)
			if code != 0 {
				t.Fatalf("unexpected exit code: %d", code)
			}
			if want := usage; out.String() != want {
				t.Fatalf("unexpected stdout:\n%s", out.String())
			}
			if errOut.Len() != 0 {
				t.Fatalf("unexpected stderr: %q", errOut.String())
			}
		})
	}
}

func TestExecuteInvalidSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := execute([]string{"invalid"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
	if want := "无效的子命令: `invalid'\n" + usage; errOut.String() != want {
		t.Fatalf("unexpected stderr:\n%s", errOut.String())
	}
}

func TestExecuteNoArgumentsCallsShow(t *testing.T) {
	oldShow := showCommand
	t.Cleanup(func() { showCommand = oldShow })

	called := false
	showCommand = func(args []string, in io.Reader, out, errOut io.Writer) int {
		called = true
		if len(args) != 0 {
			t.Fatalf("unexpected arguments: %q", args)
		}
		return 7
	}

	var out, errOut bytes.Buffer
	code := execute(nil, strings.NewReader(""), &out, &errOut)
	if code != 7 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if !called {
		t.Fatal("show handler was not called")
	}
}
