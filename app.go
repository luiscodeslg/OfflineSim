package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Settings are persisted to settings.json in the data directory.
type Settings struct {
	SimcPath    string  `json:"simcPath"`    // path to simc(.exe); empty = managed install
	Threads     int     `json:"threads"`     // 0 = all cores
	LowPriority bool    `json:"lowPriority"` // run simc below normal priority
	NightlyURL  string  `json:"nightlyUrl"`  // listing to look for nightly builds
	Iterations  int     `json:"iterations"`  // default iterations for new sims (0 = target error)
	TargetError float64 `json:"targetError"`
}

type App struct {
	dir      string
	mu       sync.Mutex
	settings Settings
	jobs     *jobManager
	install  *job // current simc install/update job, if any
	quit     chan struct{}
	quitOnce sync.Once
}

func newApp(dir string) (*App, error) {
	a := &App{dir: dir, quit: make(chan struct{})}
	a.settings = Settings{LowPriority: true, NightlyURL: defaultNightlyURL, TargetError: 0.1}
	if b, err := os.ReadFile(a.path("settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
	if a.settings.NightlyURL == "" {
		a.settings.NightlyURL = defaultNightlyURL
	}
	for _, d := range []string{"simc", "apl/default", "apl/custom", "results", "tmp"} {
		if err := os.MkdirAll(a.path(d), 0o755); err != nil {
			return nil, err
		}
	}
	a.jobs = newJobManager()
	return a, nil
}

func (a *App) path(parts ...string) string {
	return filepath.Join(append([]string{a.dir}, parts...)...)
}

func (a *App) getSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

func (a *App) saveSettings(s Settings) error {
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	b, _ := json.MarshalIndent(s, "", "  ")
	return writeFileAtomic(a.path("settings.json"), b)
}

func (a *App) threads() int {
	if t := a.getSettings().Threads; t > 0 {
		return t
	}
	return runtime.NumCPU()
}

func (a *App) shutdown() {
	a.jobs.cancelAll()
}

func (a *App) requestQuit() {
	a.quitOnce.Do(func() { close(a.quit) })
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
