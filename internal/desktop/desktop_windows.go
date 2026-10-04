package desktop

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procSetConsoleTitle       = kernel32.NewProc("SetConsoleTitleW")
)

func knownFolder(f Folder) (string, error) {
	id := map[Folder]*windows.KNOWNFOLDERID{
		Documents: windows.FOLDERID_Documents,
		Downloads: windows.FOLDERID_Downloads,
		Desktop:   windows.FOLDERID_Desktop,
	}[f]
	return windows.KnownFolderPath(id, 0)
}

// OwnConsole reports whether the program has a console window of its own,
// i.e. it was started by double-click or from a shortcut rather than from
// cmd/PowerShell. Such a window closes as soon as the program exits.
func OwnConsole() bool {
	var pids [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// SetTitle sets the console window title.
func SetTitle(title string) {
	if p, err := windows.UTF16PtrFromString(title); err == nil {
		procSetConsoleTitle.Call(uintptr(unsafe.Pointer(p)))
	}
}

// Open opens a file in its default program (Excel for .xlsx).
func Open(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// IsLocked reports whether err means the file is open in another program.
func IsLocked(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// COM identifiers of the shell link object.
var (
	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = windows.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// Method numbers in the COM vtables.
const (
	vtQueryInterface        = 0
	vtRelease               = 2
	vtShellLinkSetDesc      = 7
	vtShellLinkSetWorkDir   = 9
	vtShellLinkSetPath      = 20
	vtPersistFileSave       = 6
	clsctxInprocServer      = 1
	coinitApartmentThreaded = 0x2
)

var procCoCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")

// comCall calls method n of a COM object and checks the HRESULT.
func comCall(obj unsafe.Pointer, n int, args ...uintptr) error {
	vtbl := *(**[32]uintptr)(obj)
	hr, _, _ := syscall.SyscallN(vtbl[n], append([]uintptr{uintptr(obj)}, args...)...)
	if int32(hr) < 0 {
		return windows.Errno(hr)
	}
	return nil
}

func utf16(s string) uintptr {
	p, _ := windows.UTF16PtrFromString(s)
	return uintptr(unsafe.Pointer(p))
}

// CreateShortcut creates (or replaces) a .lnk file that starts target in
// workDir. It uses IShellLinkW directly: WScript.Shell cannot handle
// characters outside the ANSI code page, such as Cyrillic.
func CreateShortcut(lnk, target, workDir, description string) error {
	if err := createShortcut(lnk, target, workDir, description); err != nil {
		return fmt.Errorf("не удалось создать ярлык %s: %w", lnk, err)
	}
	return nil
}

func createShortcut(lnk, target, workDir, description string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, coinitApartmentThreaded); err == nil {
		defer windows.CoUninitialize()
	} else if !errors.Is(err, windows.Errno(1)) { // S_FALSE: already initialized
		return err
	}

	var link unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&link)))
	if int32(hr) < 0 {
		return windows.Errno(hr)
	}
	defer comCall(link, vtRelease)

	if err := comCall(link, vtShellLinkSetPath, utf16(target)); err != nil {
		return err
	}
	if err := comCall(link, vtShellLinkSetWorkDir, utf16(workDir)); err != nil {
		return err
	}
	if err := comCall(link, vtShellLinkSetDesc, utf16(description)); err != nil {
		return err
	}

	var file unsafe.Pointer
	if err := comCall(link, vtQueryInterface, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&file))); err != nil {
		return err
	}
	defer comCall(file, vtRelease)
	return comCall(file, vtPersistFileSave, utf16(lnk), 1)
}
