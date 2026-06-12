//go:build windows

package activity

import (
	"context"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                         = windows.NewLazySystemDLL("user32.dll")
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procGetLastInputInfo           = user32.NewProc("GetLastInputInfo")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	procGetTickCount               = kernel32.NewProc("GetTickCount")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

type windowsProbe struct{}

func NewProbe() Probe {
	return windowsProbe{}
}

func (windowsProbe) Snapshot(context.Context) (Snapshot, error) {
	idleFor, err := idleDuration()
	if err != nil {
		return Snapshot{}, err
	}
	process, err := foregroundProcessName()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		IdleFor:           idleFor,
		ForegroundProcess: process,
		At:                time.Now().UTC(),
	}, nil
}

func idleDuration() (time.Duration, error) {
	info := lastInputInfo{cbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	ret, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0, err
	}
	now, _, _ := procGetTickCount.Call()
	elapsed := uint32(now) - info.dwTime
	return time.Duration(elapsed) * time.Millisecond, nil
}

func foregroundProcessName() (string, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", nil
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return "", nil
	}
	return processName(pid)
}

func processName(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", nil
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	ret, _, err := procQueryFullProcessImageNameW.Call(
		uintptr(handle),
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		return "", err
	}
	fullPath := windows.UTF16ToString(buf[:size])
	if fullPath == "" {
		return "", nil
	}
	return strings.ToLower(filepath.Base(fullPath)), nil
}
