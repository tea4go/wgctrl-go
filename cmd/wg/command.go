package main

import (
	"fmt"
	"io"
	"runtime"
)

const usage = "可用子命令:\n" +
	"  show: 显示当前配置和设备信息\n" +
	"  showconf: 显示指定 WireGuard 接口的当前配置，供 `setconf' 使用\n" +
	"  set: 修改当前配置、添加对等节点、移除对等节点或修改对等节点\n" +
	"  setconf: 将配置文件应用到 WireGuard 接口\n" +
	"  addconf: 向 WireGuard 接口追加配置文件\n" +
	"  syncconf: 将配置文件同步到 WireGuard 接口\n" +
	"  genkey: 生成新的私钥并写入标准输出\n" +
	"  genpsk: 生成新的预共享密钥并写入标准输出\n" +
	"  pubkey: 从标准输入读取私钥并将公钥写入标准输出\n" +
	"你可以向任意子命令传递 `--help' 参数查看用法。\n"

func versionText() string {
	buildTime := BuildTime
	if buildTime == "" {
		buildTime = "unknown"
	}
	return fmt.Sprintf(
		"wireguard-tools %s - https://git.zx2c4.com/wireguard-tools/\n"+
			"Build Time : %s\n"+
			"Platform   : %s-%s\n",
		appVer,
		buildTime,
		runtime.GOOS,
		runtime.GOARCH,
	)
}

type commandFunc func(args []string, in io.Reader, out, errOut io.Writer) int

var showCommand commandFunc = show

var commands = map[string]commandFunc{
	"show": func(args []string, in io.Reader, out, errOut io.Writer) int {
		return showCommand(args, in, out, errOut)
	},
	"showconf": showconf,
	"set":      unimplementedCommand,
	"setconf":  setconf,
	"addconf":  addconf,
	"syncconf": syncconf,
	"genkey":   unimplementedCommand,
	"genpsk":   unimplementedCommand,
	"pubkey":   unimplementedCommand,
}

func execute(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		return showCommand(nil, in, out, errOut)
	}

	if len(args) == 1 {
		switch args[0] {
		case "version":
			_, _ = io.WriteString(out, versionText())
			return 0
		case "help":
			_, _ = io.WriteString(out, usage)
			return 0
		}
	}

	command, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(errOut, "无效的子命令: `%s'\n", args[0])
		_, _ = io.WriteString(errOut, usage)
		return 1
	}

	return command(args[1:], in, out, errOut)
}

func unimplementedCommand(_ []string, _ io.Reader, _ io.Writer, _ io.Writer) int {
	return 1
}
