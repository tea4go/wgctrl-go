package main

import (
	"fmt"
	"io"
)

const (
	version = "wireguard-tools v1.0.20260223 - https://git.zx2c4.com/wireguard-tools/\n"
	usage   = "Usage: wg <cmd> [<args>]\n\n" +
		"Available subcommands:\n" +
		"  show: Shows the current configuration and device information\n" +
		"  showconf: Shows the current configuration of a given WireGuard interface, for use with `setconf'\n" +
		"  set: Change the current configuration, add peers, remove peers, or change peers\n" +
		"  setconf: Applies a configuration file to a WireGuard interface\n" +
		"  addconf: Appends a configuration file to a WireGuard interface\n" +
		"  syncconf: Synchronizes a configuration file to a WireGuard interface\n" +
		"  genkey: Generates a new private key and writes it to stdout\n" +
		"  genpsk: Generates a new preshared key and writes it to stdout\n" +
		"  pubkey: Reads a private key from stdin and writes a public key to stdout\n" +
		"You may pass `--help' to any of these subcommands to view usage.\n"
)

type commandFunc func(args []string, in io.Reader, out, errOut io.Writer) int

var showCommand commandFunc = unimplementedCommand

var commands = map[string]commandFunc{
	"show": func(args []string, in io.Reader, out, errOut io.Writer) int {
		return showCommand(args, in, out, errOut)
	},
	"showconf": unimplementedCommand,
	"set":      unimplementedCommand,
	"setconf":  unimplementedCommand,
	"addconf":  unimplementedCommand,
	"syncconf": unimplementedCommand,
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
		case "version", "-v", "--version":
			_, _ = io.WriteString(out, version)
			return 0
		case "help", "-h", "--help":
			_, _ = io.WriteString(out, usage)
			return 0
		}
	}

	command, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(errOut, "Invalid subcommand: `%s'\n", args[0])
		_, _ = io.WriteString(errOut, usage)
		return 1
	}

	return command(args[1:], in, out, errOut)
}

func unimplementedCommand(_ []string, _ io.Reader, _ io.Writer, _ io.Writer) int {
	return 1
}
