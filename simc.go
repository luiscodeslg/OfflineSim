package main

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bodgit/sevenzip"
)

const defaultNightlyURL = "https://downloads.simulationcraft.org/nightly/?C=M;O=D"

// simcInstall records the managed SimulationCraft build.
type simcInstall struct {
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Exe         string    `json:"exe"`
	InstalledAt time.Time `json:"installedAt"`
}

func simcExeName() string {
	if runtime.GOOS == "windows" {
		return "simc.exe"
	}
	return "simc"
}

func (a *App) managedInstall() (simcInstall, bool) {
	var in simcInstall
	b, err := os.ReadFile(a.path("simc", "installed.json"))
	if err != nil || json.Unmarshal(b, &in) != nil {
		return in, false
	}
	if _, err := os.Stat(in.Exe); err != nil {
		return in, false
	}
	return in, true
}

// simcPath returns the simc executable to use, or "" when none is set up.
func (a *App) simcPath() string {
	if p := a.getSettings().SimcPath; p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if in, ok := a.managedInstall(); ok {
		return in.Exe
	}
	// A simc build dropped next to OfflineSim.exe also works.
	if exe, err := os.Executable(); err == nil {
		for _, rel := range []string{simcExeName(), filepath.Join("simc", simcExeName())} {
			p := filepath.Join(filepath.Dir(exe), rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// simcRoot is the folder holding the simc executable (with profiles/ next to it).
func (a *App) simcRoot() string {
	if p := a.simcPath(); p != "" {
		return filepath.Dir(p)
	}
	return ""
}

var (
	versionMu    sync.Mutex
	versionCache = map[string]string{}
)

// simcVersion runs simc without arguments and returns its banner line,
// e.g. "SimulationCraft 1200-01 for World of Warcraft 12.0.0.12345 Live".
func (a *App) simcVersion() (string, error) {
	p := a.simcPath()
	if p == "" {
		return "", errors.New("SimulationCraft is not installed")
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s|%d|%d", p, st.Size(), st.ModTime().UnixNano())
	versionMu.Lock()
	if v, ok := versionCache[key]; ok {
		versionMu.Unlock()
		return v, nil
	}
	versionMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p)
	cmd.Dir = filepath.Dir(p)
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()
	v := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SimulationCraft") {
			v = line
			break
		}
	}
	if v == "" {
		return "", fmt.Errorf("could not read simc version from %s", p)
	}
	versionMu.Lock()
	versionCache[key] = v
	versionMu.Unlock()
	return v, nil
}

// simcVersionKey is a short filesystem-safe tag for caching per simc build.
func (a *App) simcVersionKey() string {
	v, err := a.simcVersion()
	if err != nil {
		return "unknown"
	}
	re := regexp.MustCompile(`[^A-Za-z0-9.-]+`)
	k := re.ReplaceAllString(v, "_")
	if len(k) > 90 {
		k = k[:90]
	}
	return k
}

var (
	nightlyRe   = regexp.MustCompile(`href="([^"]*?(simc-([0-9]+)-([0-9]+)-win64-[0-9a-f]+\.7z))"`)
	listingDate = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2})|(\d{2})-([A-Za-z]{3})-(\d{4}) (\d{2}:\d{2})`)
)

type nightlyBuild struct {
	name, href string
	version    int // e.g. 1205 for simc-1205-01
	release    int // the "-01" part
	date       time.Time
	pos        int
}

// latestNightly finds the newest Windows x64 build in the listing. It ranks
// builds by SimC version first (so an old build with a fresh timestamp can
// never win), then by the date shown in the listing.
func latestNightly(ctx context.Context, listing string) (name, url string, err error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", listing, nil)
	req.Header.Set("User-Agent", appName+"/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("%s returned %s", listing, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", "", err
	}
	builds := parseNightlyListing(string(body))
	if len(builds) == 0 {
		return "", "", errors.New("no Windows x64 build found in the nightly listing")
	}
	best := builds[0]
	base := listing
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	if !strings.HasSuffix(base, "/") {
		base = base[:strings.LastIndex(base, "/")+1]
	}
	href := best.href
	if !strings.HasPrefix(href, "http") {
		href = base + strings.TrimPrefix(href, "./")
	}
	return best.name, href, nil
}

// parseNightlyListing returns the builds in a directory listing, best first.
func parseNightlyListing(body string) []nightlyBuild {
	var builds []nightlyBuild
	seen := map[string]bool{}
	for _, idx := range nightlyRe.FindAllStringSubmatchIndex(body, -1) {
		m := func(i int) string { return body[idx[2*i]:idx[2*i+1]] }
		b := nightlyBuild{href: m(1), name: m(2), pos: len(builds)}
		if seen[b.name] {
			continue
		}
		seen[b.name] = true
		fmt.Sscan(m(3), &b.version)
		fmt.Sscan(m(4), &b.release)
		// The date sits in the same table row / line, after the link.
		rest := body[idx[1]:]
		if len(rest) > 300 {
			rest = rest[:300]
		}
		for _, stop := range []string{"</tr>", "\n", "href="} {
			if j := strings.Index(rest, stop); j >= 0 {
				rest = rest[:j]
			}
		}
		if d := listingDate.FindStringSubmatch(rest); d != nil {
			if d[1] != "" {
				b.date, _ = time.Parse("2006-01-02 15:04", d[1]+" "+d[2])
			} else {
				b.date, _ = time.Parse("02-Jan-2006 15:04", d[3]+"-"+d[4]+"-"+d[5]+" "+d[6])
			}
		}
		builds = append(builds, b)
	}
	sort.SliceStable(builds, func(i, k int) bool {
		x, y := builds[i], builds[k]
		if x.version != y.version {
			return x.version > y.version
		}
		if x.release != y.release {
			return x.release > y.release
		}
		if !x.date.Equal(y.date) {
			return x.date.After(y.date)
		}
		return x.pos < y.pos
	})
	return builds
}

func download(ctx context.Context, j *job, url, dest string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", appName+"/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if total > 0 && time.Since(last) > 200*time.Millisecond {
				j.setProgress(float64(done)/float64(total)*0.8, fmt.Sprintf("Downloading %.1f / %.1f MB", float64(done)/1e6, float64(total)/1e6))
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	j.log(fmt.Sprintf("Downloaded %.1f MB", float64(done)/1e6))
	return nil
}

// safeJoin keeps archive entries inside dest.
func safeJoin(dest, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	p := filepath.Join(dest, filepath.FromSlash(name))
	if !strings.HasPrefix(p, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe path in archive: %s", name)
	}
	return p, nil
}

type archiveEntry struct {
	name string
	dir  bool
	open func() (io.ReadCloser, error)
}

func extractEntries(ctx context.Context, j *job, entries []archiveEntry, dest string) error {
	for i, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p, err := safeJoin(dest, e.name)
		if err != nil {
			return err
		}
		if e.dir {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := e.open()
		if err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(f, rc)
		rc.Close()
		f.Close()
		if err != nil {
			return fmt.Errorf("extracting %s: %w", e.name, err)
		}
		if i%25 == 0 {
			j.setProgress(0.8+0.2*float64(i)/float64(len(entries)), "Extracting")
		}
	}
	return nil
}

func extractArchive(ctx context.Context, j *job, archive, dest string) error {
	var entries []archiveEntry
	head := make([]byte, 6)
	if f, err := os.Open(archive); err == nil {
		_, _ = io.ReadFull(f, head)
		f.Close()
	}
	switch {
	case string(head[:2]) == "PK":
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			f := f
			entries = append(entries, archiveEntry{name: f.Name, dir: f.FileInfo().IsDir(), open: f.Open})
		}
	case string(head) == "7z\xbc\xaf\x27\x1c":
		zr, err := sevenzip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			f := f
			entries = append(entries, archiveEntry{name: f.Name, dir: f.FileInfo().IsDir(), open: f.Open})
		}
	default:
		return errors.New("unsupported archive: expected a SimulationCraft .7z or .zip")
	}
	j.log(fmt.Sprintf("Extracting %d files", len(entries)))
	return extractEntries(ctx, j, entries, dest)
}

func findSimcExe(root string) string {
	var found []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.EqualFold(info.Name(), simcExeName()) {
			found = append(found, p)
		}
		return nil
	})
	sort.Slice(found, func(i, k int) bool { return len(found[i]) < len(found[k]) })
	if len(found) == 0 {
		return ""
	}
	return found[0]
}

// installFromArchive extracts a simc build and makes it the managed install.
func (a *App) installFromArchive(ctx context.Context, j *job, archive, name, source string) error {
	dest := a.path("simc", sanitizeName(strings.TrimSuffix(strings.TrimSuffix(name, ".7z"), ".zip"))+"-"+time.Now().Format("20060102150405"))
	if err := extractArchive(ctx, j, archive, dest); err != nil {
		os.RemoveAll(dest)
		return err
	}
	exe := findSimcExe(dest)
	if exe == "" {
		os.RemoveAll(dest)
		return fmt.Errorf("no %s found in the archive", simcExeName())
	}
	old, hadOld := a.managedInstall()
	in := simcInstall{Name: name, Source: source, Exe: exe, InstalledAt: time.Now()}
	b, _ := json.MarshalIndent(in, "", "  ")
	if err := writeFileAtomic(a.path("simc", "installed.json"), b); err != nil {
		return err
	}
	// Prefer the managed build from now on.
	s := a.getSettings()
	s.SimcPath = ""
	_ = a.saveSettings(s)
	if hadOld {
		rel, err := filepath.Rel(a.path("simc"), old.Exe)
		if err == nil && !strings.HasPrefix(rel, "..") {
			top := strings.Split(filepath.ToSlash(rel), "/")[0]
			if top != "" && top != filepath.Base(dest) {
				os.RemoveAll(a.path("simc", top))
			}
		}
	}
	v, err := a.simcVersion()
	if err != nil {
		j.log("Installed, but the version check failed: " + err.Error())
	} else {
		j.log("Installed " + v)
		if e := engineInfo(v); e.WoW != "" {
			j.log(fmt.Sprintf("This engine sims WoW %s %s (level cap %d)", e.WoW, e.Expansion, e.LevelCap))
		}
	}
	return nil
}

func (a *App) installLatest(ctx context.Context, j *job) error {
	listing := a.getSettings().NightlyURL
	j.setProgress(-1, "Checking for the latest build")
	j.log("Looking for the newest Windows build at " + listing)
	name, url, err := latestNightly(ctx, listing)
	if err != nil && strings.HasPrefix(listing, "https://") {
		j.log("HTTPS failed (" + err.Error() + "), trying HTTP")
		name, url, err = latestNightly(ctx, "http://"+strings.TrimPrefix(listing, "https://"))
	}
	if err != nil {
		return err
	}
	j.log("Newest build in the listing: " + name)
	if in, ok := a.managedInstall(); ok && in.Name == name && a.getSettings().SimcPath == "" {
		j.log("Already up to date: " + name)
		return nil
	}
	j.log("Downloading " + url)
	tmp := a.path("tmp", name)
	defer os.Remove(tmp)
	if err := download(ctx, j, url, tmp); err != nil {
		return err
	}
	return a.installFromArchive(ctx, j, tmp, name, url)
}

func sanitizeName(s string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	s = re.ReplaceAllString(s, "_")
	if s == "" {
		s = "x"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// runSimc runs simc with an input file, streaming output to the job and
// parsing progress bars. Returns combined output on failure.
func (a *App) runSimc(j *job, workDir string, args ...string) error {
	exe := a.simcPath()
	if exe == "" {
		return errors.New("SimulationCraft is not installed yet. Open Settings and click Install")
	}
	cmd := exec.CommandContext(j.ctx, exe, args...)
	cmd.Dir = workDir
	hideWindow(cmd)
	if a.getSettings().LowPriority {
		setLowPriority(cmd)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	sc.Split(scanLinesOrCR)
	var lastBar string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " ")
		if line == "" {
			continue
		}
		if p, phase, ok := parseProgress(line); ok {
			j.setProgress(p, phase)
			lastBar = line
			continue
		}
		j.log(line)
	}
	if lastBar != "" {
		j.log(lastBar)
	}
	if err := cmd.Wait(); err != nil {
		if j.ctx.Err() != nil {
			return j.ctx.Err()
		}
		return fmt.Errorf("simc exited with an error (%v); see the log for details", err)
	}
	return nil
}

// scanLinesOrCR splits on \n and \r so simc's in-place progress bar updates
// arrive as separate tokens.
func scanLinesOrCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// simc progress lines look like:
//
//	Generating Baseline: Name [=========>..........] 4512/10000 Mean=123456.7 ...
//	Profilesets (12/40): combo_3 [====>....] 1200/5000
var (
	progressRe   = regexp.MustCompile(`^(.*?)\s*\[[=>.]{3,}[=>.\s]*\]\s*(?:(\d+)/(\d+))?`)
	profilesetRe = regexp.MustCompile(`Profilesets\s*\((\d+)/(\d+)\)`)
)

func parseProgress(line string) (float64, string, bool) {
	m := progressRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	var cur, tot float64
	fmt.Sscan(m[2], &cur)
	fmt.Sscan(m[3], &tot)
	phase := strings.TrimSpace(m[1])
	if tot <= 0 {
		// A progress bar without a count: still progress, just not measurable.
		return -1, phase, true
	}
	p := cur / tot
	if ps := profilesetRe.FindStringSubmatch(line); ps != nil {
		var i, n float64
		fmt.Sscan(ps[1], &i)
		fmt.Sscan(ps[2], &n)
		if n > 0 {
			p = (i - 1 + p) / n
		}
	}
	if p > 1 {
		p = 1
	}
	return p, phase, true
}
