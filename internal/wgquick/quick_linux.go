//go:build linux

package wgquick

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl"
)

// Up 创建并配置接口：执行 PreUp 钩子、创建接口、应用加密配置、
// 配置地址 / MTU / DNS / 路由（含默认路由隔离），最后执行 PostUp 钩子。
//
// 加密配置通过 wgctrl 库应用（内核与用户态后端通吃）；接口建删、地址、
// 路由、DNS 与钩子则通过 ip / resolvconf / sh 完成（与上游 wg-quick 一致）。
func Up(c *Config) (err error) {
	if c.Name == "" {
		return fmt.Errorf("wgquick: interface name is empty")
	}

	if err := runHooks(c.PreUp); err != nil {
		return fmt.Errorf("PreUp: %w", err)
	}
	if err := addInterface(c.Name); err != nil {
		return err
	}
	created := true
	defer func() {
		if err != nil && created {
			_ = delInterface(c.Name)
		}
	}()

	client, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer client.Close()

	wgc := c.WireGuardConfig()
	if c.routeMode() == "auto" && c.hasDefaultRoute() {
		mark := c.routeTable()
		wgc.FirewallMark = &mark
	}
	if err := client.ConfigureDevice(c.Name, wgc); err != nil {
		return fmt.Errorf("configure device %s: %w", c.Name, err)
	}

	for _, a := range c.Addresses {
		if err := addAddress(c.Name, a); err != nil {
			return err
		}
	}
	// 路由需要接口处于 UP 状态；与上游 wg-quick 的 set_mtu_up 一致，此步无条件 up。
	if c.MTU > 0 {
		if err := setMTUUp(c.Name, c.MTU); err != nil {
			return err
		}
	} else {
		if err := setLinkUp(c.Name); err != nil {
			return err
		}
	}
	if len(c.DNS) > 0 {
		setDNS(c.Name, c.DNS)
	}

	var addedRules []string
	defer func() {
		if err != nil && created {
			for _, fam := range addedRules {
				delDefaultRules(fam, c.routeTable())
			}
		}
	}()

	for _, p := range c.Peers {
		for _, ipn := range p.AllowedIPs {
			switch c.routeMode() {
			case "off":
				// 用户明确要求不添加路由。
				continue
			case "explicit":
				// 所有路由进显式表，规则由用户自行管理。
				if err := addRouteTable(ipn, c.Name, c.routeTable()); err != nil {
					return err
				}
				continue
			}
			// auto：默认路由隔离，普通路由进主表。
			if isDefaultRoute(ipn) {
				fam := ipFamily(ipn)
				if err := addRouteTable(ipn, c.Name, c.routeTable()); err != nil {
					return err
				}
				if err := addDefaultRules(fam, c.routeTable()); err != nil {
					return err
				}
				addedRules = append(addedRules, fam)
			} else {
				if err := addRoute(ipn, c.Name); err != nil {
					return err
				}
			}
		}
	}

	return runHooks(c.PostUp)
}

// Down 拆除接口：执行 PreDown 钩子、移除默认路由隔离规则、删除接口
// （地址与路由随接口自动移除）、清理 DNS，最后执行 PostDown 钩子。
func Down(c *Config) error {
	if c.Name == "" {
		return fmt.Errorf("wgquick: interface name is empty")
	}

	if err := runHooks(c.PreDown); err != nil {
		return fmt.Errorf("PreDown: %w", err)
	}

	// policy 路由规则不会随接口删除而自动清理；仅在 auto 模式且存在默认路由时
	// 才可能由 Up 添加过规则，因此也只在此条件下清理。
	if c.routeMode() == "auto" && c.hasDefaultRoute() {
		has4, has6 := false, false
		for _, p := range c.Peers {
			for _, ipn := range p.AllowedIPs {
				if !isDefaultRoute(ipn) {
					continue
				}
				if ipn.IP.To4() != nil {
					has4 = true
				} else {
					has6 = true
				}
			}
		}
		table := c.routeTable()
		if has4 {
			delDefaultRules("-4", table)
		}
		if has6 {
			delDefaultRules("-6", table)
		}
	}

	if interfaceExists(c.Name) {
		if err := delInterface(c.Name); err != nil {
			return err
		}
	}
	if len(c.DNS) > 0 {
		unsetDNS(c.Name)
	}

	return runHooks(c.PostDown)
}

// run 执行外部命令，失败时返回带 stderr 的错误。
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runHooks(hooks []string) error {
	for _, h := range hooks {
		if err := run("sh", "-c", h); err != nil {
			return fmt.Errorf("hook %q: %w", h, err)
		}
	}
	return nil
}

func interfaceExists(name string) bool {
	return exec.Command("ip", "link", "show", "dev", name).Run() == nil
}

func addInterface(name string) error {
	return run("ip", "link", "add", "dev", name, "type", "wireguard")
}

func delInterface(name string) error {
	return run("ip", "link", "del", "dev", name)
}

func addAddress(name string, ipn net.IPNet) error {
	return run("ip", "address", "add", ipn.String(), "dev", name)
}

func setMTUUp(name string, mtu int) error {
	return run("ip", "link", "set", "dev", name, "mtu", strconv.Itoa(mtu), "up")
}

func setLinkUp(name string) error {
	return run("ip", "link", "set", "dev", name, "up")
}

func ipFamily(ipn net.IPNet) string {
	if ipn.IP.To4() != nil {
		return "-4"
	}
	return "-6"
}

func addRoute(ipn net.IPNet, name string) error {
	fam := ipFamily(ipn)
	// 接口地址可能已产生等价的直连路由（如 Address 前缀覆盖该网段），
	// 与上游 wg-quick 一致：已存在则跳过。
	if routeExists(fam, ipn, name) {
		return nil
	}
	return run("ip", fam, "route", "add", ipn.String(), "dev", name)
}

func routeExists(fam string, ipn net.IPNet, name string) bool {
	out, err := exec.Command("ip", fam, "route", "show", "dev", name, "match", ipn.String()).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func addRouteTable(ipn net.IPNet, name string, table int) error {
	return run("ip", ipFamily(ipn), "route", "add", ipn.String(), "dev", name, "table", strconv.Itoa(table))
}

// addDefaultRules 添加默认路由隔离所需的 policy 路由规则。
func addDefaultRules(family string, table int) error {
	t := strconv.Itoa(table)
	if err := run("ip", family, "rule", "add", "not", "fwmark", t, "table", t); err != nil {
		return err
	}
	return run("ip", family, "rule", "add", "table", "main", "suppress_prefixlength", "0")
}

// delDefaultRules 移除默认路由隔离规则（best-effort）。
func delDefaultRules(family string, table int) {
	t := strconv.Itoa(table)
	_ = run("ip", family, "rule", "del", "not", "fwmark", t, "table", t)
	_ = run("ip", family, "rule", "del", "table", "main", "suppress_prefixlength", "0")
}

func setDNS(name string, dns []net.IP) {
	if _, err := exec.LookPath("resolvconf"); err != nil {
		fmt.Fprintf(os.Stderr, "wgquick: resolvconf 不可用，跳过 DNS 配置\n")
		return
	}
	var b strings.Builder
	for _, ip := range dns {
		fmt.Fprintf(&b, "nameserver %s\n", ip.String())
	}
	cmd := exec.Command("resolvconf", "-a", "tun."+name, "-m", "0", "-x")
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "wgquick: DNS 配置失败: %v: %s\n", err, strings.TrimSpace(string(out)))
	}
}

func unsetDNS(name string) {
	if _, err := exec.LookPath("resolvconf"); err != nil {
		return
	}
	if err := run("resolvconf", "-d", "tun."+name); err != nil {
		fmt.Fprintf(os.Stderr, "wgquick: 清理 DNS 失败: %v\n", err)
	}
}
