package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/pflag"
	logs "github.com/tea4go/gh/log4go"
	"github.com/tea4go/gh/network"
)

var (
	appName   = "wg"
	appVer    = "v0.0.2"
	IsBeta    string
	BuildTime string
)

func filepathJoin(elem ...string) string {
	path := filepath.Join(elem...)
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(path, "\\", "/")
	}
	return path
}

func main() {
	// 解析命令行参数
	configFile := pflag.StringP("config", "", "./conf/config.json", "配置文件路径")
	fmt.Println(*configFile)

	pflag.Usage = func() {
		fmt.Println("用法: wg <命令> [<参数>]")
		pflag.PrintDefaults()
		fmt.Printf(usage)
	}
	pflag.CommandLine.MarkHidden("daemon")
	pflag.Parse()

	log_name := os.Getenv("log_name")
	if log_name == "" {
		log_name = appName
	}
	network.SetAppVersion(appName, appVer, IsBeta, BuildTime)
	logsFileName := filepathJoin(os.TempDir(), "ulog_"+log_name+".txt")
	logs.SetLogger("file", `{"filename":"`+logsFileName+`", "perm": "0666","level":5}`)
	logs.StartLogger()
	network.StartSelfUpdate("http://wc192.yj2025.icu:8118", "http://nj.yj2025.icu:23432", "http://wc8.yj2025.icu:8118", "http://wc47.yj2025.icu:23431")

}
