//go:build windows

package wgmeta

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type windowsFileLock struct {
	file *os.File
}

func lockFile(path string) (fileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		file.Close()
		return nil, err
	}
	return &windowsFileLock{file: file}, nil
}

func (l *windowsFileLock) Close() error {
	var overlapped windows.Overlapped
	if err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &overlapped); err != nil {
		l.file.Close()
		return err
	}
	return l.file.Close()
}
