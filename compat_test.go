package main

import (
	"strings"
	"testing"
)

func TestParseNightlyListingPrefersNewestVersion(t *testing.T) {
	// An old Shadowlands build with the most recent timestamp, listed first
	// (as a ?C=M;O=D listing would), must not win over current builds.
	listing := `<table>
<tr><td><a href="simc-920-01-win64-1db0f25.7z">simc-920-01-win64-1db0f25.7z</a></td><td align="right">2026-10-27 09:00  </td></tr>
<tr><td><a href="simc-1205-01-win64-aaa1111.7z">simc-1205-01-win64-aaa1111.7z</a></td><td align="right">2026-10-25 03:10  </td></tr>
<tr><td><a href="simc-1205-01-win64-bbb2222.7z">simc-1205-01-win64-bbb2222.7z</a></td><td align="right">2026-10-26 03:10  </td></tr>
<tr><td><a href="simc-1205-01-winarm64-ccc3333.7z">arm</a></td><td align="right">2026-10-27 03:10  </td></tr>
</table>
<pre><a href="simc-1105-02-win64-ddd4444.7z">simc-1105-02-win64-ddd4444.7z</a>    27-Oct-2026 08:00   90M
</pre>`
	b := parseNightlyListing(listing)
	if len(b) != 4 {
		t.Fatalf("got %d builds", len(b))
	}
	if b[0].name != "simc-1205-01-win64-bbb2222.7z" {
		t.Fatalf("picked %s", b[0].name)
	}
	if b[len(b)-1].version != 920 {
		t.Fatalf("920 should rank last, got %s", b[len(b)-1].name)
	}
}

func TestCompatProblem(t *testing.T) {
	sl := engineInfo("SimulationCraft 920-01 for World of Warcraft 9.2.0.43340 Live (hotfix 2022-04-29/43340, git build shadowlands 1db0f25)")
	if sl.WoWMajor != 9 || sl.LevelCap != 60 || sl.Expansion != "Shadowlands" {
		t.Fatalf("engine: %+v", sl)
	}
	prof := "# WoW 12.0.5.99999, TOC 120005\nmage=\"X\"\nlevel=90\nspec=fire\n"
	msg := compatProblem(sl, parseProfile(prof), prof)
	if !strings.Contains(msg, "WoW 12.0.5") || !strings.Contains(msg, "Install") {
		t.Fatalf("msg: %q", msg)
	}
	// No addon header: fall back to the level check.
	prof2 := "mage=\"X\"\nlevel=90\n"
	if msg := compatProblem(sl, parseProfile(prof2), prof2); !strings.Contains(msg, "level 60") {
		t.Fatalf("level msg: %q", msg)
	}
	mid := engineInfo("SimulationCraft 1205-01 for World of Warcraft 12.0.5.12345 Live")
	if msg := compatProblem(mid, parseProfile(prof), prof); msg != "" {
		t.Fatalf("unexpected: %q", msg)
	}
}
