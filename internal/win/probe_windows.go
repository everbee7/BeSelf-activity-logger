package win

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processQueryLimitedInformation = 0x1000
	desktopSwitchDesktop           = 0x0100
	uoiName                        = 2
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID    = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo      = user32.NewProc("GetLastInputInfo")
	procOpenInputDesktop      = user32.NewProc("OpenInputDesktop")
	procSwitchDesktop         = user32.NewProc("SwitchDesktop")
	procCloseDesktop          = user32.NewProc("CloseDesktop")
	procGetUserObjectInfo     = user32.NewProc("GetUserObjectInformationW")
	procGetTickCount64        = kernel32.NewProc("GetTickCount64")
	procQueryFullProcessImage = kernel32.NewProc("QueryFullProcessImageNameW")
)

type Probe struct{}

type ActiveWindow struct {
	ProcessName    string
	WindowTitle    string
	ExecutablePath string
}

func NewProbe() *Probe {
	return &Probe{}
}

func (p *Probe) ActiveWindow() (ActiveWindow, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ActiveWindow{}, errors.New("no foreground window")
	}

	title := windowText(windows.Handle(hwnd))
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ActiveWindow{WindowTitle: title}, nil
	}
	path := processPath(pid)
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		name = ""
	}
	if name == "" && pid != 0 {
		name = "pid-" + strconvUint(uint64(pid))
	}
	return ActiveWindow{ProcessName: name, WindowTitle: title, ExecutablePath: path}, nil
}

func (p *Probe) IdleFor() time.Duration {
	type lastInputInfo struct {
		Size uint32
		Time uint32
	}
	var info lastInputInfo
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0
	}
	tick, _, _ := procGetTickCount64.Call()
	if tick < uintptr(info.Time) {
		return 0
	}
	return time.Duration(uint64(tick)-uint64(info.Time)) * time.Millisecond
}

func (p *Probe) IsLocked() bool {
	desk, _, _ := procOpenInputDesktop.Call(0, 0, desktopSwitchDesktop)
	if desk == 0 {
		return true
	}
	defer procCloseDesktop.Call(desk)
	if name := strings.ToLower(desktopName(windows.Handle(desk))); name != "" && name != "default" {
		return true
	}
	ok, _, _ := procSwitchDesktop.Call(desk)
	return ok == 0
}

func desktopName(desktop windows.Handle) string {
	var needed uint32
	procGetUserObjectInfo.Call(uintptr(desktop), uoiName, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return ""
	}
	buf := make([]uint16, needed/2+1)
	ok, _, _ := procGetUserObjectInfo.Call(
		uintptr(desktop),
		uoiName,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ok == 0 {
		return ""
	}
	return strings.TrimSpace(windows.UTF16ToString(buf))
}

func windowText(hwnd windows.Handle) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return strings.TrimSpace(windows.UTF16ToString(buf[:n]))
}

func processPath(pid uint32) string {
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, syscall.MAX_PATH)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImage.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 || size == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func ExecutablePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

func strconvUint(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
