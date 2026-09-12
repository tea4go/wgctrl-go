package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/pflag"
	logs "github.com/tea4go/gh/log4go"
	"github.com/tea4go/gh/network"
	"github.com/tea4go/gh/utils"
)

var (
	appName      = "wg"
	appVer       = "v0.0.2"
	IsBeta       string
	BuildTime    string
	pGiteeToken  = pflag.String("gitee_token", "", "Gitee 访问令牌，也可通过同名环境变量设置")
	pGiteeGistID = pflag.String("gitee_gist_id", "", "Gitee 代码片段 ID，也可通过同名环境变量设置")
	pList        = pflag.Bool("list", false, "显示接口简介（interface 与 peer 摘要）")
)

// runExecute 是 run 在完成全局参数解析与日志配置后调用的命令分发函数，
// 默认指向 execute，测试中可注入替代实现。
var runExecute = execute

// configureMainLogging 配置并启动日志记录器，测试中可注入替代实现。
var configureMainLogging = func() {
	logName := os.Getenv("log_name")
	if logName == "" {
		logName = appName
	}
	logsFileName := filepathJoin(os.TempDir(), "ulog_"+logName+".txt")
	_ = logs.SetLogger("file", `{"filename":"`+logsFileName+`", "perm": "0666","level":5}`)
	logs.StartLogger()
}

// startSelfUpdate 触发自更新框架，处理 --upgrade/--restart/--publish 等运维 flag。
var startSelfUpdate = func() {
	network.StartSelfUpdate("http://wc192.yj2025.icu:8118", "http://nj.yj2025.icu:23432", "http://wc8.yj2025.icu:8118", "http://wc47.yj2025.icu:23431")
}

func filepathJoin(elem ...string) string {
	path := filepath.Join(elem...)
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(path, "\\", "/")
	}
	return path
}

func main() {
	utils.LoadDotEnv()
	pflag.CommandLine.MarkHidden("daemon")

	network.SetAppVersion(appName, appVer, IsBeta, BuildTime)

	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run 是可测试的命令入口：解析全局参数、处理帮助/版本，然后配置日志并分发命令。
func run(args []string, in io.Reader, out, errOut io.Writer) int {
	opts, err := parseMainArgs(args)
	if err != nil {
		fmt.Fprintf(errOut, "%v\n", err)
		_, _ = io.WriteString(errOut, usage)
		return 1
	}

	if opts.showVersion {
		_, _ = io.WriteString(out, versionText())
		return 0
	}
	if opts.showHelp {
		fmt.Fprintln(out, "用法: wg <命令> [<参数>]")
		fmt.Fprint(out, pflag.CommandLine.FlagUsages())
		fmt.Fprintln(out)
		fmt.Fprint(out, usage)
		return 0
	}

	// 自更新框架的运维 flag（--upgrade/--restart/--publish 等）在参数解析后处理；
	// 其内部的 -v/--help 处理已被上面的 showVersion/showHelp 提前拦截，不会重复触发。
	startSelfUpdate()

	syncGiteeToken = logs.GetParamString("gitee_token", *pGiteeToken, "")
	syncGiteeGistID = logs.GetParamString("gitee_gist_id", *pGiteeGistID, "")

	configureMainLogging()

	if *pList {
		return list(opts.args, in, out, errOut)
	}
	return runExecute(opts.args, in, out, errOut)
}

type mainOptions struct {
	args        []string
	showHelp    bool
	showVersion bool
}

func parseMainArgs(args []string) (mainOptions, error) {
	resetMainFlags()
	pflag.CommandLine.Init(appName, pflag.ContinueOnError)
	pflag.CommandLine.SetOutput(io.Discard)
	pflag.CommandLine.SetInterspersed(false)
	if err := pflag.CommandLine.Parse(args); err != nil {
		return mainOptions{}, formatMainFlagError(err)
	}

	showVersion, _ := pflag.CommandLine.GetBool("version")
	showHelp, _ := pflag.CommandLine.GetBool("help")

	return mainOptions{
		args:        pflag.Args(),
		showHelp:    showHelp,
		showVersion: showVersion,
	}, nil
}

// resetMainFlags 将全局 flag 重置为默认值，避免多次调用 run 时状态残留。
func resetMainFlags() {
	defaults := map[string]string{
		"help":          "false",
		"version":       "false",
		"log_level":     "5",
		"log_name":      "",
		"log_short":     "false",
		"gitee_token":   "",
		"gitee_gist_id": "",
		"list":          "false",
	}
	for name, value := range defaults {
		if f := pflag.Lookup(name); f != nil {
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
