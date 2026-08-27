//go:build !linux

package main

import (
	"fmt"
	"io"
)

func syncgitee(_ []string, _ io.Reader, _ io.Writer, errOut io.Writer) int {
	fmt.Fprintln(errOut, "错误: wg syncgitee 命令仅支持 Linux")
	return 1
}
