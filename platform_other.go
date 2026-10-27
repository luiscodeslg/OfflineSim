//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func hideWindow(cmd *exec.Cmd)     {}
func setLowPriority(cmd *exec.Cmd) {}

func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, url).Start()
}

func openPath(path string) { openBrowser(path) }

func openWindow(url, dataPath string) bool { return false }

func showError(msg string) { fmt.Fprintln(os.Stderr, msg) }

func waitForExit(pid int) {}
