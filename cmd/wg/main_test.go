package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	flag "github.com/spf13/pflag"
)

func TestRunNoArgumentsConfiguresLoggingThenExecutes(t *testing.T) {
	oldExecute := runExecute
	oldConfigure := configureMainLogging
	t.Cleanup(func() { runExecute = oldExecute })
	t.Cleanup(func() { configureMainLogging = oldConfigure })

	configured := false
	runExecute = func(args []string, in io.Reader, out, errOut io.Writer) int {
		if len(args) != 0 {
			t.Fatalf("unexpected args: %q", args)
		}
		return 7
	}
	configureMainLogging = func() {
		configured = true
	}

	var out, errOut bytes.Buffer
	code := run(nil, strings.NewReader(""), &out, &errOut)
	if code != 7 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if !configured {
		t.Fatal("logging was not configured")
	}
}

func TestRunUsesLog4goLogLevelFlag(t *testing.T) {
	oldExecute := runExecute
	oldConfigure := configureMainLogging
	t.Cleanup(func() { runExecute = oldExecute })
	t.Cleanup(func() { configureMainLogging = oldConfigure })

	runExecute = func(args []string, in io.Reader, out, errOut io.Writer) int {
		if strings.Join(args, ",") != "show,interfaces" {
			t.Fatalf("unexpected args: %q", args)
		}
		if got := flag.Lookup("log_level").Value.String(); got != "7" {
			t.Fatalf("log_level=%q", got)
		}
		return 0
	}
	configureMainLogging = func() {}

	var out, errOut bytes.Buffer
	code := run([]string{"--log_level=7", "show", "interfaces"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d err=%q", code, errOut.String())
	}
}

func TestRunDoesNotDefineCustomLogLevelFlag(t *testing.T) {
	oldExecute := runExecute
	oldConfigure := configureMainLogging
	t.Cleanup(func() { runExecute = oldExecute })
	t.Cleanup(func() { configureMainLogging = oldConfigure })

	runExecute = func(args []string, in io.Reader, out, errOut io.Writer) int {
		t.Fatalf("unexpected execute call with %q", args)
		return 1
	}
	configureMainLogging = func() {
		t.Fatal("unexpected logging initialization")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--log-level=7", "show"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown flag") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestRunUsesEnvLogLevelThroughLog4go(t *testing.T) {
	oldExecute := runExecute
	oldConfigure := configureMainLogging
	t.Cleanup(func() { runExecute = oldExecute })
	t.Cleanup(func() { configureMainLogging = oldConfigure })
	t.Setenv("log_level", "6")

	runExecute = func(args []string, in io.Reader, out, errOut io.Writer) int {
		if strings.Join(args, ",") != "show" {
			t.Fatalf("unexpected args: %q", args)
		}
		return 0
	}
	configureMainLogging = func() {}

	var out, errOut bytes.Buffer
	code := run([]string{"show"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d err=%q", code, errOut.String())
	}
}

func TestRunShowsVersionFromMainFlags(t *testing.T) {
	oldExecute := runExecute
	oldConfigure := configureMainLogging
	t.Cleanup(func() { runExecute = oldExecute })
	t.Cleanup(func() { configureMainLogging = oldConfigure })

	runExecute = func(args []string, in io.Reader, out, errOut io.Writer) int {
		t.Fatalf("unexpected execute call with %q", args)
		return 0
	}
	configureMainLogging = func() {
		t.Fatal("unexpected logging initialization")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"-v"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	if out.String() != version {
		t.Fatalf("unexpected stdout: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}
