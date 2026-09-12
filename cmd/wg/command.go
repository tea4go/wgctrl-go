package main

import (
	"fmt"
	"io"
	"runtime"
)

// version 是 wg 工具的版本号，用于 versionText 输出。
var version = "v0.0.2"

// versionText 返回带构建时间与平台信息的版本字符串。
func versionText() string {
	buildTime := BuildTime
	if buildTime == "" {
		buildTime = "unknown"
	}
	return fmt.Sprintf(
		"wireguard-tools %s - https://git.zx2c4.com/wireguard-tools/\n"+
			"Build Time : %s\n"+
			"Platform   : %s-%s\n",
		version,
		buildTime,
		runtime.GOOS,
		runtime.GOARCH,
	)
}

const usage = "可用子命令:\n" +
	"  show: 显示当前配置和设备信息\n" +
	"  showconf: 显示指定 WireGuard 接口的当前配置，供 `setconf' 使用\n" +
	"  set: 修改当前配置、添加对等节点、移除对等节点或修改对等节点（尚未实现）\n" +
	"  setconf: 将配置文件应用到 WireGuard 接口\n" +
	"  addconf: 向 WireGuard 接口追加配置文件\n" +
	"  syncconf: 将配置文件同步到 WireGuard 接口\n" +
	"  syncgitee: 将接口节点配置合并同步到 Gitee 代码片段\n" +
	"  genkey: 生成新的私钥并写入标准输出（尚未实现）\n" +
	"  genpsk: 生成新的预共享密钥并写入标准输出（尚未实现）\n" +
	"  pubkey: 从标准输入读取私钥并将公钥写入标准输出（尚未实现）\n" +
	"常用例子:\n" +
	"  wgc show                     # 显示当前配置和设备信息\n" +
	"  wgc showconf wg0             # 显示 wg0 接口的当前配置，供 `setconf' 使用\n" +
	"  wgc setconf  wg0 wg0.conf    # 将 wg0.conf 配置文件应用到 wg0 接口\n" +
	"  wgc addconf  wg0 wg0.conf    # 向 wg0 接口追加 wg0.conf 配置文件\n" +
	"  wgc syncconf wg0 wg0.conf    # 将 wg0.conf 配置文件同步到 wg0 接口\n" +
	"  wgc --gitee_token token --gitee_gist_id id syncgitee wg0 [文件名] # 创建或合并同步 wg0 节点配置\n"

type commandFunc func(args []string, in io.Reader, out, errOut io.Writer) int

var showCommand commandFunc = show

var commands = map[string]commandFunc{
	"show": func(args []string, in io.Reader, out, errOut io.Writer) int {
		return showCommand(args, in, out, errOut)
	},
	"showconf":  showconf,
	"set":       unimplementedCommand("set"),
	"setconf":   setconf,
	"addconf":   addconf,
	"syncconf":  syncconf,
	"syncgitee": syncgitee,
	"genkey":    unimplementedCommand("genkey"),
	"genpsk":    unimplementedCommand("genpsk"),
	"pubkey":    unimplementedCommand("pubkey"),
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

func unimplementedCommand(name string) commandFunc {
	return func(_ []string, _ io.Reader, _ io.Writer, errOut io.Writer) int {
		fmt.Fprintf(errOut, "错误: wg %s 命令尚未实现\n", name)
		return 1
	}
}
