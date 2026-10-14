//go:build desktop

package rtcbench

import (
	"syscall"
	"unsafe"
)

func init() {
	detachConsole = func() {
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		var processes [2]uint32
		attached, _, _ := kernel32.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&processes[0])), 2)
		if attached == 1 {
			kernel32.NewProc("FreeConsole").Call()
		}
	}
}
