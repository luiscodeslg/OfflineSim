// OfflineSim: a local, offline desktop front end for the SimulationCraft engine.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

const appName = "OfflineSim"

var appVersion = "1.0.0"

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, appName)
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, appName)
	}
	return filepath.Join(".", appName+"-data")
}

func main() {
	dataDir := flag.String("data", "", "data directory (default: %LOCALAPPDATA%\\OfflineSim, or ./data next to the exe in portable mode)")
	port := flag.Int("port", 0, "port for the local UI server (0 = random)")
	browser := flag.Bool("browser", false, "open the UI in the default browser instead of an app window")
	headless := flag.Bool("headless", false, "only run the server and print its URL")
	flag.Parse()

	dir := *dataDir
	if dir == "" {
		// Portable mode: a "data" folder next to the exe wins.
		if exe, err := os.Executable(); err == nil {
			p := filepath.Join(filepath.Dir(exe), "data")
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				dir = p
			}
		}
	}
	if dir == "" {
		dir = defaultDataDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal("cannot create data directory %s: %v", dir, err)
	}
	if f, err := os.OpenFile(filepath.Join(dir, "offlinesim.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(f)
	}

	app, err := newApp(dir)
	if err != nil {
		fatal("startup failed: %v", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fatal("cannot listen: %v", err)
	}
	url := "http://" + ln.Addr().String() + "/"
	log.Printf("OfflineSim %s serving %s (data: %s)", appVersion, url, dir)
	go func() {
		if err := http.Serve(ln, app.routes()); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()

	switch {
	case *headless:
		fmt.Println(url)
		select {}
	case *browser:
		openBrowser(url)
		<-app.quit
	default:
		if !openWindow(url, filepath.Join(dir, "webview")) {
			// WebView2 runtime missing: fall back to the default browser.
			log.Printf("app window unavailable, using browser")
			openBrowser(url)
			<-app.quit
		}
	}
	app.shutdown()
}

func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	showError(msg)
	os.Exit(1)
}
