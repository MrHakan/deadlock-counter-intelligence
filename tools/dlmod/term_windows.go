//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleOutputCP         = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP         = kernel32.NewProc("SetConsoleOutputCP")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

const (
	enableProcessedInput            = 0x0001
	enableLineInput                 = 0x0002
	enableEchoInput                 = 0x0004
	enableVirtualTerminalInput      = 0x0200
	enableProcessedOutput           = 0x0001
	enableVirtualTerminalProcessing = 0x0004
	utf8CodePage                    = 65001
)

func setConsoleMode(handle syscall.Handle, mode uint32) error {
	if ok, _, err := procSetConsoleMode.Call(uintptr(handle), uintptr(mode)); ok == 0 {
		return err
	}
	return nil
}

// enableVT turns on escape sequences for the console window and returns the previous mode.
func enableVT() (uint32, error) {
	out := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(out, &mode); err != nil {
		return 0, err
	}
	return mode, setConsoleMode(out, mode|enableProcessedOutput|enableVirtualTerminalProcessing)
}

// enterRaw delivers key presses as they happen (arrow keys as escape sequences) and switches
// the console to UTF-8. Consoles older than Windows 10 refuse, and dlmod uses its plain menu.
func enterRaw() (func(), error) {
	in, out := syscall.Handle(os.Stdin.Fd()), syscall.Handle(os.Stdout.Fd())
	var inMode uint32
	if err := syscall.GetConsoleMode(in, &inMode); err != nil {
		return nil, err
	}
	outMode, err := enableVT()
	if err != nil {
		return nil, err
	}
	raw := inMode&^(enableEchoInput|enableProcessedInput|enableLineInput) | enableVirtualTerminalInput
	if err := setConsoleMode(in, raw); err != nil {
		setConsoleMode(out, outMode)
		return nil, err
	}
	codePage, _, _ := procGetConsoleOutputCP.Call()
	procSetConsoleOutputCP.Call(utf8CodePage)
	return func() {
		setConsoleMode(in, inMode)
		setConsoleMode(out, outMode)
		if codePage != 0 {
			procSetConsoleOutputCP.Call(codePage)
		}
	}, nil
}

type consoleInfo struct {
	size, cursor             [2]int16
	attributes               uint16
	left, top, right, bottom int16
	maximum                  [2]int16
}

func terminalSize() (int, int) {
	var info consoleInfo
	if ok, _, _ := procGetConsoleScreenBufferInfo.Call(os.Stdout.Fd(), uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 80, 25
	}
	return int(info.right-info.left) + 1, int(info.bottom-info.top) + 1
}

// colorSupported enables escape sequences on the console; it fails on consoles older than Windows 10.
func colorSupported() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	_, err := enableVT()
	return err == nil
}

// deadlockRunning walks the process list through the Toolhelp API, which takes about a
// millisecond, instead of starting tasklist.exe.
func deadlockRunning() bool {
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		if exeNameIs(entry.ExeFile[:], "deadlock.exe") {
			return true
		}
	}
	return false
}

// registryValue reads a string value without starting reg.exe.
func registryValue(root, path, name string) string {
	hive := syscall.Handle(syscall.HKEY_CURRENT_USER)
	if root == "HKLM" {
		hive = syscall.HKEY_LOCAL_MACHINE
	}
	subkey, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(hive, subkey, 0, syscall.KEY_READ, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	value, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var kind, size uint32
	if syscall.RegQueryValueEx(key, value, nil, &kind, nil, &size) != nil || size < 2 ||
		(kind != syscall.REG_SZ && kind != syscall.REG_EXPAND_SZ) {
		return ""
	}
	buffer := make([]uint16, size/2+1)
	if syscall.RegQueryValueEx(key, value, nil, &kind, (*byte)(unsafe.Pointer(&buffer[0])), &size) != nil {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}
