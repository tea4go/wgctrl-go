package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	flag "github.com/spf13/pflag"
	logs "github.com/tea4go/gh/log4go"
)

const commandAppName = "wg"

var (
	runExecute = execute
	pHelp      = flag.BoolP("help", "h", false, "显示帮助")
	pVersion   = flag.BoolP("version", "v", false, "显示版本")
)

type mainOptions struct {
	args        []string
	showHelp    bool
	showVersion bool
}

var configureMainLogging = func() {
	logs.SetConsole2Stderr(true)
	logs.SetLogFuncCallDepth(5)

	logName := os.Getenv("log_name")
	if logName == "" {
		logName = commandAppName
	}
	logFile := filepath.ToSlash(filepath.Join(os.TempDir(), "ulog_"+logName+".txt"))

	_ = logs.SetLogger("file", `{"filename":"`+logFile+`","perm":"0666","level":5}`)
	logs.StartLogger()
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	opts, err := parseMainArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "%v\n", err)
		_, _ = io.WriteString(errOut, usage)
		return 1
	}

	if opts.showVersion {
		_, _ = io.WriteString(out, version)
		return 0
	}
	if opts.showHelp {
		_, _ = io.WriteString(out, usage)
		return 0
	}

	configureMainLogging()
	return runExecute(opts.args, in, out, errOut)
}

func parseMainArgs(args []string) (mainOptions, error) {
	resetMainFlags()
	flag.CommandLine.Init(commandAppName, flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	flag.CommandLine.SetInterspersed(false)
	if err := flag.CommandLine.Parse(args); err != nil {
		return mainOptions{}, formatMainFlagError(err)
	}

	return mainOptions{
		args:        flag.Args(),
		showHelp:    *pHelp,
		showVersion: *pVersion,
	}, nil
}

func resetMainFlags() {
	defaults := map[string]string{
		"help":      "false",
		"version":   "false",
		"log_level": "5",
		"log_name":  "",
		"log_short": "false",
	}
	for name, value := range defaults {
		if f := flag.Lookup(name); f != nil {
			_ = f.Value.Set(value)
			f.Changed = false
		}
	}
}

func formatMainFlagError(err error) error {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return err
	}
	if strings.Contains(msg, "unknown shorthand flag") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "unknown long flag") {
		return fmt.Errorf("未知的全局参数: %s", msg)
	}
	if strings.Contains(msg, "needs an argument") || strings.Contains(msg, "requires an argument") {
		return fmt.Errorf("缺少日志级别值: -l")
	}
	return fmt.Errorf("%s", msg)
}
