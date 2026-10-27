//go:build windows

package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

const createNoWindow = 0x08000000

//go:embed icon.ico
var iconICO []byte

var user32 = syscall.NewLazyDLL("user32.dll")

func init() {
	// The WebView2 window must be created and pumped on the main OS thread.
	runtime.LockOSThread()
}

// enableHiDPI keeps the window sharp on scaled displays (no manifest needed).
func enableHiDPI() {
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		p.Call(perMonitorAwareV2)
	}
}

// setWindowIcon loads our .ico from disk and assigns it to the window,
// so the title bar and taskbar show the app icon.
func setWindowIcon(hwnd uintptr, dir string) {
	path := filepath.Join(dir, "offlinesim.ico")
	if err := os.WriteFile(path, iconICO, 0o644); err != nil {
		return
	}
	p, _ := syscall.UTF16PtrFromString(path)
	load := user32.NewProc("LoadImageW")
	send := user32.NewProc("SendMessageW")
	const imageIcon, lrLoadFromFile, wmSetIcon = 1, 0x10, 0x80
	for _, size := range []struct{ kind, px uintptr }{{0, 16}, {1, 32}} {
		h, _, _ := load.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, size.px, size.px, lrLoadFromFile)
		if h != 0 {
			send.Call(hwnd, wmSetIcon, size.kind, h)
		}
	}
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func setLowPriority(cmd *exec.Cmd) {
	const belowNormal = 0x00004000
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= belowNormal
}

func openBrowser(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	_ = cmd.Start()
}

func openPath(path string) {
	cmd := exec.Command("explorer", path)
	_ = cmd.Start()
}

func openWindow(url, dataPath string) bool {
	enableHiDPI()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  appName + " - offline WoW character simulator",
			Width:  1440,
			Height: 920,
			Center: true,
		},
	})
	if w == nil {
		return false
	}
	defer w.Destroy()
	setWindowIcon(uintptr(w.Window()), filepath.Dir(dataPath))
	w.Navigate(url)
	w.Run()
	return true
}

func showError(msg string) {
	box := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(appName)
	m, _ := syscall.UTF16PtrFromString(msg)
	box.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

func waitForExit(pid int) {
	const synchronize = 0x00100000
	k32 := syscall.NewLazyDLL("kernel32.dll")
	h, _, _ := k32.NewProc("OpenProcess").Call(synchronize, 0, uintptr(pid))
	if h == 0 {
		return
	}
	k32.NewProc("WaitForSingleObject").Call(h, 15000)
	k32.NewProc("CloseHandle").Call(h)
}
