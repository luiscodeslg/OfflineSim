package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"offlinesim/internal/update"
)

// appBuild is the release build number, set by CI with
// -ldflags "-X main.appBuild=N". "0" means a local/dev build.
var appBuild = "0"

//go:embed update/public-key.pem
var updatePublicKey []byte

// updateBase is where releases are published. The repo is public, so the
// latest release's assets are plain downloads with no token.
var updateBase = "https://github.com/luiscodeslg/OfflineSim/releases/latest/download/"

// Hosts GitHub serves release assets from. Every request and redirect must
// stay on https and on this list.
var updateHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

const updateEvery = 2 * time.Hour

func currentBuild() int {
	n, _ := strconv.Atoi(appBuild)
	return n
}

type updateStatus struct {
	State     string    `json:"state"` // idle, checking, current, downloading, ready, error
	Build     int       `json:"build"`
	Latest    int       `json:"latest,omitempty"`
	Version   string    `json:"version,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	Error     string    `json:"error,omitempty"`
	LastCheck time.Time `json:"lastCheck"`
	CanApply  bool      `json:"canApply"`
}

type selfUpdater struct {
	mu      sync.Mutex
	dir     string // <data>/updates
	status  updateStatus
	client  *http.Client
	running bool
}

func newSelfUpdater(dir string) *selfUpdater {
	u := &selfUpdater{dir: dir, status: updateStatus{State: "idle", Build: currentBuild()}}
	u.client = &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return errors.New("too many redirects")
			}
			return allowedUpdateURL(req.URL)
		},
	}
	return u
}

func allowedUpdateURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("update source must be https, got %s", u.Scheme)
	}
	for _, h := range updateHosts {
		if u.Hostname() == h {
			return nil
		}
	}
	return fmt.Errorf("update host not allowed: %s", u.Hostname())
}

func (u *selfUpdater) get() updateStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.status
	s.CanApply = s.State == "ready" && runtime.GOOS == "windows"
	return s
}

func (u *selfUpdater) set(f func(s *updateStatus)) {
	u.mu.Lock()
	f(&u.status)
	u.mu.Unlock()
}

func (u *selfUpdater) fetch(ctx context.Context, name string, limit int64) ([]byte, error) {
	target, err := url.Parse(updateBase + name)
	if err != nil {
		return nil, err
	}
	if err := allowedUpdateURL(target); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
	req.Header.Set("User-Agent", appName+"/"+appVersion)
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNoRelease
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %s for %s", resp.Status, name)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than allowed", name)
	}
	return b, nil
}

var errNoRelease = errors.New("no release has been published yet")

// check looks for a newer signed build and downloads it. It never installs
// anything; apply does that after the user agrees to restart.
func (u *selfUpdater) check(ctx context.Context) error {
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return nil
	}
	u.running = true
	u.status.State, u.status.Error = "checking", ""
	u.mu.Unlock()
	err := u.doCheck(ctx)
	u.mu.Lock()
	u.running = false
	u.status.LastCheck = time.Now()
	if err != nil {
		u.status.State, u.status.Error = "error", err.Error()
		if errors.Is(err, errNoRelease) {
			u.status.State, u.status.Error = "current", ""
		}
	}
	u.mu.Unlock()
	if err != nil {
		log.Printf("update check: %v", err)
	}
	return err
}

func (u *selfUpdater) doCheck(ctx context.Context) error {
	pub, err := update.ParsePublicKey(updatePublicKey)
	if err != nil {
		return err
	}
	mb, err := u.fetch(ctx, "manifest.json", 64<<10)
	if err != nil {
		return err
	}
	sig, err := u.fetch(ctx, "manifest.sig", 1<<10)
	if err != nil {
		return err
	}
	m, err := update.Verify(pub, mb, string(sig))
	if err != nil {
		return err
	}
	u.set(func(s *updateStatus) { s.Latest, s.Version, s.Notes = m.Build, m.Version, m.Notes })
	if m.Build <= currentBuild() {
		u.set(func(s *updateStatus) { s.State = "current" })
		return nil
	}
	if _, err := u.staged(pub); err == nil {
		u.set(func(s *updateStatus) { s.State = "ready" })
		return nil
	}
	u.set(func(s *updateStatus) { s.State = "downloading" })
	exe, err := u.fetch(ctx, update.ExeName, update.MaxExe)
	if err != nil {
		return err
	}
	if err := update.CheckFile(m, exe); err != nil {
		return err
	}
	if err := os.MkdirAll(u.dir, 0o755); err != nil {
		return err
	}
	// Write the exe first and the manifest last, so a half-finished download
	// never looks complete.
	stage := filepath.Join(u.dir, "staged")
	os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{update.ExeName, exe}, {"manifest.sig", sig}, {"manifest.json", mb}} {
		if err := writeFileAtomic(filepath.Join(stage, f.name), f.data); err != nil {
			return err
		}
	}
	if _, err := u.staged(pub); err != nil {
		return err
	}
	u.set(func(s *updateStatus) { s.State = "ready" })
	log.Printf("update: build %d downloaded and verified", m.Build)
	return nil
}

// staged re-verifies the downloaded update from disk and returns the exe path.
func (u *selfUpdater) staged(pub []byte) (string, error) {
	stage := filepath.Join(u.dir, "staged")
	mb, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		return "", err
	}
	sig, err := os.ReadFile(filepath.Join(stage, "manifest.sig"))
	if err != nil {
		return "", err
	}
	m, err := update.Verify(pub, mb, string(sig))
	if err != nil {
		return "", err
	}
	if m.Build <= currentBuild() {
		return "", errors.New("staged update is not newer than this build")
	}
	exePath := filepath.Join(stage, update.ExeName)
	data, err := os.ReadFile(exePath)
	if err != nil {
		return "", err
	}
	if err := update.CheckFile(m, data); err != nil {
		return "", err
	}
	return exePath, nil
}

// apply swaps the running exe for the verified update and restarts.
// Windows lets a running exe be renamed, so the old one moves aside to
// OfflineSim.exe.old and is deleted on the next start.
func (u *selfUpdater) apply() error {
	if runtime.GOOS != "windows" {
		return errors.New("self-update is only supported on Windows")
	}
	pub, err := update.ParsePublicKey(updatePublicKey)
	if err != nil {
		return err
	}
	src, err := u.staged(pub)
	if err != nil {
		return fmt.Errorf("the downloaded update failed verification: %w", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("can't replace %s (is the folder read-only?): %w", exe, err)
	}
	if err := os.WriteFile(exe, data, 0o755); err != nil {
		os.Rename(old, exe)
		return err
	}
	args := append([]string{"-wait-pid", strconv.Itoa(os.Getpid())}, userArgs()...)
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		os.Remove(exe)
		os.Rename(old, exe)
		return err
	}
	os.RemoveAll(filepath.Join(u.dir, "staged"))
	log.Printf("update: restarting into the new build")
	return nil
}

// userArgs are the original command line flags minus our internal ones.
func userArgs() []string {
	var out []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-wait-pid" || args[i] == "--wait-pid" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-wait-pid=") || strings.HasPrefix(args[i], "--wait-pid=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// cleanupOldExe removes the previous exe left behind by an update.
func cleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	for i := 0; i < 10; i++ {
		if err := os.Remove(old); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(time.Second)
	}
}

// loop checks at startup and then every couple of hours.
func (u *selfUpdater) loop(enabled func() bool) {
	time.Sleep(8 * time.Second)
	for {
		if enabled() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			_ = u.check(ctx)
			cancel()
		}
		time.Sleep(updateEvery)
	}
}
