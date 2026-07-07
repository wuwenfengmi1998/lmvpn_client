//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"

	"lmvpn/internal/log"
)

const daemonBinaryName = "lmvpnd.exe"

// launchElevated launches the daemon-launch subcommand with UAC
// elevation via ShellExecute "runas" on Windows.
func launchElevated(exe, daemonBin, home string, uid, gid int) error {
	args := fmt.Sprintf("daemon-launch --user-home \"%s\" --uid %d --gid %d --daemon-bin \"%s\"",
		home, uid, gid, daemonBin)

	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	params, _ := syscall.UTF16PtrFromString(args)
	dir, _ := syscall.UTF16PtrFromString("")

	ret, _, err := proc.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		1, // SW_SHOWNORMAL
	)

	if ret <= 32 {
		return fmt.Errorf("UAC elevation failed (code %d): %w", ret, err)
	}
	log.L().Info("daemon launched via UAC",
		"uid", uid, "gid", gid, "home", home, "daemon_bin", daemonBin)
	return nil
}
