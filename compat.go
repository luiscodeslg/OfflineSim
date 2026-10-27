package main

import (
	"fmt"
	"regexp"
	"strconv"
)

// Expansion level caps by WoW major version.
var levelCaps = map[int]struct {
	cap  int
	name string
}{
	8: {50, "Battle for Azeroth"}, 9: {60, "Shadowlands"}, 10: {70, "Dragonflight"},
	11: {80, "The War Within"}, 12: {90, "Midnight"}, 13: {90, "The Last Titan"},
}

var (
	engineWoWRe  = regexp.MustCompile(`World of Warcraft (\d+)\.(\d+)(?:\.(\d+))?`)
	engineSimcRe = regexp.MustCompile(`SimulationCraft (\d+)[-.](\d+)`)
	profileWoWRe = regexp.MustCompile(`(?m)^#\s*WoW (\d+)\.(\d+)(?:\.(\d+))?`)
)

// EngineInfo describes the installed simc build in WoW terms.
type EngineInfo struct {
	Banner    string `json:"banner"`
	WoWMajor  int    `json:"wowMajor"`
	WoW       string `json:"wow"`       // "12.0.5"
	Expansion string `json:"expansion"` // "Midnight"
	LevelCap  int    `json:"levelCap"`
}

func engineInfo(banner string) EngineInfo {
	e := EngineInfo{Banner: banner}
	if m := engineWoWRe.FindStringSubmatch(banner); m != nil {
		e.WoWMajor, _ = strconv.Atoi(m[1])
		e.WoW = m[1] + "." + m[2]
		if m[3] != "" {
			e.WoW += "." + m[3]
		}
	} else if m := engineSimcRe.FindStringSubmatch(banner); m != nil {
		// "SimulationCraft 1205-01": the leading digits before the last two are the WoW major.
		if v, err := strconv.Atoi(m[1]); err == nil && v >= 100 {
			e.WoWMajor = v / 100
		}
	}
	if c, ok := levelCaps[e.WoWMajor]; ok {
		e.LevelCap, e.Expansion = c.cap, c.name
	}
	return e
}

// profileWoW reads the WoW version line the /simc addon writes ("# WoW 12.0.5.12345").
func profileWoW(text string) (major int, version string) {
	if m := profileWoWRe.FindStringSubmatch(text); m != nil {
		major, _ = strconv.Atoi(m[1])
		version = m[1] + "." + m[2]
		if m[3] != "" {
			version += "." + m[3]
		}
	}
	return
}

// compatProblem explains why the installed engine can't sim this character,
// or returns "" when it looks fine.
func compatProblem(e EngineInfo, info ProfileInfo, text string) string {
	if e.WoWMajor == 0 {
		return ""
	}
	engine := "WoW " + e.WoW
	if e.Expansion != "" {
		engine += " (" + e.Expansion + ")"
	}
	fix := " Go to Settings and click Install / update latest nightly."
	if major, ver := profileWoW(text); major > e.WoWMajor {
		return fmt.Sprintf("Your SimulationCraft engine is for %s, but this character was exported from WoW %s. It's too old to sim it.%s", engine, ver, fix)
	}
	if lvl, err := strconv.Atoi(info.Level); err == nil && e.LevelCap > 0 && lvl > e.LevelCap {
		return fmt.Sprintf("This character is level %d, but your SimulationCraft engine is for %s, which stops at level %d.%s", lvl, engine, e.LevelCap, fix)
	}
	return ""
}
