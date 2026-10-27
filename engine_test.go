package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNightlyListingBothNamingSchemes(t *testing.T) {
	listing := `<a href="simc-920-01-win64-1db0f25.7z">old</a> 2026-10-27 09:00
<a href="simc-1210.01.73a372b-win64.7z">new</a> 2026-10-26 03:10
<a href="simc-1210.01.73a372b-winarm64.7z">arm</a> 2026-10-27 03:10
<a href="simc-1210.01.73a372b-win64.7z.sha256">sum</a>`
	b := parseNightlyListing(listing)
	if len(b) != 2 || b[0].name != "simc-1210.01.73a372b-win64.7z" || b[0].version != 1210 || b[0].release != 1 {
		t.Fatalf("builds: %+v", b)
	}
}

func TestVersionFromNameAndEngineInfo(t *testing.T) {
	v := versionFromName("simc-1210.01.73a372b-win64.7z")
	if !strings.HasPrefix(v, "SimulationCraft 1210-01") {
		t.Fatal(v)
	}
	if e := engineInfo(v); e.WoWMajor != 12 || e.LevelCap != 90 {
		t.Fatalf("%+v", e)
	}
	if e := engineInfo("SimulationCraft 1210.01 for World of Warcraft 12.1.0.1 Live"); e.WoWMajor != 12 {
		t.Fatalf("%+v", e)
	}
}

func fakeExe(t *testing.T, body string) string {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	p := filepath.Join(t.TempDir(), "simc-1210.01.73a372b-win64", "simc")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755)
	return p
}

func TestProbeBanner(t *testing.T) {
	// Banner only appears with the "help" argument, with a CR and prefix noise.
	exe := fakeExe(t, `if [ "$1" = help ]; then printf 'noise\r\nSimulationCraft 1210-01 for World of Warcraft 12.1.0.1 Live\r\n'; fi`)
	b, _ := probeBanner(exe)
	if !strings.HasPrefix(b, "SimulationCraft 1210-01 for World of Warcraft") {
		t.Fatalf("banner %q", b)
	}
	// Banner on stderr and a non-zero exit still counts.
	exe = fakeExe(t, `echo 'SimulationCraft 1205-01 for World of Warcraft 12.0.5 Live' >&2; exit 1`)
	if b, _ := probeBanner(exe); !strings.Contains(b, "1205-01") {
		t.Fatalf("stderr banner %q", b)
	}
}

func TestSimcVersionFallsBackToFileName(t *testing.T) {
	exe := fakeExe(t, `echo 'usage: simc [options]'; exit 1`)
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.settings.SimcPath = exe
	v, err := a.simcVersion()
	if err != nil || !strings.HasPrefix(v, "SimulationCraft 1210-01") {
		t.Fatalf("v=%q err=%v", v, err)
	}
}
