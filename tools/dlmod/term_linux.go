//go:build linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func ioctl(fd, request uintptr, argument unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(argument)); errno != 0 {
		return errno
	}
	return nil
}

// enterRaw delivers key presses as they happen, without echo or line editing.
func enterRaw() (func(), error) {
	fd := os.Stdin.Fd()
	var saved syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&saved)); err != nil {
		return nil, err
	}
	raw := saved
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR |
		syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag = raw.Cflag&^(syscall.CSIZE|syscall.PARENB) | syscall.CS8
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&raw)); err != nil {
		return nil, err
	}
	return func() { ioctl(fd, syscall.TCSETS, unsafe.Pointer(&saved)) }, nil
}

func terminalSize() (int, int) {
	var size struct{ rows, columns, x, y uint16 }
	if ioctl(os.Stdout.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&size)) != nil || size.columns == 0 {
		return 80, 24
	}
	return int(size.columns), int(size.rows)
}

func colorSupported() bool {
	var state syscall.Termios
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" &&
		ioctl(os.Stdout.Fd(), syscall.TCGETS, unsafe.Pointer(&state)) == nil
}

// deadlockRunning looks for Proton's deadlock.exe among the process names in /proc.
func deadlockRunning() bool {
	names, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, name := range names {
		if data, err := os.ReadFile(name); err == nil && bytes.EqualFold(bytes.TrimSpace(data), []byte("deadlock.exe")) {
			return true
		}
	}
	return false
}

func registryValue(root, path, name string) string { return "" }
